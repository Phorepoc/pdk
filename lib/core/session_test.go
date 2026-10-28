// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/device/phlink"
	"github.com/Phorepoc/pdk/lib/mock"
	"github.com/Phorepoc/pdk/lib/store"
)

const fixture = "../../testdata/boot-slotb-rearm.log"

type rig struct {
	ph *mock.Phone
	s  *core.Session
	st *store.Store
	m  *core.Manager
}

func newRig(t *testing.T, maxLines int) *rig {
	t.Helper()
	lines, err := mock.LoadTranscript(fixture, maxLines)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ph := mock.New(lines)
	m := core.NewManager(st)
	s := core.NewSession("pp", &phlink.Driver{}, ph.Opener(), st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; st.Close() })
	c, cc := context.WithTimeout(ctx, 5*time.Second)
	defer cc()
	if err := s.WaitConnected(c); err != nil {
		t.Fatal("never connected:", err)
	}
	return &rig{ph: ph, s: s, st: st, m: m}
}

func ctx(t *testing.T, d time.Duration) context.Context {
	c, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return c
}

func waitFor(t *testing.T, s *core.Session, pat string, since uint64) core.WaitResult {
	t.Helper()
	r, err := s.Exec(ctx(t, 10*time.Second), "test", "wait_for", device.Args{"pattern": pat, "timeout_s": 8, "since_line": since})
	if err != nil {
		t.Fatalf("wait_for %q: %v", pat, err)
	}
	return r.(core.WaitResult)
}

func TestBootIsRecorded(t *testing.T) {
	r := newRig(t, 1200)
	waitFor(t, r.s, `running slot A`, 0)
	h := r.s.Health()
	if h.State != "connected" || h.Slot != "A" || h.Boot == 0 {
		t.Fatalf("health %+v", h)
	}
	time.Sleep(400 * time.Millisecond) // line writer batch
	boots, _ := r.st.Boots("pp", 0, 10)
	if len(boots) != 1 || boots[0].Slot != "A" || boots[0].Lines < 1000 {
		t.Fatalf("boots %+v", boots)
	}
	ev, _ := r.st.Events(store.EventQuery{Device: "pp", Kinds: []string{"boot."}})
	if len(ev) < 2 {
		t.Fatalf("want boot.start + boot.slot events, got %+v", ev)
	}
}

func TestPeekPokeDump(t *testing.T) {
	r := newRig(t, 50)
	r.ph.Mem[0x01ca0004] = 0xdeadbeef
	res, err := r.s.Exec(ctx(t, 5*time.Second), "test", "peek", device.Args{"addr": "0x01ca0000", "count": float64(4)})
	if err != nil {
		t.Fatal(err)
	}
	w := res.([]phlink.Word)
	if len(w) != 4 || w[1].Value != "0xdeadbeef" || w[3].Addr != "0x01ca000c" {
		t.Fatalf("peek %+v", w)
	}
	res, err = r.s.Exec(ctx(t, 5*time.Second), "test", "poke", device.Args{"addr": "0x01f00010", "value": "0x12345678"})
	if err != nil || res.(phlink.Word).Value != "0x12345678" || r.ph.Mem[0x01f00010] != 0x12345678 {
		t.Fatalf("poke %v %v", res, err)
	}
	res, err = r.s.Exec(ctx(t, 5*time.Second), "test", "dump", device.Args{"ranges": "0x1000:70, 0x2000"})
	if err != nil || len(res.([]phlink.Word)) != 71 {
		t.Fatalf("dump %d %v", len(res.([]phlink.Word)), err)
	}
	ops, _ := r.st.Ops("pp", 0, 10)
	if len(ops) < 3 || ops[0].Op != "dump" || ops[0].Caller != "test" {
		t.Fatalf("ops not recorded: %+v", ops)
	}
}

func TestPushRebootsIntoSlotB(t *testing.T) {
	r := newRig(t, 30)
	img := make([]byte, 300000+3) // odd size: exercises the zero padding
	rand.Read(img)
	p := filepath.Join(t.TempDir(), "symbian.img")
	if err := writeFile(p, img); err != nil {
		t.Fatal(err)
	}
	sub := r.m.Bus.Subscribe(100000)
	defer sub.Close()
	mark := r.s.Health().Lines
	res, err := r.s.Exec(ctx(t, 20*time.Second), "test", "push", device.Args{"file": p})
	if err != nil {
		t.Fatal(err)
	}
	pr := res.(phlink.PushResult)
	if !pr.Rebooted || pr.Sum != pr.PhoneSum {
		t.Fatalf("push %+v", pr)
	}
	waitFor(t, r.s, `running slot B`, mark)
	if !bytes.Equal(r.ph.SlotB, img) {
		t.Fatal("slot B content differs")
	}
	var sawProgress, sawDisc bool
	for len(sub.C) > 0 {
		m := <-sub.C
		if m.Type == "progress" {
			sawProgress = true
		}
		if m.Type == "event" && m.Data.(store.Event).Kind == "link.disconnected" {
			sawDisc = true
		}
	}
	if !sawProgress || !sawDisc {
		t.Fatalf("progress=%v disconnect=%v", sawProgress, sawDisc)
	}
	if r.s.Health().Reconnects < 1 {
		t.Fatal("no reconnect counted")
	}
}

func TestPushFailedVerdict(t *testing.T) {
	r := newRig(t, 10)
	r.ph.FailSum = true
	_, err := r.s.Exec(ctx(t, 10*time.Second), "test", "push", device.Args{"file": writeTemp(t, []byte("hello world!"))})
	if err == nil || !strings.Contains(err.Error(), "UPLOAD FAILED") {
		t.Fatalf("want UPLOAD FAILED, got %v", err)
	}
}

func TestRearmWithoutImageRefused(t *testing.T) {
	r := newRig(t, 10)
	_, err := r.s.Exec(ctx(t, 10*time.Second), "test", "rearm", nil)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("got %v", err)
	}
}

func TestSelfHealsAfterUnplug(t *testing.T) {
	r := newRig(t, 10)
	r.ph.Unplug()
	deadline := time.Now().Add(3 * time.Second)
	for r.s.Health().State == "connected" {
		if time.Now().After(deadline) {
			t.Fatal("unplug not noticed")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// an op issued while unplugged waits for the phone rather than failing at once
	done := make(chan error, 1)
	go func() {
		_, err := r.s.Exec(ctx(t, 10*time.Second), "test", "poke", device.Args{"addr": 16, "value": 1})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	r.ph.Plug()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if h := r.s.Health(); h.State != "connected" || h.Reconnects != 1 {
		t.Fatalf("health %+v", h)
	}
}

func TestOpsAreSerialized(t *testing.T) {
	r := newRig(t, 10)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			_, err := r.s.Exec(ctx(t, 10*time.Second), "test", "peek", device.Args{"addr": float64(0x1000 * i), "count": float64(64)})
			errs <- err
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
