// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

import (
	"testing"
)

func TestWordSum(t *testing.T) {
	// 01 00 00 00 | 02 00 00 00 | ff (padded: ff 00 00 00)
	if s := WordSum([]byte{1, 0, 0, 0, 2, 0, 0, 0, 0xff}); s != 0x102 {
		t.Fatalf("got %x", s)
	}
	if s := WordSum([]byte{0xff, 0xff, 0xff, 0xff, 2, 0, 0, 0}); s != 1 { // wraps at 32 bits
		t.Fatalf("got %x", s)
	}
}

func TestClassify(t *testing.T) {
	d := &Driver{}
	cases := []struct {
		line string
		kind string
	}{
		{"[EKERN] _E32Startup reached HwInit0 -- from-source kernel is running!", "boot.start"},
		{"[EKERN][slot] running slot B (one-shot boot), slot B len=0x035dc000", "boot.slot"},
		{"phlink: UPLOAD OK len=035dc000 sum=931b25fc", "upload.verdict"},
		{"[EKERN] *** ExcFault: exception dispatched ***", "fault"},
		{"[EKERN]     category='KERN-EXEC'", "panic"},
		{"[EKERN][mon] free RAM KB=0x000cd1b8", "mon.ram"},
		{"[EKERN][mon] sd io: calls=0x00000109 sectors=0x00000200 ms=0x00000010", "mon.sdio"},
		{"[EKERN][key] power down", "key.power"},
	}
	for _, c := range cases {
		ev := d.Classify(c.line)
		found := false
		for _, e := range ev {
			found = found || e.Kind == c.kind
		}
		if !found {
			t.Errorf("%q: want %s, got %+v", c.line, c.kind, ev)
		}
	}
	// glued monitor lines yield both events
	ev := d.Classify("[EKERN][mon] busy% cpu0..3 = 00 12 00 99[EKERN][mon] free RAM KB=0x00000010")
	if len(ev) != 2 || ev[0].Data["busy"].([]int)[3] != 99 {
		t.Fatalf("glued: %+v", ev)
	}
	if !d.BootStart("[EKERN] _E32Startup reached HwInit0 -- x") || d.BootStart("[EKERN] KernelMain entered") {
		t.Fatal("BootStart")
	}
	if ev := d.Classify("[ctor] TDfc done"); len(ev) != 0 {
		t.Fatalf("noise classified: %+v", ev)
	}
}
