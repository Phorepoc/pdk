// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package nokia_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/device/nokia"
	"github.com/Phorepoc/pdk/lib/mock"
	"github.com/Phorepoc/pdk/lib/store"
	"github.com/Phorepoc/pdk/lib/transport"
)

func session(t *testing.T, tok string, op transport.Opener) *core.Session {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, err := nokia.New(map[string]any{"token": tok, "timeout_s": int64(5)})
	if err != nil {
		t.Fatal(err)
	}
	m := core.NewManager(st)
	s := core.NewSession("n", d, op, st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; st.Close() })
	c, cc := context.WithTimeout(ctx, 5*time.Second)
	defer cc()
	if err := s.WaitConnected(c); err != nil {
		t.Fatal(err)
	}
	return s
}

func run(t *testing.T, s *core.Session, op string, a device.Args) (any, error) {
	c, cc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cc()
	return s.Exec(c, "test", op, a)
}

func TestMockAgent(t *testing.T) {
	ag := mock.NewNokiaAgent("4711")
	s := session(t, "4711", ag.Opener())

	if r, err := run(t, s, "info", device.Args{}); err != nil || r.(map[string]string)["agent"] != "mock" {
		t.Fatalf("info: %v %v", r, err)
	}
	big := strings.Repeat("0123456789abcdef", 1000) // spans several data lines
	if _, err := run(t, s, "fs_write", device.Args{"path": `C:\Data\phedbg\big.txt`, "text": big}); err != nil {
		t.Fatal("fs_write:", err)
	}
	r, err := run(t, s, "fs_read", device.Args{"path": `c:\data\phedbg\big.txt`})
	if err != nil || r.(map[string]any)["text"] != big {
		t.Fatalf("fs_read round trip failed: %v", err)
	}
	r, err = run(t, s, "fs_list", device.Args{"path": `c:\data\phedbg\`})
	if err != nil || len(r.(map[string]any)["entries"].([]map[string]string)) != 1 {
		t.Fatalf("fs_list: %v %v", r, err)
	}
	if _, err := run(t, s, "fs_write", device.Args{"path": `c:\data\readme.txt`, "text": "x"}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("write outside allow-list: want denied, got %v", err)
	}
	if _, err := run(t, s, "key", device.Args{"code": "5"}); !nokia.IsUnsupported(err) {
		t.Fatalf("key: want unsupported, got %v", err)
	}
	if _, err := run(t, s, "fs_delete", device.Args{"path": `c:\data\phedbg\big.txt`}); err != nil {
		t.Fatal(err)
	}
	r, err = run(t, s, "screenshot", device.Args{})
	if err != nil || r.(map[string]any)["size"] != len(ag.Screen) {
		t.Fatalf("screenshot: %v %v", r, err)
	}
}

func TestBadToken(t *testing.T) {
	s := session(t, "wrong", mock.NewNokiaAgent("4711").Opener())
	if _, err := run(t, s, "info", device.Args{}); err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("want auth error, got %v", err)
	}
}

// TestTCPListen: the phone-side agent connects IN (the PyS60 shape) over a real socket.
func TestTCPListen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	ag := mock.NewNokiaAgent("t")
	go func() {
		for i := 0; i < 50; i++ {
			c, err := net.Dial("tcp", addr)
			if err != nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			fmt.Fprintln(c, ag.Hello())
			sc := bufio.NewScanner(c)
			for sc.Scan() {
				for _, l := range ag.Handle(sc.Text()) {
					fmt.Fprintln(c, l)
				}
			}
			return
		}
	}()
	s := session(t, "t", &transport.TCPOpener{Listen: addr})
	if _, err := run(t, s, "ps", device.Args{}); err != nil {
		t.Fatal(err)
	}
}

// TestKeepAliveReconnect: on an idle TCP link the session pings the agent; when the agent's side drops the connection the
// session reconnects by itself and keeps pinging.
func TestKeepAliveReconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var pings, accepts atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := accepts.Add(1)
			fmt.Fprintln(c, "@0 hello proto=1 agent=mock ver=0")
			go func() {
				sc := bufio.NewScanner(c)
				for sc.Scan() {
					f := strings.Fields(sc.Text())
					if len(f) >= 2 && f[1] == "ping" {
						fmt.Fprintf(c, "@%s ok pong=1\n", f[0])
						if pings.Add(1) == 3 && n == 1 {
							c.Close() // the agent's Wi-Fi link dies
							return
						}
					}
				}
			}()
		}
	}()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, _ := nokia.New(map[string]any{"token": "t", "transport": "tcp", "dial": ln.Addr().String()})
	d.KeepAliveEvery = 200 * time.Millisecond
	m := core.NewManager(st)
	s := core.NewSession("n", d, d.Opener(), st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done; st.Close() }()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !(accepts.Load() >= 2 && pings.Load() >= 6) {
		time.Sleep(100 * time.Millisecond)
	}
	if accepts.Load() < 2 || pings.Load() < 6 {
		t.Fatalf("keep-alive/reconnect: accepts=%d pings=%d", accepts.Load(), pings.Load())
	}
}
