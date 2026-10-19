package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
)

// File access on the phone (the IDE view's Project panel). The handlers translate to the driver's fs_* ops and
// answer 501 (with the op contract) when the driver does not offer them. Contract, see docs/phonelab.md "IDE view":
//
//	fs_list  {path}                          -> {entries:[{name, type:"d"|"f", size, mtime}]}
//	fs_read  {path, offset, len}             -> {data_b64, eof}
//	fs_write {path, offset, data_b64, truncate} -> {written}
//	fs_delete {path}                         -> {deleted}
const fsChunk = 3072

var fsOps = []string{"fs_list", "fs_read", "fs_write", "fs_delete"}

func (s *Server) mountFS(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/devices/{id}/fs", s.fsList)
	mux.HandleFunc("GET /api/devices/{id}/fs/content", s.fsRead)
	mux.HandleFunc("PUT /api/devices/{id}/fs/content", s.fsWrite)
	mux.HandleFunc("DELETE /api/devices/{id}/fs/content", s.fsDelete)
	mux.HandleFunc("GET /api/devices/{id}/ttys", s.ttyList)
	mux.HandleFunc("GET /api/devices/{id}/tty/{tty}", s.ttyBridge)
}

type FSEntry struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"` // d | f
	Size  int64   `json:"size"`
	MTime float64 `json:"mtime,omitempty"`
}

func hasOp(ss *core.Session, name string) bool {
	for _, o := range ss.Ops() {
		if o.Name == name {
			return true
		}
	}
	return false
}

// fsGate answers 501 unless the driver offers op.
func (s *Server) fsGate(w http.ResponseWriter, ss *core.Session, op string) bool {
	if hasOp(ss, op) {
		return true
	}
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":       fmt.Sprintf("device %q has no %s op: the phone-side file service is not implemented yet", ss.ID, op),
		"missing_ops": fsOps,
	})
	return false
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean("/" + strings.ReplaceAll(p, "\\", "/"))
}

func (s *Server) fsCall(ctx context.Context, ss *core.Session, op string, a device.Args) (map[string]any, error) {
	res, err := s.Exec(ctx, ss, "fs", op, a)
	if err != nil {
		return nil, err
	}
	m, ok := res.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: unexpected result %T", op, res)
	}
	return m, nil
}

func (s *Server) fsList(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil || !s.fsGate(w, ss, "fs_list") {
		return
	}
	p := cleanPath(r.URL.Query().Get("path"))
	m, err := s.fsCall(r.Context(), ss, "fs_list", device.Args{"path": p})
	if err != nil {
		writeErr(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"path": p, "entries": m["entries"]})
}

func (s *Server) fsRead(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil || !s.fsGate(w, ss, "fs_read") {
		return
	}
	p := cleanPath(r.URL.Query().Get("path"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(p)))
	var off int64
	for {
		m, err := s.fsCall(r.Context(), ss, "fs_read", device.Args{"path": p, "offset": off, "len": fsChunk})
		if err != nil {
			if off == 0 {
				w.Header().Del("Content-Disposition")
				writeErr(w, 422, err)
			}
			return
		}
		b, _ := base64.StdEncoding.DecodeString(fmt.Sprint(m["data_b64"]))
		_, _ = w.Write(b)
		off += int64(len(b))
		if eof, _ := m["eof"].(bool); eof || len(b) == 0 {
			return
		}
	}
}

// fsWrite: PUT body = file bytes, ?path=/dir/name. Written in chunks; the first truncates.
func (s *Server) fsWrite(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil || !s.fsGate(w, ss, "fs_write") {
		return
	}
	p := cleanPath(r.URL.Query().Get("path"))
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	buf := make([]byte, fsChunk)
	var off int64
	for {
		n, rerr := io.ReadFull(r.Body, buf)
		if n > 0 || off == 0 {
			_, err := s.fsCall(r.Context(), ss, "fs_write", device.Args{
				"path": p, "offset": off, "truncate": off == 0, "data_b64": base64.StdEncoding.EncodeToString(buf[:n])})
			if err != nil {
				writeErr(w, 422, err)
				return
			}
			off += int64(n)
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrUnexpectedEOF) {
				writeErr(w, 400, rerr)
				return
			}
			break
		}
	}
	writeJSON(w, 200, map[string]any{"path": p, "written": off})
}

func (s *Server) fsDelete(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil || !s.fsGate(w, ss, "fs_delete") {
		return
	}
	p := cleanPath(r.URL.Query().Get("path"))
	if _, err := s.fsCall(r.Context(), ss, "fs_delete", device.Args{"path": p}); err != nil {
		writeErr(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": p})
}

// ---- terminals: the phone's phetty ttys, reached through phfs.exe (tty_* ops). The web terminal is a WebSocket:
// binary frames = raw tty bytes both ways; text frames = JSON control. Server -> client control:
// {"type":"tty","id":N} once attached, {"type":"error","error":"..."} when phetty is not reachable (then the socket closes).
// Client -> server: {"type":"resize","cols":N,"rows":M}. The bridge polls tty_read (quick after output or input,
// backing off to 1 s when idle); the polls run as the quiet caller, so they stay out of the op history.

func (s *Server) ttyList(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	if !hasOp(ss, "tty_list") {
		writeJSON(w, 200, map[string]any{"implemented": false, "ttys": []any{}})
		return
	}
	start := r.URL.Query().Get("start") == "1"
	m, err := s.fsCall(r.Context(), ss, "tty_list", device.Args{"start": start})
	if err != nil {
		writeJSON(w, 200, map[string]any{"implemented": true, "running": false, "error": err.Error(), "ttys": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"implemented": true, "running": true, "ttys": m["ttys"]})
}

// ttyBridge: /tty/{n}; n = 0 (or "new") opens a new tty. ?start=1 starts phettyd if needed.
func (s *Server) ttyBridge(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Hour)
	defer cancel()
	ctl := func(v map[string]any) error {
		b, _ := json.Marshal(v)
		return c.Write(ctx, websocket.MessageText, b)
	}
	fail := func(err error) {
		_ = ctl(map[string]any{"type": "error", "error": err.Error()})
		c.Close(websocket.StatusNormalClosure, "")
	}
	call := func(op string, a device.Args) (map[string]any, error) {
		res, err := s.Exec(ctx, ss, core.QuietCaller, op, a)
		if err != nil {
			return nil, err
		}
		m, _ := res.(map[string]any)
		return m, nil
	}
	if !hasOp(ss, "tty_read") {
		fail(fmt.Errorf("device %q has no terminal ops", ss.ID))
		return
	}
	if _, err := call("tty_list", device.Args{"start": r.URL.Query().Get("start") == "1"}); err != nil {
		fail(err)
		return
	}
	id, _ := strconv.Atoi(r.PathValue("tty"))
	// a resize restarts the tty's shell (eshell reads its console size once): only a tty opened by this socket follows
	// the browser's size; attaching to an existing one keeps its shell and size
	ours := id <= 0
	if id <= 0 {
		m, err := call("tty_new", device.Args{})
		if err != nil {
			fail(err)
			return
		}
		id, _ = m["id"].(int)
	}
	if err := ctl(map[string]any{"type": "tty", "id": id}); err != nil {
		return
	}

	input := make(chan []byte, 64)
	sizes := make(chan [2]int, 16)
	go func() { // reader: keys and control frames from the browser
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				var m struct {
					Type       string
					Cols, Rows int
				}
				if json.Unmarshal(data, &m) == nil && m.Type == "resize" && m.Cols > 0 && m.Rows > 0 && ours {
					select {
					case sizes <- [2]int{m.Cols, m.Rows}:
					default:
					}
				}
				continue
			}
			select {
			case input <- data:
			case <-ctx.Done():
				return
			}
		}
	}()

	var from uint32
	var size, want [2]int
	var wantAt time.Time
	delay := 50 * time.Millisecond
	for ctx.Err() == nil {
		// apply the browser's size once it has been stable for half a second (a drag sends many)
		if want != size && time.Since(wantAt) > 500*time.Millisecond {
			if _, err := call("tty_resize", device.Args{"id": id, "cols": want[0], "rows": want[1]}); err == nil {
				size = want
			}
		}
		select {
		case z := <-sizes:
			want, wantAt = z, time.Now()
			delay = 100 * time.Millisecond
			continue
		case b := <-input:
			for len(b) > 0 {
				n := min(len(b), fsChunk)
				if _, err := call("tty_write", device.Args{"id": id, "text": string(b[:n])}); err != nil {
					fail(err)
					return
				}
				b = b[n:]
			}
			delay = 30 * time.Millisecond
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
		m, err := call("tty_read", device.Args{"id": id, "from": from})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if strings.Contains(err.Error(), "not found") {
				fail(fmt.Errorf("tty %d is gone (phetty stopped or the phone rebooted)", id))
				return
			}
			delay = time.Second // link busy (a push, a screenshot): try again later
			continue
		}
		b, _ := m["bytes"].([]byte)
		if next, ok := m["next"].(uint32); ok {
			from = next
		}
		if len(b) > 0 {
			if c.Write(ctx, websocket.MessageBinary, b) != nil {
				return
			}
			delay = 30 * time.Millisecond
		} else if delay < time.Second {
			delay = min(delay*2, time.Second)
		}
	}
}
