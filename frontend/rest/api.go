// Package api is the REST + WebSocket surface (see api/README in docs). Every device action goes through
// POST /api/devices/{id}/ops/{op}, so the op list (GET .../ops) is the whole contract; the web UI and the MCP
// server are generated from it.
package rest

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

type Server struct {
	M     *core.Manager
	Token string
}

// Handler returns the API mux. extra is mounted for everything else (the SPA, /mcp).
func (s *Server) Handler(extra map[string]http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/devices", s.devices)
	mux.HandleFunc("GET /api/devices/{id}", s.deviceOne)
	mux.HandleFunc("GET /api/devices/{id}/ops", s.ops)
	mux.HandleFunc("POST /api/devices/{id}/ops/{op}", s.exec)
	mux.HandleFunc("GET /api/devices/{id}/frame", s.frame)
	mux.HandleFunc("GET /api/devices/{id}/lines", s.lines)
	mux.HandleFunc("GET /api/devices/{id}/events", s.events)
	mux.HandleFunc("GET /api/devices/{id}/boots", s.boots)
	mux.HandleFunc("GET /api/devices/{id}/history", s.history)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("GET /api/devices/{id}/stream", s.stream)
	mux.HandleFunc("POST /api/blobs", s.putBlob)
	mux.HandleFunc("GET /api/blobs", s.listBlobs)
	mux.HandleFunc("GET /api/blobs/{sha}", s.getBlob)
	mux.HandleFunc("GET /api/blobs/{sha}/meta", s.blobMeta)
	mux.HandleFunc("POST /api/pin", s.pin)
	s.mountFS(mux)
	for p, h := range extra {
		mux.Handle(p, h)
	}
	return s.auth(logRequests(mux))
}

func (s *Server) auth(h http.Handler) http.Handler {
	if s.Token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/mcp") {
			h.ServeHTTP(w, r) // the SPA itself is public; it asks for the token
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.URL.Query().Get("token") // WebSocket from a browser cannot set headers
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
			writeErr(w, http.StatusUnauthorized, errors.New("bad or missing token"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) *core.Session {
	ss := s.M.Get(r.PathValue("id"))
	if ss == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no device %q", r.PathValue("id")))
	}
	return ss
}

func qInt(r *http.Request, k string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(k), 10, 64)
	return v
}

// ---- status

// HealthReport is the library's (core.HealthReport).
type HealthReport = core.HealthReport

func (s *Server) Health() HealthReport { return s.M.Health() }

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	h := s.Health()
	code := http.StatusOK
	if !h.OK {
		code = http.StatusServiceUnavailable // the service is up; a device is not (monitors read the body)
	}
	writeJSON(w, code, h)
}

type DeviceInfo struct {
	ID     string          `json:"id"`
	Driver string          `json:"driver"`
	Health core.Health     `json:"health"`
	Ops    []device.OpSpec `json:"ops"`
}

func info(ss *core.Session) DeviceInfo {
	return DeviceInfo{ID: ss.ID, Driver: ss.Driver.Name(), Health: ss.Health(), Ops: ss.Ops()}
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	out := []DeviceInfo{}
	for _, ss := range s.M.All() {
		out = append(out, info(ss))
	}
	writeJSON(w, 200, out)
}

func (s *Server) deviceOne(w http.ResponseWriter, r *http.Request) {
	if ss := s.session(w, r); ss != nil {
		writeJSON(w, 200, info(ss))
	}
}

func (s *Server) ops(w http.ResponseWriter, r *http.Request) {
	if ss := s.session(w, r); ss != nil {
		writeJSON(w, 200, ss.Ops())
	}
}

// ---- ops

// Exec runs an op through the library (core.Manager.Exec).
func (s *Server) Exec(ctx context.Context, ss *core.Session, caller, op string, args device.Args) (any, error) {
	return s.M.Exec(ctx, ss, caller, op, args)
}

func (s *Server) exec(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	args := device.Args{}
	if r.ContentLength != 0 {
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil && err != io.EOF {
			writeErr(w, 400, fmt.Errorf("body must be a JSON object of args: %w", err))
			return
		}
	}
	for k, v := range args { // json.Number -> float64/string handled by Args
		if n, ok := v.(json.Number); ok {
			args[k] = n
		}
	}
	caller := r.Header.Get("X-Phonelab-Caller")
	if caller == "" {
		caller = "api"
	}
	res, err := s.Exec(r.Context(), ss, caller, r.PathValue("op"), args)
	if err != nil {
		writeJSON(w, 422, map[string]any{"error": err.Error(), "result": res})
		return
	}
	writeJSON(w, 200, map[string]any{"result": res})
}

// frame is the live view: GET .../frame?which=2[&store=1] -> image/png straight back (not stored, not in the op
// history unless store=1). which as for the screenshot op (default 2 = the whole screen as seen). Headers carry
// the size and how long the capture took. 409 when the link is busy (a push) -- the client just tries later.
func (s *Server) frame(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	which := float64(2)
	if v := r.URL.Query().Get("which"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, 400, fmt.Errorf("which: %w", err))
			return
		}
		which = float64(n)
	}
	store := r.URL.Query().Get("store") == "1"
	caller := core.QuietCaller
	if store {
		caller = "web"
	}
	// the live view sends only changed tiles over the link (protocol 3); ?full=1 forces the old whole-screen capture
	args := device.Args{"which": which, "store": store}
	if !store && r.URL.Query().Get("full") != "1" {
		args["live"] = true
	}
	res, err := s.Exec(r.Context(), ss, caller, "screenshot", args)
	if err != nil {
		code := 502
		if strings.Contains(err.Error(), "busy") || errors.Is(err, context.DeadlineExceeded) {
			code = 409
		}
		writeErr(w, code, err)
		return
	}
	type shot interface{ PNGBytes() []byte }
	b, _ := json.Marshal(res)
	var meta struct {
		Blob    string  `json:"blob"`
		Width   int     `json:"width"`
		Height  int     `json:"height"`
		Seconds float64 `json:"seconds"`
		Wire    int     `json:"wire_bytes"`
		Tiles   int     `json:"tiles"`
		Key     bool    `json:"key"`
	}
	json.Unmarshal(b, &meta)
	var png []byte
	if p, ok := res.(shot); ok {
		png = p.PNGBytes()
	}
	if png == nil && meta.Blob != "" { // stored: serve the blob
		rc, _, err := s.M.Store.OpenBlob(meta.Blob)
		if err == nil {
			png, _ = io.ReadAll(rc)
			rc.Close()
		}
		w.Header().Set("X-Blob", meta.Blob)
	}
	if png == nil {
		writeErr(w, 500, errors.New("the driver returned no image"))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Width", strconv.Itoa(meta.Width))
	w.Header().Set("X-Frame-Height", strconv.Itoa(meta.Height))
	w.Header().Set("X-Frame-Seconds", strconv.FormatFloat(meta.Seconds, 'f', 3, 64))
	w.Header().Set("X-Frame-Wire", strconv.Itoa(meta.Wire))   // bytes that crossed the USB link for this frame
	w.Header().Set("X-Frame-Tiles", strconv.Itoa(meta.Tiles)) // live view: tiles carried (0 on an unchanged screen)
	w.Header().Set("X-Frame-Key", strconv.FormatBool(meta.Key))
	w.Header().Set("Access-Control-Expose-Headers", "X-Frame-Width, X-Frame-Height, X-Frame-Seconds, X-Frame-Wire, X-Frame-Tiles, X-Frame-Key, X-Blob")
	w.Write(png)
}

// ---- history

func (s *Server) lines(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	q := r.URL.Query()
	out, err := s.M.Store.Lines(store.LineQuery{Device: ss.ID, Boot: qInt(r, "boot"), After: qInt(r, "after"),
		Before: qInt(r, "before"), Grep: q.Get("grep"), Limit: int(qInt(r, "limit"))})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if out == nil {
		out = []store.Line{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	var kinds []string
	if k := r.URL.Query().Get("kind"); k != "" {
		kinds = strings.Split(k, ",")
	}
	out, err := s.M.Store.Events(store.EventQuery{Device: ss.ID, Boot: qInt(r, "boot"), Kinds: kinds,
		Before: qInt(r, "before"), Limit: int(qInt(r, "limit"))})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if out == nil {
		out = []store.Event{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) boots(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	out, err := s.M.Store.Boots(ss.ID, qInt(r, "before"), int(qInt(r, "limit")))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if out == nil {
		out = []store.Boot{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	out, err := s.M.Store.Ops(ss.ID, qInt(r, "before"), int(qInt(r, "limit")))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if out == nil {
		out = []store.Op{}
	}
	writeJSON(w, 200, out)
}

// ---- blobs

func (s *Server) putBlob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, 400, fmt.Errorf("want multipart/form-data with a 'file' part: %w", err))
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "image"
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		if part.FormName() != "file" {
			continue
		}
		b, err := s.M.Store.PutBlob(kind, part.FileName(), r.URL.Query().Get("device"), 0, map[string]any{"source": "upload"}, part)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		writeJSON(w, 200, b)
		return
	}
	writeErr(w, 400, errors.New("no 'file' part"))
}

func (s *Server) listBlobs(w http.ResponseWriter, r *http.Request) {
	out, err := s.M.Store.Blobs(r.URL.Query().Get("kind"), int(qInt(r, "limit")))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if out == nil {
		out = []store.Blob{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) blobMeta(w http.ResponseWriter, r *http.Request) {
	b, err := s.M.Store.GetBlob(r.PathValue("sha"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) getBlob(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha")
	b, err := s.M.Store.GetBlob(sha)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	rc, size, err := s.M.Store.OpenBlob(sha)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	defer rc.Close()
	ct := mime.TypeByExtension(filepath.Ext(b.Name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": b.Name}))
	}
	_, _ = io.Copy(w, rc)
}

func (s *Server) pin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		What string `json:"what"`
		ID   string `json:"id"`
		On   bool   `json:"on"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.M.Store.Pin(req.What, req.ID, req.On); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- live stream

// stream is a WebSocket of core.Msg JSON. ?types=line,event,state,progress,op filters (default all).
// The first message is a "hello" with the current health of the device(s).
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	dev := r.PathValue("id")
	if dev != "" && s.M.Get(dev) == nil {
		writeErr(w, 404, fmt.Errorf("no device %q", dev))
		return
	}
	types := map[string]bool{}
	if t := r.URL.Query().Get("types"); t != "" {
		for _, x := range strings.Split(t, ",") {
			types[x] = true
		}
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // LAN tool; origin not checked
	if err != nil {
		return
	}
	defer c.CloseNow()
	sub := s.M.Bus.Subscribe(4096)
	defer sub.Close()
	ctx := c.CloseRead(r.Context())
	hello, _ := json.Marshal(core.Msg{Type: "hello", Device: dev, Data: s.Health()})
	if err := c.Write(ctx, websocket.MessageText, hello); err != nil {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			pc, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pc)
			cancel()
			if err != nil {
				return
			}
		case m := <-sub.C:
			if dev != "" && m.Device != dev {
				continue
			}
			if len(types) > 0 && !types[m.Type] {
				continue
			}
			b, _ := json.Marshal(m)
			wc, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Write(wc, websocket.MessageText, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
