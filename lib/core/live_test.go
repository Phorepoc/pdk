package core_test

import (
	"bytes"
	"fmt"
	"image/png"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/device/pinephone"
)

func sh(s pinephone.Shot) string {
	s.Data = nil
	return fmt.Sprintf("%+v", s)
}

func liveArgs(which float64) device.Args {
	return device.Args{"which": which, "store": false, "live": true}
}

// The live view against the mock phone: a keyframe, then diffs that carry only the changed 32x32 tiles.
func TestLiveView(t *testing.T) {
	r := newRig(t, 10)
	c := ctx(t, 20*time.Second)
	shot := func(which float64) pinephone.Shot {
		t.Helper()
		res, err := r.s.Exec(c, "test", "screenshot", liveArgs(which))
		if err != nil {
			t.Fatal(err)
		}
		return res.(pinephone.Shot)
	}
	check := func(s pinephone.Shot, w, h int) {
		t.Helper()
		img, err := png.Decode(bytes.NewReader(s.Data))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < h; y += 7 {
			for x := 0; x < w; x += 5 {
				want := r.ph.Screen[y*w+x] & 0xffffff
				cr, cg, cb, _ := img.At(x, y).RGBA()
				if got := (cr>>8)<<16 | (cg>>8)<<8 | cb>>8; got != want {
					t.Fatalf("pixel %d,%d = %06x, want %06x", x, y, got, want)
				}
			}
		}
	}
	k := shot(0)
	if !k.Key || k.Tiles != 12*20 || k.Width != 360 || k.Height != 640 {
		t.Fatalf("first frame is not a full keyframe: %s", sh(k))
	}
	check(k, 360, 640)
	u := shot(0)
	if u.Key || u.Tiles != 0 || u.Wire > 80 {
		t.Fatalf("unchanged screen: %s", sh(u))
	}
	check(u, 360, 640)
	// repaint a 40x20 patch (touches 2x1 tiles at the 32 px grid -> 2 tiles)
	for y := 100; y < 120; y++ {
		for x := 40; x < 80; x++ {
			r.ph.Screen[y*360+x] = 0xff123456
		}
	}
	d := shot(0)
	if d.Key || d.Tiles != 2 || d.Wire > 2*3072+200 {
		t.Fatalf("diff frame: %s", sh(d))
	}
	check(d, 360, 640)
	t.Logf("key: %d wire bytes (%d tiles); idle: %d; small change: %d (%d tiles)", k.Wire, k.Tiles, u.Wire, d.Wire, d.Tiles)
	// another source: a keyframe again, 360x720
	if c2 := shot(2); !c2.Key || c2.Height != 720 {
		t.Fatalf("composite: %+v", c2)
	}
}

// An image without the live view (protocol 2) still gives a picture: the old whole-screen capture.
func TestLiveViewOldImage(t *testing.T) {
	r := newRig(t, 10)
	r.ph.NoLive = true
	res, err := r.s.Exec(ctx(t, 20*time.Second), "test", "screenshot", liveArgs(0))
	if err != nil {
		t.Fatal(err)
	}
	s := res.(pinephone.Shot)
	if s.Key || s.Tiles != 0 || len(s.Data) == 0 || s.Width != 360 {
		t.Fatalf("fallback: %s", sh(s))
	}
}
