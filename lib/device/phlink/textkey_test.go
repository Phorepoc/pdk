package phlink

import "testing"

func TestTextKeyCoversPrintableASCII(t *testing.T) {
	for r := rune(0x20); r < 0x7f; r++ {
		if _, _, err := textKey(r); err != nil {
			t.Errorf("%q: %v", r, err)
		}
	}
	if c, s, _ := textKey(':'); c != 0x7d || !s {
		t.Errorf("':' = %#x shift=%v", c, s)
	}
	if c, s, _ := textKey('b'); c != 'B' || s {
		t.Errorf("'.' = %#x shift=%v", c, s)
	}
	if _, _, err := textKey('é'); err == nil {
		t.Error("é should fail")
	}
}

func TestToPortraitFollowsTheLayoutLog(t *testing.T) {
	d := &Driver{}
	if x, y := d.toPortrait(180, 320); x != 180 || y != 320 {
		t.Fatalf("portrait mapped to %d,%d", x, y)
	}
	d.Classify("[user] PH: avkon HandleResourceChangeL(layout switch): application rect 640x360, appui 3401888")
	// landscape (320,179) is the raw (180,320) wserv rotates to it: x,y -> y,359-x
	if x, y := d.toPortrait(320, 179); x != 180 || y != 320 {
		t.Fatalf("landscape 320,179 mapped to %d,%d, want 180,320", x, y)
	}
	d.Classify("[EKERN][power] screen OFF done")
	if !d.scr.off.Load() {
		t.Fatal("screen off not noted")
	}
	d.Classify("[EKERN][disp] HAL display state ON")
	if d.scr.off.Load() {
		t.Fatal("screen on not noted")
	}
	d.Classify("application rect 360x640") // not a layout line: no change
	d.Classify("[user] PH: avkon HandleResourceChangeL(layout switch): application rect 360x640, appui 1")
	if d.scr.landscape.Load() {
		t.Fatal("portrait not noted")
	}
}
