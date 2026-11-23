// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gpd

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

// fakeRig plays the kernel's side of the debug link over loopback UDP: broadcasts (here: unicasts) PHL1 log packets,
// answers PHC1 commands to the sender, streams a screenshot.
type fakeRig struct {
	t      *testing.T
	conn   *net.UDPConn
	lab    *net.UDPAddr // where the lab listens
	mu     sync.Mutex
	boot   uint32
	seq    uint32
	mute   bool // do not answer commands
	cmds   chan []byte
	w, h   int
	drop   map[int]bool // screenshot packet indexes to drop, first attempt only
	shots  int
	dumpTx string // log text sent after a D command
}

func newFakeRig(t *testing.T) *fakeRig {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRig{t: t, conn: c, cmds: make(chan []byte, 256), boot: 0xA1, seq: 1, w: 40, h: 20}
	t.Cleanup(func() { c.Close() })
	go r.serve()
	return r
}

func (r *fakeRig) port() int { return r.conn.LocalAddr().(*net.UDPAddr).Port }

func (r *fakeRig) logf(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sendLogLocked(s, r.seq)
	r.seq++
}

func (r *fakeRig) sendLogLocked(s string, seq uint32) {
	b := []byte("PHL1")
	b = binary.LittleEndian.AppendUint32(b, r.boot)
	b = binary.LittleEndian.AppendUint32(b, seq)
	r.conn.WriteToUDP(append(b, s...), r.lab)
}

// skip makes the next log packet carry a sequence n ahead (n packets lost).
func (r *fakeRig) skip(n uint32) { r.mu.Lock(); r.seq += n; r.mu.Unlock() }

// reset starts a new boot id (a new kernel boot).
func (r *fakeRig) reset(boot uint32) { r.mu.Lock(); r.boot, r.seq = boot, 1; r.mu.Unlock() }

func (r *fakeRig) serve() {
	buf := make([]byte, 2048)
	for {
		n, from, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		d := append([]byte(nil), buf[:n]...)
		if n < 13 || string(d[:4]) != cmdMagic {
			continue
		}
		select {
		case r.cmds <- d:
		default:
		}
		r.mu.Lock()
		mute := r.mute
		r.mu.Unlock()
		if mute {
			continue
		}
		id := binary.LittleEndian.Uint32(d[8:])
		rep := func(data []byte) {
			b := []byte(replyMagic)
			b = binary.LittleEndian.AppendUint32(b, id)
			b = append(b, d[12])
			b = binary.LittleEndian.AppendUint32(b, 0)
			r.conn.WriteToUDP(append(b, data...), from)
		}
		switch d[12] {
		case 'P':
			p := binary.LittleEndian.AppendUint32(nil, r.boot)
			rep(append(p, 0x00, 0xe0, 0x4c, 0x68, 0x00, 0x01))
		case 'S':
			r.shots++
			w, h := r.w, r.h
			rep(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, uint16(w)), uint16(h)))
			r.stream(from, id, w, h, r.shots == 1)
		case 'D':
			rep(nil)
			if r.dumpTx != "" {
				r.logf(r.dumpTx)
			}
		default:
			rep(nil)
		}
	}
}

func (r *fakeRig) stream(to *net.UDPAddr, id uint32, w, h int, dropFirst bool) {
	total, per, idx := w*h, 363, 0
	for first := 0; first < total; first += per {
		n := min(per, total-first)
		if !(dropFirst && r.drop[idx]) {
			b := []byte(screenMagic)
			b = binary.LittleEndian.AppendUint32(b, id)
			b = binary.LittleEndian.AppendUint16(b, uint16(w))
			b = binary.LittleEndian.AppendUint16(b, uint16(h))
			b = binary.LittleEndian.AppendUint32(b, uint32(first))
			b = binary.LittleEndian.AppendUint16(b, uint16(n))
			for i := 0; i < n; i++ {
				b = append(b, 0x30, 0x20, 0x10, 0) // B G R X: R=0x10 G=0x20 B=0x30
			}
			r.conn.WriteToUDP(b, to)
		}
		idx++
	}
	end := []byte(screenMagic)
	end = binary.LittleEndian.AppendUint32(end, id)
	end = append(end, make([]byte, 4+4+2)...)
	r.conn.WriteToUDP(end, to)
}

// next returns the next command the rig received (op byte and args), failing after 3 s.
func (r *fakeRig) next() (byte, []byte) {
	r.t.Helper()
	select {
	case d := <-r.cmds:
		return d[12], d[13:]
	case <-time.After(3 * time.Second):
		r.t.Fatal("the rig got no command")
		return 0, nil
	}
}

func (r *fakeRig) drain() {
	for {
		select {
		case <-r.cmds:
		default:
			return
		}
	}
}

type env struct {
	rig  *fakeRig
	drv  *Driver
	s    *core.Session
	st   *store.Store
	slot string
}

func newEnv(t *testing.T, opts map[string]any) *env {
	t.Helper()
	rig := newFakeRig(t)
	slot := t.TempDir()
	o := map[string]any{"listen_log": "127.0.0.1:0", "cmd_port": int64(rig.port()), "to": "127.0.0.1",
		"link_timeout_s": int64(2), "ping_s": int64(1), "wd_min_s": int64(1), "wd_max_s": int64(100), "slot_dir": slot}
	for k, v := range opts {
		o[k] = v
	}
	drv, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	drv.op.mu.Lock()
	err = drv.op.bind()
	drv.op.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	rig.lab = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: drv.op.LocalAddr().(*net.UDPAddr).Port}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := core.NewManager(st)
	s := core.NewSession("gpd-win2", drv, drv.Opener(), st, m.Bus)
	m.Add(s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; drv.op.Close(); st.Close() })
	return &env{rig: rig, drv: drv, s: s, st: st, slot: slot}
}

func (e *env) up(t *testing.T) {
	t.Helper()
	e.rig.logf("[boot] hello from the rig\n")
	c, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := e.s.WaitConnected(c); err != nil {
		t.Fatal("link never came up:", err)
	}
	e.wait(t, "hello from the rig")
}

func (e *env) wait(t *testing.T, pat string) {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if _, err := e.s.Exec(c, "test", "wait_for", device.Args{"pattern": pat, "timeout_s": 5, "since_line": 0}); err != nil {
		t.Fatalf("wait_for %q: %v", pat, err)
	}
}

func (e *env) exec(t *testing.T, op string, a device.Args) any {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := e.s.Exec(c, "test", op, a)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return res
}

func (e *env) events(kind string) []store.Event {
	ev, _ := e.st.Events(store.EventQuery{Device: "gpd-win2", Kinds: []string{kind}, Limit: 100})
	return ev
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if f() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out waiting for", what)
}

func TestLogIngestBootsAndLoss(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	if h := e.s.Health(); h.State != "connected" || !strings.Contains(h.Port, "127.0.0.1") || h.Boot == 0 {
		t.Fatalf("health %+v", h)
	}
	// a log line split over two packets joins into one line
	e.rig.logf("split ")
	e.rig.logf("line\n")
	e.wait(t, "split line")
	// 3 packets lost
	e.rig.skip(3)
	e.rig.logf("after the gap\n")
	e.wait(t, "after the gap")
	waitFor(t, "link.loss", func() bool { return len(e.events("link.loss")) == 1 })
	if d := string(e.events("link.loss")[0].Data); !strings.Contains(d, `"count":3`) {
		t.Fatalf("loss event %s", d)
	}
	bs, _ := e.st.Boots("gpd-win2", 0, 10)
	if len(bs) != 1 {
		t.Fatalf("boots %+v", bs)
	}
	// the stored lines are the same ones log_lines reads
	waitFor(t, "stored lines", func() bool {
		ls, _ := e.st.Lines(store.LineQuery{Device: "gpd-win2", Limit: 50})
		return len(ls) > 0
	})
}

func TestWatchdogResetIsABootEnd(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	time.Sleep(1200 * time.Millisecond) // silence >= wd_min_s, no reboot command sent
	e.rig.reset(0xB2)
	e.rig.logf("second boot\n")
	e.wait(t, "second boot")
	waitFor(t, "two boots", func() bool { bs, _ := e.st.Boots("gpd-win2", 0, 10); return len(bs) == 2 })
	waitFor(t, "fault.watchdog", func() bool { return len(e.events("fault")) == 1 })
	if d := string(e.events("fault")[0].Data); !strings.Contains(d, `"watchdog"`) {
		t.Fatalf("fault %s", d)
	}
	bs, _ := e.st.Boots("gpd-win2", 0, 10) // newest first
	waitFor(t, "old boot outcome", func() bool {
		bs, _ = e.st.Boots("gpd-win2", 0, 10)
		return len(bs) == 2 && bs[1].Outcome == "fault"
	})
	if bs[0].Outcome == "fault" {
		t.Fatalf("the new boot is not the faulty one: %+v", bs)
	}
}

func TestCommandedRebootIsNotAFault(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	e.rig.drain()
	e.exec(t, "reboot", nil)
	for {
		if op, _ := e.rig.next(); op == 'R' {
			break
		}
	}
	time.Sleep(1200 * time.Millisecond)
	e.rig.reset(0xC3)
	e.rig.logf("after reboot\n")
	e.wait(t, "after reboot")
	waitFor(t, "two boots", func() bool { bs, _ := e.st.Boots("gpd-win2", 0, 10); return len(bs) == 2 })
	waitFor(t, "reset.commanded", func() bool { return len(e.events("reset.commanded")) == 1 })
	if n := len(e.events("fault")); n != 0 {
		t.Fatalf("%d fault events after a commanded reboot", n)
	}
}

func TestInputOps(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	e.rig.drain()
	e.exec(t, "key", device.Args{"key": "f6", "hold_ms": 10}) // the PC board's F6 = OK
	op, args := e.rig.next()
	if op != 'K' || !bytes.Equal(args, []byte{0xa7, 0, 1}) {
		t.Fatalf("key down: %c % x", op, args)
	}
	if op, args = e.rig.next(); op != 'K' || !bytes.Equal(args, []byte{0xa7, 0, 0}) {
		t.Fatalf("key up: %c % x", op, args)
	}
	e.exec(t, "key", device.Args{"key": "menu", "action": "down"})
	if _, args = e.rig.next(); !bytes.Equal(args, []byte{0xb4, 0, 1}) {
		t.Fatalf("menu down % x", args)
	}
	e.rig.drain()
	e.exec(t, "touch", device.Args{"x": 300, "y": 20, "hold_ms": 10})
	op, args = e.rig.next()
	if op != 'T' || !bytes.Equal(args, []byte{0x2c, 1, 20, 0, 1}) {
		t.Fatalf("touch down: %c % x", op, args)
	}
	if _, args = e.rig.next(); !bytes.Equal(args, []byte{0x2c, 1, 20, 0, 2}) {
		t.Fatalf("touch up % x", args)
	}
	e.rig.drain()
	e.exec(t, "swipe", device.Args{"x1": 10, "y1": 10, "x2": 10, "y2": 110, "ms": 40, "steps": 4})
	var acts []byte
	for i := 0; i < 6; i++ {
		_, a := e.rig.next()
		acts = append(acts, a[4])
	}
	if !bytes.Equal(acts, []byte{1, 0, 0, 0, 0, 2}) {
		t.Fatalf("swipe actions %v", acts)
	}
	e.rig.drain()
	e.exec(t, "text", device.Args{"text": "Hi 2"})
	time.Sleep(200 * time.Millisecond)
	n := len(e.rig.cmds)
	if n < 8 {
		t.Fatalf("text sent %d key events", n)
	}
	if _, err := e.s.Exec(context.Background(), "test", "key", device.Args{"key": "nope"}); err == nil {
		t.Fatal("an unknown key must fail")
	}
}

func TestThreadDumpAndHang(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	e.rig.dumpTx = "Thread dump:\nT1 ready\nT2 waitsem\n"
	e.rig.drain()
	res := e.exec(t, "threads", device.Args{"wait_s": 8}).(map[string]any)
	if op, _ := e.rig.next(); op != 'D' {
		t.Fatalf("op %c", op)
	}
	if ls, _ := res["lines"].([]string); len(ls) < 3 || !strings.Contains(strings.Join(ls, "|"), "T2 waitsem") {
		t.Fatalf("dump lines %v", res["lines"])
	}
	waitFor(t, "threads.dump event", func() bool { return len(e.events("threads.dump")) > 0 })
	e.rig.drain()
	e.exec(t, "hang", nil)
	if op, _ := e.rig.next(); op != 'H' {
		t.Fatalf("op %c", op)
	}
}

func TestScreenshotReassemblesAndReasks(t *testing.T) {
	e := newEnv(t, nil)
	e.rig.drop = map[int]bool{1: true} // the first attempt loses its middle packet
	e.up(t)
	res := e.exec(t, "screenshot", nil).(map[string]any)
	if res["attempts"] != 2 || res["width"] != 40 || res["height"] != 20 {
		t.Fatalf("result %+v", res)
	}
	rc, _, err := e.st.OpenBlob(res["blob"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	img, err := png.Decode(rc)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(39, 19).RGBA()
	if r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
		t.Fatalf("pixel %x %x %x", r>>8, g>>8, b>>8)
	}
}

func TestPushSlotServedAndReboot(t *testing.T) {
	e := newEnv(t, map[string]any{"lab": "192.168.2.50:8737"})
	e.up(t)
	srv := httptest.NewServer(e.drv.SlotHandler())
	defer srv.Close()
	// an empty slot is a 404 on purpose
	r0, _ := http.Get(srv.URL + "/gpd/phorepoc.elf")
	if r0 == nil || r0.StatusCode != 404 {
		t.Fatalf("empty slot: %v", r0)
	}
	elf := append([]byte("\x7fELF"), make([]byte, 1000)...)
	img := filepath.Join(t.TempDir(), "phorepoc.elf")
	os.WriteFile(img, elf, 0o644)
	e.rig.drain()
	res := e.exec(t, "push", device.Args{"file": img}).(map[string]any)
	if res["rebooted"] != true || res["sha256"] == "" {
		t.Fatalf("push result %+v", res)
	}
	for {
		if op, _ := e.rig.next(); op == 'R' {
			break
		}
	}
	got, err := http.Get(srv.URL + "/gpd/phorepoc.elf")
	if err != nil || got.StatusCode != 200 {
		t.Fatalf("get: %v %v", got, err)
	}
	var body bytes.Buffer
	body.ReadFrom(got.Body)
	got.Body.Close()
	if !bytes.Equal(body.Bytes(), elf) {
		t.Fatal("slot content differs")
	}
	r1, _ := http.Get(srv.URL + "/gpd/..%2fsecret")
	r2, _ := http.Get(srv.URL + "/gpd/grubx64.efi")
	if r1 == nil || r1.StatusCode != 404 || r2 == nil || r2.StatusCode != 404 {
		t.Fatal("traversal and an unpushed file must 404")
	}
	// GRUB without the lab address is flagged; a push of a non-ELF image or an ISO is refused
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bad := filepath.Join(t.TempDir(), "grub")
	os.WriteFile(bad, []byte("grub without the address"), 0o644)
	r3, err := e.s.Exec(c, "test", "push", device.Args{"file": bad, "name": "grubx64.efi", "reboot": false})
	if err != nil || r3.(map[string]any)["warning"] == nil {
		t.Fatalf("grub push: %v %v", r3, err)
	}
	if _, err := e.s.Exec(c, "test", "push", device.Args{"file": bad}); err == nil {
		t.Fatal("a non-ELF phorepoc.elf must be refused")
	}
	iso := make([]byte, 0x9000)
	copy(iso[0x8001:], "CD001")
	os.WriteFile(bad, iso, 0o644)
	if _, err := e.s.Exec(c, "test", "push", device.Args{"file": bad}); err == nil || !strings.Contains(err.Error(), "ISO") {
		t.Fatalf("ISO: %v", err)
	}
	// the next boot is recorded with the pushed image as its slot
	e.rig.reset(0xD4)
	e.rig.logf("new image running\n")
	e.wait(t, "new image running")
	waitFor(t, "boot slot", func() bool {
		bs, _ := e.st.Boots("gpd-win2", 0, 10)
		return len(bs) == 2 && len(bs[0].Slot) == 12 && strings.HasPrefix(res["sha256"].(string), bs[0].Slot)
	})
	// info lists the slot and answers the ping
	info := e.exec(t, "info", nil).(map[string]any)
	ping, _ := info["ping"].(map[string]any)
	if ping["mac"] != "00:e0:4c:68:00:01" || ping["boot"] != "000000d4" {
		t.Fatalf("info %+v", info)
	}
}

func TestPushWhileLinkDown(t *testing.T) {
	e := newEnv(t, nil) // the rig never speaks: the link stays down
	elf := append([]byte("\x7fELF"), 1, 2, 3)
	img := filepath.Join(t.TempDir(), "x.elf")
	os.WriteFile(img, elf, 0o644)
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := e.s.Exec(c, "test", "push", device.Args{"file": img})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["rebooted"] != false || m["reboot_note"] == nil {
		t.Fatalf("%+v", m)
	}
	if b, err := os.ReadFile(filepath.Join(e.slot, "phorepoc.elf")); err != nil || !bytes.Equal(b, elf) {
		t.Fatalf("slot file: %v", err)
	}
	if h := e.s.Health(); h.State != "disconnected" {
		t.Fatalf("health %+v", h)
	}
	c2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if _, err := e.s.Exec(c2, "test", "key", device.Args{"key": "f6"}); err == nil {
		t.Fatal("an input op needs the link")
	}
}

func TestLinkDownAndUpFromHeartbeat(t *testing.T) {
	e := newEnv(t, nil)
	e.up(t)
	// the pings keep a silent kernel's link up
	time.Sleep(2500 * time.Millisecond)
	if h := e.s.Health(); h.State != "connected" {
		t.Fatalf("a silent but answering rig went down: %+v", h)
	}
	e.rig.mu.Lock()
	e.rig.mute = true
	e.rig.mu.Unlock()
	waitFor(t, "link down", func() bool { return e.s.Health().State == "disconnected" })
	if len(e.events("link.disconnected")) == 0 {
		t.Fatal("no link.disconnected event")
	}
	if h := e.s.Health(); h.LastByteAge < 1 {
		t.Fatalf("the age of the last log must show: %+v", h)
	}
	e.rig.mu.Lock()
	e.rig.mute = false
	e.rig.mu.Unlock()
	e.rig.logf("back\n")
	waitFor(t, "link up", func() bool { return e.s.Health().State == "connected" })
	e.wait(t, "back")
}

func TestWireFormat(t *testing.T) {
	p := cmdPacket(7, 0x01020304, 'K', []byte{1, 2})
	want := fmt.Sprintf("%x", []byte{'P', 'H', 'C', '1', 7, 0, 0, 0, 4, 3, 2, 1, 'K', 1, 2})
	if fmt.Sprintf("%x", p) != want {
		t.Fatalf("% x", p)
	}
	fr := wrapFrame(2, 9, []byte{1, 2, 3})
	if len(fr) != 19 || fr[4] != 2 || binary.LittleEndian.Uint32(fr[12:]) != 6 {
		t.Fatalf("% x", fr)
	}
}

func TestRigReport(t *testing.T) {
	d, err := New(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ev := d.Classify("[rig] report kind=fault boot=614d069d bytes=4382 log=/var/log/phorepoc/boot-3.log")
	if len(ev) != 1 || ev[0].Kind != "rig.report" || ev[0].Data["kind"] != "fault" || ev[0].Data["boot_id"] != "614d069d" ||
		ev[0].Data["bytes"] != 4382 {
		t.Fatalf("report: %+v", ev)
	}
	ev = d.Classify("[rig] report kind=no-log boot=- bytes=0 log=/var/log/phorepoc/boot-4.log stage=disp-mapped(0x20),crash")
	if len(ev) != 1 || ev[0].Data["kind"] != "no-log" || ev[0].Data["stage"] != "disp-mapped(0x20),crash" {
		t.Fatalf("no-log: %+v", ev)
	}
	if ev = d.Classify("[rig] fault 0:FAULT: RIGTEST 0x00000001 (1)"); len(ev) != 1 || ev[0].Kind != "rig.fault" {
		t.Fatalf("fault: %+v", ev)
	}
	if ev = d.Classify("[rig] | 0:FATAL: something"); len(ev) != 0 {
		t.Fatalf("replayed line made events: %+v", ev)
	}
	if ev = d.Classify("[disklog] phorepoc-log at LBA 1"); len(ev) != 1 || ev[0].Kind != "disklog" {
		t.Fatalf("disklog tag: %+v", ev)
	}
}
