// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pinephone

// File access on the phone (the IDE view's Project panel): phlink's 'F' command. The PC sends {len LE32, op LE32}
// then len payload bytes; the kernel link hands it to the user-side helper phfs.exe through the PhFs.A64 LDD
// (phfs_api.h in adaptation-a64 (usbcc/) defines the payloads) and the answer comes back as blob kind 4:
// {status LE32 (a Symbian error code, 0 = ok), data}.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const (
	blobFS = 4

	fsOpList   = 1
	fsOpRead   = 2
	fsOpWrite  = 3
	fsOpDelete = 4

	// the terminal requests: phfs.exe bridges them to the phetty daemon's ttys
	ttyOpList   = 5
	ttyOpNew    = 6
	ttyOpRead   = 7
	ttyOpWrite  = 8
	ttyOpResize = 9
	ttyOpKill   = 10

	fsMaxChunk = 3072 // a write chunk / read length the link takes in one request
	// fsHeaderGap: between a file request's header and its payload, for a log transfer in flight to finish (a 1 KB
	// bulk IN takes well under a millisecond at high speed; 20 ms leaves room for a busy DFC thread)
	fsHeaderGap = 20 * time.Millisecond
)

func fsOps() []device.OpSpec {
	return []device.OpSpec{
		{Name: "fs_list", Capability: "files",
			Description: "List a directory on the phone (needs caps 'fs': phfs.exe in the image). path = \"/\" lists the drives, " +
				"else \"/C:/dir\". Returns {entries:[{name,type:\"d\"|\"f\",size,mtime}]}.",
			Params: []device.Param{{Name: "path", Type: "string", Required: true, Description: "/ or /C:/dir"}}},
		{Name: "fs_read", Capability: "files",
			Description: "Read len (default and max 3072) bytes of a file from offset. Returns {data_b64, eof}.",
			Params: []device.Param{
				{Name: "path", Type: "string", Required: true},
				{Name: "offset", Type: "int", Description: "default 0"},
				{Name: "len", Type: "int", Description: "default/max 3072"},
			}},
		{Name: "fs_write", Capability: "files",
			Description: "Write data_b64 (max 3072 bytes) into a file at offset; truncate = replace the file first (creates the " +
				"folder). Returns {written}.",
			Params: []device.Param{
				{Name: "path", Type: "string", Required: true},
				{Name: "offset", Type: "int", Description: "default 0"},
				{Name: "data_b64", Type: "string", Required: true},
				{Name: "truncate", Type: "bool", Description: "replace the file (the first chunk of an upload)"},
			}},
		{Name: "fs_delete", Capability: "files",
			Description: "Delete a file, or a folder with everything in it. A whole drive is refused.",
			Params:      []device.Param{{Name: "path", Type: "string", Required: true, Description: "/C:/dir/file"}}},
		{Name: "tty_list", Capability: "tty",
			Description: "The phetty daemon's ttys: {ttys:[{id,cols,rows,shell,end,title}]}. Fails with \"not found\" when phettyd " +
				"is not running; start = start it.",
			Params: []device.Param{{Name: "start", Type: "bool", Description: "start phettyd if it is not running"}}},
		{Name: "tty_new", Capability: "tty", Description: "Open a new tty (a shell). Returns {id}.",
			Params: []device.Param{{Name: "cols", Type: "int"}, {Name: "rows", Type: "int"}}},
		{Name: "tty_read", Capability: "tty",
			Description: "Terminal output of a tty from sequence number from (0 = the whole scrollback). Returns {seq, next, text}.",
			Params:      []device.Param{{Name: "id", Type: "int", Required: true}, {Name: "from", Type: "int"}}},
		{Name: "tty_write", Capability: "tty",
			Description: "Type into a tty: raw terminal input (\r = Enter; ANSI arrows).",
			Params:      []device.Param{{Name: "id", Type: "int", Required: true}, {Name: "text", Type: "string", Required: true}}},
		{Name: "tty_resize", Capability: "tty", Description: "Resize a tty (restarts its shell).",
			Params: []device.Param{{Name: "id", Type: "int", Required: true}, {Name: "cols", Type: "int", Required: true},
				{Name: "rows", Type: "int", Required: true}}},
		{Name: "tty_kill", Capability: "tty", Description: "Close a tty and its shell.",
			Params: []device.Param{{Name: "id", Type: "int", Required: true}}},
	}
}

// symbianErr names the codes a file request can fail with.
func symbianErr(code int32) string {
	names := map[int32]string{
		-1: "not found", -2: "general error", -4: "out of memory", -5: "not supported", -6: "bad argument",
		-9: "bad name", -11: "in use", -12: "access denied", -14: "access denied", -15: "path not found", -16: "already exists",
		-18: "phfs.exe is not running on the phone (is it in the image?)", -21: "access denied", -22: "disk full", -29: "aborted",
		-40: "too big", -46: "permission denied", -47: "request too big",
	}
	if n, ok := names[code]; ok {
		return fmt.Sprintf("%s (%d)", n, code)
	}
	return strconv.Itoa(int(code))
}

// fsRequest sends one 'F' request and waits for the kind-4 blob; returns the data after the status.
func (d *Driver) fsRequest(ctx context.Context, l device.Link, op uint32, payload []byte) ([]byte, error) {
	frames, stop := l.Frames()
	defer stop()
	// the header alone first, as a push does: the phone switches to receiving the request (phlink EUpFs) and holds its
	// log; a payload arriving in the same write met a log transfer still in flight, and concurrent IN/OUT at high speed
	// wedges the link with a core spinning (2026-10-20, ph package install: 3 KB writes, every few requests)
	if err := l.Write(ctx, cmd9('F', uint32(len(payload)), op)); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		time.Sleep(fsHeaderGap)
		if err := l.Write(ctx, payload); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	idle := time.NewTimer(8 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-idle.C:
			return nil, fmt.Errorf("no answer to the file request (an image without the 'fs' cap, or the link is busy)")
		case f := <-frames:
			if !f.SumOK {
				return nil, fmt.Errorf("file reply frame %d: checksum mismatch", f.Seq)
			}
			switch f.Type {
			case frameBlobData:
				buf.Write(f.Payload)
			case frameBlobEnd:
				if len(f.Payload) < 8 || binary.LittleEndian.Uint32(f.Payload) != blobFS {
					buf.Reset() // another blob (a profile in flight): not ours
					continue
				}
				if n := int(binary.LittleEndian.Uint32(f.Payload[4:])); n != buf.Len() || n < 4 {
					return nil, fmt.Errorf("file reply: got %d of %d bytes", buf.Len(), n)
				}
				b := buf.Bytes()
				if st := int32(binary.LittleEndian.Uint32(b)); st != 0 {
					return nil, fmt.Errorf("phone: %s", symbianErr(st))
				}
				return b[4:], nil
			}
		}
	}
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func (d *Driver) execFS(ctx context.Context, l device.Link, op string, a device.Args) (any, bool, error) {
	switch op {
	case "fs_list":
		p, err := a.String("path")
		if err != nil {
			return nil, true, err
		}
		data, err := d.fsRequest(ctx, l, fsOpList, []byte(p))
		if err != nil {
			return nil, true, err
		}
		entries := []map[string]any{}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.SplitN(line, " ", 4)
			if len(f) != 4 {
				continue
			}
			size, _ := strconv.ParseInt(f[1], 10, 64)
			mt, _ := strconv.ParseInt(f[2], 10, 64)
			entries = append(entries, map[string]any{"name": f[3], "type": f[0], "size": size, "mtime": mt})
		}
		return map[string]any{"entries": entries}, true, nil
	case "fs_read":
		p, err := a.String("path")
		if err != nil {
			return nil, true, err
		}
		off, err := a.UintOr("offset", 32, 0)
		if err != nil {
			return nil, true, err
		}
		n, err := a.UintOr("len", 32, fsMaxChunk)
		if err != nil {
			return nil, true, err
		}
		if n > fsMaxChunk {
			n = fsMaxChunk
		}
		payload := append(append(le32(uint32(off)), le32(uint32(n))...), p...)
		data, err := d.fsRequest(ctx, l, fsOpRead, payload)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data), "eof": uint64(len(data)) < n}, true, nil
	case "fs_write":
		p, err := a.String("path")
		if err != nil {
			return nil, true, err
		}
		off, err := a.UintOr("offset", 32, 0)
		if err != nil {
			return nil, true, err
		}
		b64, err := a.String("data_b64")
		if err != nil {
			return nil, true, err
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, true, fmt.Errorf("data_b64: %v", err)
		}
		if len(data) > fsMaxChunk {
			return nil, true, fmt.Errorf("at most %d bytes per write", fsMaxChunk)
		}
		flags := uint32(0)
		if t, _ := a["truncate"].(bool); t {
			flags = 1
		}
		payload := append(append(append(le32(uint32(off)), le32(flags)...), le32(uint32(len(p)))...), p...)
		payload = append(payload, data...)
		res, err := d.fsRequest(ctx, l, fsOpWrite, payload)
		if err != nil {
			return nil, true, err
		}
		w := len(data)
		if len(res) >= 4 {
			w = int(binary.LittleEndian.Uint32(res))
		}
		return map[string]any{"written": w}, true, nil
	case "fs_delete":
		p, err := a.String("path")
		if err != nil {
			return nil, true, err
		}
		if _, err := d.fsRequest(ctx, l, fsOpDelete, []byte(p)); err != nil {
			return nil, true, err
		}
		return map[string]any{"deleted": p}, true, nil
	case "tty_list", "tty_new", "tty_read", "tty_write", "tty_resize", "tty_kill":
		return d.execTty(ctx, l, op, a)
	}
	return nil, false, nil
}

// TtyInfo is one line of tty_list.
type TtyInfo struct {
	ID    int    `json:"id"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Shell int    `json:"shell"` // 0 dead, 1 running, 2 running and restarted
	End   uint32 `json:"end"`   // sequence number of the next byte
	Title string `json:"title"`
}

func (d *Driver) execTty(ctx context.Context, l device.Link, op string, a device.Args) (any, bool, error) {
	num := func(k string, def uint64) uint32 { v, _ := a.UintOr(k, 32, def); return uint32(v) }
	var payload []byte
	code := map[string]uint32{"tty_list": ttyOpList, "tty_new": ttyOpNew, "tty_read": ttyOpRead, "tty_write": ttyOpWrite,
		"tty_resize": ttyOpResize, "tty_kill": ttyOpKill}[op]
	switch op {
	case "tty_list":
		f := uint32(0)
		if v, _ := a["start"].(bool); v {
			f = 1
		}
		payload = le32(f)
	case "tty_new":
		payload = append(le32(num("cols", 0)), le32(num("rows", 0))...)
	case "tty_read":
		payload = append(le32(num("id", 0)), le32(num("from", 0))...)
	case "tty_write":
		t, err := a.String("text")
		if err != nil {
			return nil, true, err
		}
		if len(t) > fsMaxChunk {
			return nil, true, fmt.Errorf("at most %d bytes per write", fsMaxChunk)
		}
		payload = append(le32(num("id", 0)), t...)
	case "tty_resize":
		payload = append(append(le32(num("id", 0)), le32(num("cols", 80))...), le32(num("rows", 24))...)
	case "tty_kill":
		payload = le32(num("id", 0))
	}
	data, err := d.fsRequest(ctx, l, code, payload)
	if err != nil {
		if strings.HasPrefix(err.Error(), "phone: not supported") {
			return nil, true, fmt.Errorf("the phone's image predates the terminal service (phfs.exe without tty support): push a newer image")
		}
		if strings.HasPrefix(err.Error(), "phone: not found") && op == "tty_list" {
			return nil, true, fmt.Errorf("phetty is not running on the phone (start it, or the image was built with PHETTY=exclude)")
		}
		return nil, true, err
	}
	switch op {
	case "tty_list":
		ttys := []TtyInfo{}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.SplitN(line, " ", 6)
			if len(f) < 5 {
				continue
			}
			var t TtyInfo
			t.ID, _ = strconv.Atoi(f[0])
			t.Cols, _ = strconv.Atoi(f[1])
			t.Rows, _ = strconv.Atoi(f[2])
			t.Shell, _ = strconv.Atoi(f[3])
			e, _ := strconv.ParseUint(f[4], 10, 32)
			t.End = uint32(e)
			if len(f) == 6 {
				t.Title = f[5]
			}
			ttys = append(ttys, t)
		}
		return map[string]any{"ttys": ttys}, true, nil
	case "tty_new":
		if len(data) < 4 {
			return nil, true, fmt.Errorf("tty_new: short reply")
		}
		return map[string]any{"id": int(binary.LittleEndian.Uint32(data))}, true, nil
	case "tty_read":
		if len(data) < 4 {
			return nil, true, fmt.Errorf("tty_read: short reply")
		}
		seq := binary.LittleEndian.Uint32(data)
		b := data[4:]
		return map[string]any{"seq": seq, "next": seq + uint32(len(b)), "text": string(b), "bytes": b}, true, nil
	}
	return map[string]any{"ok": true}, true, nil
}
