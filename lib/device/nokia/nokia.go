// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package nokia drives stock Nokia Symbian^3-family phones through an on-phone debug agent that speaks
// the "phedbg line protocol v1" (docs/nokia.md). The agent is either phedbg (a native exe, USB CDC ACM or TCP) or
// a PyS60 script (TCP). The phone is someone's own phone: the agent only runs while its owner has it open, and
// every request carries the pairing token the agent shows on screen.
//
// Wire format (text lines, '\n'-terminated, values percent-encoded: '%', ' ', '=', CR, LF):
//
//	host -> phone   <id> <op> tok=<token> [k=v ...]
//	                <id> d <base64>                  data for fs_write (after the request line)
//	phone -> host   @<id> ok [k=v ...]               final reply
//	                @<id> err code=<code> [msg=...]  final error (code: unsupported, denied, auth, notfound, io, badarg)
//	                @<id> e k=v ...                  one entry (fs_list, ps)
//	                @<id> d <base64>                 one data chunk (fs_read, screenshot)
//	                @0 hello proto=1 agent=<name> ver=<v> ops=<a,b,..>   on connect
//	                # <text>                         a log line (anything else is a log line too)
package nokia

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/transport"
)

func init() {
	device.Register("nokia", func(cfg map[string]any) (device.Driver, error) { return New(cfg) })
}

// ChunkBytes is the raw size of one base64 data line (4096 chars encoded, under the Session's 8 KiB line cap).
const ChunkBytes = 3072

// Driver is the nokia device family.
type Driver struct {
	Token          string
	Timeout        time.Duration
	KeepAliveEvery time.Duration // protocol keep-alive ("ping" op); default 20 s on tcp, off on serial (keepalive_s = 0 turns it off)
	opener         transport.Opener
	id             atomic.Uint32
}

// New reads the [device.opts] section: token (the pairing code), transport ("serial" default | "tcp"),
// dial / listen (tcp), timeout_s.
func New(cfg map[string]any) (*Driver, error) {
	d := &Driver{Timeout: 30 * time.Second}
	str := func(k string) string { s, _ := cfg[k].(string); return s }
	d.Token = str("token")
	if v, ok := cfg["timeout_s"].(int64); ok && v > 0 {
		d.Timeout = time.Duration(v) * time.Second
	}
	switch t := str("transport"); t {
	case "", "serial":
	case "tcp":
		d.KeepAliveEvery = 20 * time.Second
		if (str("dial") == "") == (str("listen") == "") {
			return nil, fmt.Errorf("nokia: transport tcp needs exactly one of dial / listen")
		}
		d.opener = &transport.TCPOpener{Dial: str("dial"), Listen: str("listen")}
	default:
		return nil, fmt.Errorf("nokia: unknown transport %q (serial | tcp)", t)
	}
	if v, ok := cfg["keepalive_s"].(int64); ok {
		d.KeepAliveEvery = time.Duration(v) * time.Second
	}
	return d, nil
}

// KeepAlive makes the session ping the agent on an idle link (device.KeepAliver).
func (d *Driver) KeepAlive() (time.Duration, func() []byte) {
	if d.KeepAliveEvery <= 0 {
		return 0, nil
	}
	return d.KeepAliveEvery, func() []byte { return []byte(fmt.Sprintf("%d ping\n", d.id.Add(1))) }
}

func (*Driver) Name() string { return "nokia" }

// Opener returns the TCP opener when configured (nil = the service's default serial opener).
func (d *Driver) Opener() transport.Opener { return d.opener }

func p(name, typ, desc string, req bool) device.Param {
	return device.Param{Name: name, Type: typ, Description: desc, Required: req}
}

func (*Driver) Ops() []device.OpSpec {
	path := p("path", "string", `phone path, e.g. c:\data\phedbg\x.txt`, true)
	return []device.OpSpec{
		{Name: "info", Capability: "status", Description: "Agent name/version, phone model string, free RAM, drives."},
		{Name: "fs_list", Capability: "files", Description: "List a directory on the phone.", Params: []device.Param{path}},
		{Name: "fs_read", Capability: "files", Description: "Read a file from the phone (any readable path).",
			Params: []device.Param{path, p("out", "file", "save to this server path instead of returning text", false)}},
		{Name: "fs_write", Capability: "files", Description: "Write a file (only inside the agent's write allow-list).",
			Params: []device.Param{path, p("file", "file", "server-side source file", false), p("blob", "blob", "sha256 of an uploaded blob", false),
				p("text", "string", "literal contents", false)}},
		{Name: "fs_delete", Capability: "files", Description: "Delete a file (only inside the write allow-list).", Params: []device.Param{path}},
		{Name: "exec", Capability: "process", Description: "Start an exe on the phone (RProcess); wait=1 waits for its exit reason.",
			Params: []device.Param{path, p("args", "string", "command line", false), p("wait", "bool", "wait for exit", false)}},
		{Name: "ps", Capability: "process", Description: "List running processes."},
		{Name: "log", Capability: "status", Description: "The agent's session log (every request and its verdict)."},
		{Name: "screenshot", Capability: "display", Description: "Capture the screen (PNG or BMP from the agent).",
			Params: []device.Param{p("out", "file", "save to this server path", false)}},
		{Name: "key", Capability: "input", Description: "Send a key event (if the agent's platform allows; else 'unsupported').",
			Params: []device.Param{p("code", "int", "Symbian key/scan code", true)}},
		{Name: "touch", Capability: "input", Description: "Tap at x,y (if the agent's platform allows; else 'unsupported').",
			Params: []device.Param{p("x", "int", "", true), p("y", "int", "", true)}},
	}
}

// Error is an agent-side failure. Code "unsupported" means the agent or platform cannot do the op.
type Error struct{ Code, Msg string }

func (e *Error) Error() string {
	if e.Msg == "" {
		return "phone: " + e.Code
	}
	return "phone: " + e.Code + ": " + e.Msg
}

// IsUnsupported reports whether err is the agent saying "unsupported".
func IsUnsupported(err error) bool { e, ok := err.(*Error); return ok && e.Code == "unsupported" }

// Enc percent-encodes a value for the wire.
func Enc(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' || c == ' ' || c == '=' || c == '\r' || c == '\n' || c < 0x20 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Dec undoes Enc.
func Dec(s string) string {
	v, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return v
}

// KV parses "k=v k=v" fields.
func KV(fields []string) map[string]string {
	m := map[string]string{}
	for _, f := range fields {
		if k, v, ok := strings.Cut(f, "="); ok {
			m[k] = Dec(v)
		}
	}
	return m
}

// Reply is a finished request.
type Reply struct {
	KV      map[string]string
	Entries []map[string]string
	Data    []byte
}

var reHello = regexp.MustCompile(`^@0 hello `)

// Call sends one request (plus optional data lines) and collects the reply.
func (d *Driver) Call(ctx context.Context, l device.Link, op string, kv [][2]string, data []byte) (*Reply, error) {
	id := d.id.Add(1)
	if id == 0 {
		id = d.id.Add(1)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s tok=%s", id, op, Enc(d.Token))
	for _, f := range kv {
		fmt.Fprintf(&b, " %s=%s", f[0], Enc(f[1]))
	}
	b.WriteByte('\n')
	for off := 0; off < len(data); off += ChunkBytes {
		end := min(off+ChunkBytes, len(data))
		fmt.Fprintf(&b, "%d d %s\n", id, base64.StdEncoding.EncodeToString(data[off:end]))
	}
	ctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	m := l.Mark()
	if err := l.Write(ctx, []byte(b.String())); err != nil {
		return nil, err
	}
	// Wait for the final line, then collect this request's entry/data lines from the ring in order.
	fin := regexp.MustCompile(fmt.Sprintf(`^@%d (?:ok|err)(?: |$)`, id))
	if _, err := l.WaitLine(ctx, m, fin); err != nil {
		return nil, fmt.Errorf("%s: no reply: %w", op, err)
	}
	re := regexp.MustCompile(fmt.Sprintf(`^@%d (ok|err|e|d)(?: (.*))?$`, id))
	r := &Reply{}
	var raw bytes.Buffer
	for _, line := range l.LinesSince(m, 1<<20) {
		sm := re.FindStringSubmatch(line)
		if sm == nil {
			continue
		}
		rest := sm[2]
		switch sm[1] {
		case "ok":
			r.KV = KV(strings.Fields(rest))
			r.Data = raw.Bytes()
			return r, nil
		case "err":
			kv := KV(strings.Fields(rest))
			return nil, &Error{Code: kv["code"], Msg: kv["msg"]}
		case "e":
			r.Entries = append(r.Entries, KV(strings.Fields(rest)))
		case "d":
			chunk, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rest))
			if err != nil {
				return nil, fmt.Errorf("%s: bad data line: %w", op, err)
			}
			raw.Write(chunk)
		}
	}
	return nil, fmt.Errorf("%s: reply fell out of the line ring", op)
}

func (d *Driver) Exec(ctx context.Context, l device.Link, op string, a device.Args) (any, error) {
	if device.FindOp(d, op) == nil {
		return nil, &Error{Code: "unsupported", Msg: op}
	}
	s := func(k string) string { v, _ := a.String(k); return v }
	var kv [][2]string
	for _, k := range []string{"path", "args", "code", "x", "y"} {
		if a.Has(k) {
			kv = append(kv, [2]string{k, s(k)})
		}
	}
	if a.Has("wait") && (s("wait") == "true" || s("wait") == "1") {
		kv = append(kv, [2]string{"wait", "1"})
	}
	var data []byte
	if op == "fs_write" {
		var err error
		switch {
		case a.Has("text"):
			data = []byte(s("text"))
		case a.Has("file"):
			data, err = os.ReadFile(s("file"))
		case a.Has("blob"):
			var rc io.ReadCloser
			if rc, _, err = l.Blob(s("blob")); err == nil {
				data, err = io.ReadAll(rc)
				rc.Close()
			}
		default:
			err = fmt.Errorf("fs_write needs text, file or blob")
		}
		if err != nil {
			return nil, err
		}
		kv = append(kv, [2]string{"size", strconv.Itoa(len(data))})
	}
	r, err := d.Call(ctx, l, op, kv, data)
	if err != nil {
		return nil, err
	}
	switch op {
	case "fs_list", "ps", "log":
		return map[string]any{"entries": r.Entries, "info": r.KV}, nil
	case "fs_read", "screenshot":
		name := "file"
		if op == "screenshot" {
			name = "screenshot." + r.KV["fmt"]
		}
		res := map[string]any{"size": len(r.Data), "info": r.KV}
		if sha, err := l.PutBlob(op, name, r.KV, r.Data); err == nil {
			res["blob"] = sha
		}
		if out := s("out"); out != "" {
			if err := os.WriteFile(out, r.Data, 0o644); err != nil {
				return nil, err
			}
			res["out"] = out
		} else if op == "fs_read" && isText(r.Data) {
			res["text"] = string(r.Data)
		}
		return res, nil
	}
	return r.KV, nil
}

func isText(b []byte) bool {
	if len(b) > 64<<10 {
		return false
	}
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return false
		}
	}
	return true
}

// BootStart: the agent's hello marks a new session (the phone itself does not reboot under us).
func (*Driver) BootStart(line string) bool { return reHello.MatchString(line) }

func (*Driver) Classify(line string) []device.Event {
	if reHello.MatchString(line) {
		return []device.Event{{Kind: "agent.hello", Data: map[string]any{"kv": KV(strings.Fields(line)[2:])}}}
	}
	if strings.HasPrefix(line, "# ") {
		return []device.Event{{Kind: "agent.log", Data: map[string]any{"text": line[2:]}}}
	}
	return nil
}
