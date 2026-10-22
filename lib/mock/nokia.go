// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/transport"
)

// NokiaAgent is a fake phedbg: an in-memory file system behind the phedbg line protocol v1 (docs/nokia.md).
type NokiaAgent struct {
	Token       string
	Allow       []string        // write allow-list prefixes (lower case, '\' separators)
	Unsupported map[string]bool // ops that answer "unsupported"
	Screen      []byte

	mu      sync.Mutex
	Files   map[string][]byte
	pending map[string]*pendingWrite
}

type pendingWrite struct {
	path string
	size int
	buf  []byte
}

func NewNokiaAgent(token string) *NokiaAgent {
	return &NokiaAgent{Token: token, Allow: []string{`c:\data\phedbg\`},
		Unsupported: map[string]bool{"key": true, "touch": true},
		Screen:      []byte("BM-fake-screen"),
		Files:       map[string][]byte{`c:\data\readme.txt`: []byte("hello\n")}, pending: map[string]*pendingWrite{}}
}

func normPath(p string) string { return strings.ToLower(strings.ReplaceAll(p, "/", `\`)) }

func enc(s string) string { return strings.ReplaceAll(url.PathEscape(s), "=", "%3D") }

// Hello is the line the agent sends on connect.
func (a *NokiaAgent) Hello() string { return "@0 hello proto=1 agent=mock ver=0" }

// Handle processes one host line and returns the reply lines.
func (a *NokiaAgent) Handle(line string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := strings.Fields(line)
	if len(f) < 2 {
		return nil
	}
	id, op := f[0], f[1]
	errl := func(code, msg string) []string { return []string{fmt.Sprintf("@%s err code=%s msg=%s", id, code, enc(msg))} }
	if op == "d" { // data for a pending fs_write
		pw := a.pending[id]
		if pw == nil || len(f) < 3 {
			return nil
		}
		b, err := base64.StdEncoding.DecodeString(f[2])
		if err != nil {
			delete(a.pending, id)
			return errl("badarg", "base64")
		}
		pw.buf = append(pw.buf, b...)
		if len(pw.buf) >= pw.size {
			delete(a.pending, id)
			a.Files[pw.path] = pw.buf
			return []string{fmt.Sprintf("@%s ok size=%d", id, len(pw.buf))}
		}
		return nil
	}
	kv := map[string]string{}
	for _, x := range f[2:] {
		if k, v, ok := strings.Cut(x, "="); ok {
			v, _ = url.PathUnescape(v)
			kv[k] = v
		}
	}
	if kv["tok"] != a.Token {
		return errl("auth", "bad pairing token")
	}
	if a.Unsupported[op] {
		return errl("unsupported", op)
	}
	path := normPath(kv["path"])
	allowed := func() bool {
		if strings.Contains(path, "..") {
			return false
		}
		for _, p := range a.Allow {
			if strings.HasPrefix(path, p) {
				return true
			}
		}
		return false
	}
	ok := func(rest string) string { return strings.TrimSpace(fmt.Sprintf("@%s ok %s", id, rest)) }
	switch op {
	case "info":
		return []string{ok("agent=mock ver=0 model=Mock ram_free=1000000")}
	case "fs_list":
		var out []string
		var names []string
		for k := range a.Files {
			if strings.HasPrefix(k, path) && !strings.Contains(k[len(path):], `\`) {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		for _, k := range names {
			out = append(out, fmt.Sprintf("@%s e name=%s size=%d dir=0", id, enc(k[len(path):]), len(a.Files[k])))
		}
		return append(out, ok(fmt.Sprintf("count=%d", len(names))))
	case "fs_read", "screenshot":
		data, found := a.Files[path], true
		if op == "screenshot" {
			data = a.Screen
		} else {
			_, found = a.Files[path]
		}
		if !found {
			return errl("notfound", path)
		}
		var out []string
		for off := 0; off < len(data); off += 3072 {
			out = append(out, fmt.Sprintf("@%s d %s", id, base64.StdEncoding.EncodeToString(data[off:min(off+3072, len(data))])))
		}
		extra := ""
		if op == "screenshot" {
			extra = " fmt=bmp w=360 h=640"
		}
		return append(out, ok(fmt.Sprintf("size=%d%s", len(data), extra)))
	case "fs_write":
		if !allowed() {
			return errl("denied", "outside the write allow-list")
		}
		n, _ := strconv.Atoi(kv["size"])
		if n == 0 {
			a.Files[path] = nil
			return []string{ok("size=0")}
		}
		a.pending[id] = &pendingWrite{path: path, size: n}
		return nil
	case "fs_delete":
		if !allowed() {
			return errl("denied", "outside the write allow-list")
		}
		if _, f := a.Files[path]; !f {
			return errl("notfound", path)
		}
		delete(a.Files, path)
		return []string{ok("")}
	case "ps":
		return []string{fmt.Sprintf("@%s e name=EKern.exe pid=1", id), fmt.Sprintf("@%s e name=phedbg.exe pid=42", id), ok("count=2")}
	case "exec":
		return []string{ok("pid=77")}
	}
	return errl("unsupported", op)
}

// Opener serves the agent over an in-memory pipe (like a TCP/ACM link).
func (a *NokiaAgent) Opener() transport.Opener { return agentOpener{a} }

type agentOpener struct{ a *NokiaAgent }

func (agentOpener) Describe() string { return "mock nokia agent" }

func (o agentOpener) Open(ctx context.Context) (transport.Port, error) {
	host, phone := net.Pipe()
	go func() {
		w := bufio.NewWriter(phone)
		fmt.Fprintln(w, o.a.Hello())
		w.Flush()
		sc := bufio.NewScanner(phone)
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			for _, l := range o.a.Handle(sc.Text()) {
				fmt.Fprintln(w, l)
			}
			w.Flush()
		}
		phone.Close()
	}()
	return &pipePort{c: host}, nil
}

type pipePort struct{ c net.Conn }

func (p *pipePort) Name() string { return "mock-nokia" }
func (p *pipePort) Read(b []byte) (int, error) {
	_ = p.c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	n, err := p.c.Read(b)
	if err != nil && errors.Is(err, os.ErrDeadlineExceeded) {
		return n, nil
	}
	return n, err
}
func (p *pipePort) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *pipePort) Close() error                { return p.c.Close() }
func (p *pipePort) Alive() bool                 { return true }
