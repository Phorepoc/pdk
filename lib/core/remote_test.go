// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/device/phlink"
)

func TestScreenshotRoundTrip(t *testing.T) {
	r := newRig(t, 20)
	res, err := r.s.Exec(ctx(t, 20*time.Second), "test", "screenshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	sh := res.(phlink.Shot)
	if sh.Width != 360 || sh.Height != 640 || len(sh.Blob) != 64 {
		t.Fatalf("shot %+v", sh)
	}
	if sh.Wire >= 360*640*3 {
		t.Errorf("RLE did not compress the flat bands: %d wire bytes", sh.Wire)
	}
	rc, _, err := r.st.OpenBlob(sh.Blob)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	img, err := png.Decode(rc)
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range [][2]int{{0, 0}, {359, 0}, {200, 300}, {5, 600}, {6, 600}, {359, 639}} {
		want := r.ph.Screen[pt[1]*360+pt[0]] & 0xffffff
		c, g, b, _ := img.At(pt[0], pt[1]).RGBA()
		got := (c>>8)<<16 | (g>>8)<<8 | b>>8
		if got != want {
			t.Errorf("pixel %v = %06x, want %06x", pt, got, want)
		}
	}
	// the log interleaved with the frames still arrives as lines
	waitFor(t, r.s, `free RAM KB=0x000cd1b8`, 0)
	blobs, _ := r.st.Blobs("screenshot", 5)
	if len(blobs) != 1 {
		t.Fatalf("blobs %+v", blobs)
	}
}

func TestInputOps(t *testing.T) {
	r := newRig(t, 10)
	c := ctx(t, 10*time.Second)
	if _, err := r.s.Exec(c, "test", "key", device.Args{"key": "lsk", "hold_ms": float64(10)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Exec(c, "test", "touch", device.Args{"x": float64(100), "y": float64(200), "hold_ms": float64(10)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Exec(c, "test", "swipe", device.Args{"x1": float64(10), "y1": float64(600), "x2": float64(10), "y2": float64(100), "ms": float64(24)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Exec(c, "test", "touch", device.Args{"x": float64(400), "y": float64(10)}); err == nil {
		t.Fatal("off-screen touch accepted")
	}
	if _, err := r.s.Exec(c, "test", "text", device.Args{"text": "a1"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	got := strings.Join(r.ph.Input, "|")
	for _, want := range []string{"K a4 0|K a4 1", "T c80064 0|T c80064 2", "T 258000a 0", "T 64000a 2", "K 41 0|K 41 1|K 31 0"} {
		if !strings.Contains(got, want) {
			t.Errorf("input %q missing %q", got, want)
		}
	}
	v, err := r.s.Exec(c, "test", "version", nil)
	if err != nil || v.(phlink.Version).Proto != 3 {
		t.Fatalf("version %+v %v", v, err)
	}
}

func TestOldImageSaysSo(t *testing.T) {
	r := newRig(t, 10)
	r.ph.Proto1 = true
	c := ctx(t, 10*time.Second)
	v, err := r.s.Exec(c, "test", "version", nil)
	if err != nil || v.(phlink.Version).Proto != 1 {
		t.Fatalf("version %+v %v", v, err)
	}
	_, err = r.s.Exec(c, "test", "screenshot", nil)
	if err == nil || !strings.Contains(err.Error(), "protocol 1") {
		t.Fatalf("want a protocol 1 error, got %v", err)
	}
}

func TestHotspots(t *testing.T) {
	r := newRig(t, 10)
	c := ctx(t, 15*time.Second)
	syms := filepath.Join(t.TempDir(), "x.syms")
	os.WriteFile(syms, []byte("I 80001000 2000 ewsrv.exe\nF 80001000 CWsWindow::Redraw()\nF 80002000 CFoo::Bar()\n"), 0o644)
	res, err := r.s.Exec(c, "test", "hotspots", device.Args{"seconds": float64(1), "symbols": syms})
	if err != nil {
		t.Fatal(err)
	}
	h := res.(phlink.Hotspots)
	if h.Samples != 100 || len(h.Functions) != 3 {
		t.Fatalf("hotspots %+v", h)
	}
	f := h.Functions[0]
	if f.Function != "CWsWindow::Redraw()" || f.Pct != 60 || f.Thread != "EWSRV.EXE / Wserv" || f.Image != "ewsrv.exe" {
		t.Fatalf("top %+v", f)
	}
	if len(f.Callers) != 1 || f.Callers[0].Thread != "CFoo::Bar()" || f.Callers[0].Pct != 60 {
		t.Fatalf("callers %+v", f.Callers)
	}
	if h.Functions[2].Function != "(kernel)" || h.Threads[0].Thread != "EWSRV.EXE / Wserv" {
		t.Fatalf("functions %+v threads %+v", h.Functions, h.Threads)
	}
}

func TestProfile(t *testing.T) {
	r := newRig(t, 10)
	c := ctx(t, 15*time.Second)
	res, err := r.s.Exec(c, "test", "profile", device.Args{"window_ms": float64(10)})
	if err != nil {
		t.Fatal(err)
	}
	p := res.(phlink.Profile)
	if p.Cores != 4 || p.CoreBusy[0] != 50 || p.CoreBusy[1] != 0 || p.WindowS != 1 {
		t.Fatalf("cores %+v", p)
	}
	if len(p.Threads) == 0 || p.Threads[0].Name != "Telephone" || p.Threads[0].CPU != 25 || p.Threads[0].Process != "Telephone" {
		t.Fatalf("threads %+v", p.Threads)
	}
	for _, th := range p.Threads {
		if strings.HasPrefix(th.Name, "Null") {
			t.Fatalf("idle thread listed: %+v", th)
		}
	}
	if p.Processes[0].Name != "Telephone" || p.Processes[0].RAM != 0x400000 || p.GlobalRAM != 0x1000000 || p.FreeRAM != 0xc8000<<10 {
		t.Fatalf("processes %+v global %x", p.Processes, p.GlobalRAM)
	}
	// the second call diffs against the first: one snapshot per call
	if _, err := r.s.Exec(c, "test", "profile", nil); err != nil {
		t.Fatal(err)
	}
}

// A default tap must keep the finger down long enough for a game that polls the pointer once per frame:
// Down, then Up no sooner than 150 ms later.
func TestDefaultTapHoldsTheFinger(t *testing.T) {
	r := newRig(t, 10)
	c := ctx(t, 10*time.Second)
	start := time.Now()
	if _, err := r.s.Exec(c, "test", "touch", device.Args{"x": float64(100), "y": float64(200)}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("default tap lasted %v, want >= 150ms", d)
	}
	time.Sleep(50 * time.Millisecond)
	if got := strings.Join(r.ph.Input, "|"); !strings.Contains(got, "T c80064 0|T c80064 2") {
		t.Errorf("tap events %q: want Down then Up", got)
	}
}
