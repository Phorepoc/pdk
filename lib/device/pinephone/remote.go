// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pinephone

// Protocol v2 ops (phlink.cpp "Protocol v2"): version, screenshot (binary frames), key and touch injection.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const (
	frameShotBegin = 1
	frameShotRows  = 2
	frameShotEnd   = 3
)

// OS screen size (the ROM's nHD screen; the DE doubles it onto the 720x1440 panel). Touch coordinates are in it.
const ScreenW, ScreenH = 360, 640

// Keys maps friendly names to TStdScanCode (e32keys.h) as S60 on the PinePhone uses them.
var Keys = map[string]uint32{
	"lsk": 0xa4, "left_softkey": 0xa4, // EStdKeyDevice0
	"rsk": 0xa5, "right_softkey": 0xa5, // EStdKeyDevice1
	"power":  0xa6,             // EStdKeyDevice2 (SysAp: short = power menu, long = off)
	"select": 0xa7, "ok": 0xa7, // EStdKeyDevice3
	"menu": 0xb4, "app": 0xb4, // EStdKeyApplication0 (long = task switcher)
	"green": 0xc4, "yes": 0xc4, "call": 0xc4, // EStdKeyYes
	"red": 0xc5, "no": 0xc5, "end": 0xc5, // EStdKeyNo
	"up": 0x10, "down": 0x11, "left": 0x0e, "right": 0x0f,
	"enter": 0x03, "backspace": 0x01, "clear": 0x01, "escape": 0x04, "space": 0x05,
	"volup": 0xa2, "voldown": 0xa3,
	"pencil": 0x12, "edit": 0x12, // the S60 edit (ABC) key = EStdKeyLeftShift
	"lock":   0xb3, // EStdKeyDeviceF: the keylock switch on S60v5 touch phones
	"camera": 0xab, // EStdKeyDevice7
	// the phone keypad's * and # as S60 matches them (scan codes, not characters): keylock's unlock is lsk then
	// EStdKeyNkpAsterisk (repository 10283322, as S60 phones ship it) -- 0x2a (ASCII '*') never unlocked
	"hash": 0x7f, "#": 0x7f, // EStdKeyHash
	"star": 0x85, "*": 0x85, // EStdKeyNkpAsterisk
}

func remoteOps() []device.OpSpec {
	return []device.OpSpec{
		{Name: "profile", Capability: "profile",
			Description: "CPU and RAM profile: per-core busy %, top threads by CPU (% of one core), every process's CPU and " +
				"RAM (bytes in the chunks it owns), free RAM, kernel heap. Measured since the previous profile call (or over " +
				"window_ms on the first call), so polling it every few seconds gives a live profile. Needs caps 'profile'.",
			Params: []device.Param{
				{Name: "top", Type: "int", Description: "threads to return (default 25, 0 = all)"},
				{Name: "window_ms", Type: "int", Description: "first-call measuring window (default 1000)"},
			}},
		{Name: "hotspots", Capability: "profile",
			Description: "Where the CPU time goes, by function: samples every busy core's user pc every 2 ms (kernel PC " +
				"sampler) and returns the hottest functions, images and threads (% of busy samples). action = run " +
				"(default: start, wait seconds, stop) | start | stop (start, do something on the phone, then stop). " +
				"symbols = a file from the ROM map's symbol export (else raw pcs). Needs caps 'hotspots'.",
			Params: []device.Param{
				{Name: "action", Type: "string", Description: "run | start | stop"},
				{Name: "seconds", Type: "int", Description: "run: sampling time (default 10, max 300)"},
				{Name: "top", Type: "int", Description: "functions to return (default 40, 0 = all)"},
				{Name: "symbols", Type: "file", Description: "symbol file (e.g. .pinephone-build/symbian.syms)"},
			}},
		{Name: "threads", Capability: "debug",
			Description: "Thread dump: every thread's state (ready, waitsem, blocked...), the object it waits on, and for a " +
				"user thread blocked in an exec call its pc/lr named with the symbol file. For 'why does it hang'. " +
				"Needs caps 'threads'.",
			Params: []device.Param{
				{Name: "symbols", Type: "file", Description: "symbol file (e.g. .pinephone-build/symbian.syms)"},
				{Name: "filter", Type: "string", Description: "only threads whose name contains this (case-insensitive)"},
			}},
		{Name: "version", Capability: "info",
			Description: "Ask the running image which phlink protocol it speaks (1 = no screenshot/input support)."},
		{Name: "screenshot", Capability: "screen",
			Description: "Capture the screen as PNG (stored in history). which: 0 = what the OS draws (360x640), 1 = the panel " +
				"layer (720x1440: top bar + soft key bar + golden overlay), 2 = the whole screen as seen (both, 360x720 = half " +
				"the panel; needs caps 'composite'). Returns the blob sha (GET /api/blobs/{sha}). Live view: GET .../frame.",
			Params: []device.Param{
				{Name: "which", Type: "int", Description: "0 = OS screen (default), 1 = panel layer, 2 = composite"},
				{Name: "store", Type: "bool", Description: "keep it in the history (default true)"},
				{Name: "live", Type: "bool", Description: "live view (with store=false): only changed 32x32 tiles cross the link, LZ4-compressed (protocol 3); an older image falls back to a full capture"},
			}},
		{Name: "key", Capability: "input",
			Description: "Press a key. key = a name (lsk, rsk, select, menu, green, red, power, up/down/left/right, enter, " +
				"backspace, 0-9, a-z...) or a TStdScanCode number. action = press (default; down, hold, up) | down | up.",
			Params: []device.Param{
				{Name: "key", Type: "string", Required: true, Description: "key name or scan code"},
				{Name: "action", Type: "string", Description: "press | down | up"},
				{Name: "hold_ms", Type: "int", Description: "press duration (default 100; >800 = a long press)"},
			}},
		{Name: "text", Capability: "input",
			Description: "Type text as key presses (digits, letters, space, enter via \\n). Uses the scan codes; what appears depends on the focused editor's input mode.",
			Params:      []device.Param{{Name: "text", Type: "string", Required: true, Description: "text to type"}}},
		{Name: "touch", Capability: "input",
			Description: "Touch the screen at x,y in OS screen pixels (360x640). action = tap (default) | down | move | up.",
			Params: []device.Param{
				{Name: "x", Type: "int", Required: true, Description: "0..359"},
				{Name: "y", Type: "int", Required: true, Description: "0..639"},
				{Name: "action", Type: "string", Description: "tap | down | move | up"},
				{Name: "hold_ms", Type: "int", Description: "tap duration (default 200 ms: a game that polls the pointer once per frame misses an 80 ms tap; >800 = long tap)"},
				{Name: "panel", Type: "bool", Description: "x,y are panel pixels (720x1440: top bar 0-79, OS 80-1359, soft keys 1360+) -- reaches the kernel bars and the golden menu"},
			}},
		{Name: "swipe", Capability: "input",
			Description: "Drag from x1,y1 to x2,y2 (OS screen pixels) over ms milliseconds.",
			Params: []device.Param{
				{Name: "x1", Type: "int", Required: true}, {Name: "y1", Type: "int", Required: true},
				{Name: "x2", Type: "int", Required: true}, {Name: "y2", Type: "int", Required: true},
				{Name: "ms", Type: "int", Description: "duration (default 300)"},
				{Name: "panel", Type: "bool", Description: "coordinates are panel pixels (720x1440), as for touch"},
			}},
	}
}

var reVersion = regexp.MustCompile(`phlink: (?:V proto=([0-9a-fA-F]+) caps=(\S+)|rx 'V')`)

type Version struct {
	Proto int      `json:"proto"`
	Caps  []string `json:"caps"`
}

func (d *Driver) version(ctx context.Context, l device.Link) (Version, error) {
	m := l.Mark()
	if err := l.Write(ctx, []byte{'V'}); err != nil {
		return Version{}, err
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := l.WaitLine(c, m, reVersion)
	if err != nil {
		return Version{}, fmt.Errorf("no answer to V: %w", err)
	}
	if r[1] == "" {
		return Version{Proto: 1, Caps: []string{}}, nil // an old image echoes unknown bytes
	}
	p, _ := strconv.ParseUint(r[1], 16, 32)
	return Version{Proto: int(p), Caps: strings.Split(r[2], ",")}, nil
}

var reShotErr = regexp.MustCompile(`phlink: (S (?:busy|no picture|map failed|row too wide).*|rx 'S')`)

type Shot struct {
	Blob    string  `json:"blob"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Wire    int     `json:"wire_bytes"`
	PNG     int     `json:"png_bytes"`
	Seconds float64 `json:"seconds"`
	Tiles   int     `json:"tiles,omitempty"` // live view: tiles carried by this frame
	Key     bool    `json:"key,omitempty"`   // live view: a keyframe (every tile)
	Data    []byte  `json:"-"`               // the PNG itself when not stored (live view)
}

// PNGBytes lets the API's live view (GET .../frame) take the image without a blob (any driver can implement it).
func (s Shot) PNGBytes() []byte { return s.Data }

func (d *Driver) screenshot(ctx context.Context, l device.Link, which uint32, store bool) (Shot, error) {
	frames, stop := l.Frames()
	defer stop()
	m := l.Mark()
	t0 := time.Now()
	if err := l.Write(ctx, cmd9('S', which, 0)); err != nil {
		return Shot{}, err
	}
	// an error line, or the frames
	errc := make(chan string, 1)
	ec, ecancel := context.WithCancel(ctx)
	defer ecancel()
	go func() {
		if r, err := l.WaitLine(ec, m, reShotErr); err == nil {
			errc <- r[1]
		}
	}()
	var img *image.NRGBA
	var w, h, wire int
	idle := time.NewTimer(10 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return Shot{}, ctx.Err()
		case e := <-errc:
			if e == "rx 'S'" {
				return Shot{}, fmt.Errorf("the running image has no screenshot support (phlink protocol 1)")
			}
			return Shot{}, fmt.Errorf("phone: %s", e)
		case <-idle.C:
			return Shot{}, fmt.Errorf("screenshot stalled (no frame for 10 s)")
		case f := <-frames:
			idle.Reset(10 * time.Second)
			wire += 16 + len(f.Payload)
			if !f.SumOK {
				return Shot{}, fmt.Errorf("screenshot frame %d: checksum mismatch", f.Seq)
			}
			p := f.Payload
			switch f.Type {
			case frameShotBegin:
				if len(p) < 16 {
					return Shot{}, fmt.Errorf("short begin frame")
				}
				wh := binary.LittleEndian.Uint32(p[4:])
				w, h = int(wh&0xffff), int(wh>>16)
				if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
					return Shot{}, fmt.Errorf("bad screenshot size %dx%d", w, h)
				}
				img = image.NewNRGBA(image.Rect(0, 0, w, h))
			case frameShotRows:
				if img == nil {
					continue // rows of a capture started before we subscribed
				}
				if len(p) < 4 {
					return Shot{}, fmt.Errorf("short rows frame")
				}
				row := int(binary.LittleEndian.Uint16(p))
				rows := int(binary.LittleEndian.Uint16(p[2:]))
				if err := decodeRows(img, row, rows, p[4:]); err != nil {
					return Shot{}, err
				}
			case frameShotEnd:
				if img == nil {
					continue
				}
				var buf bytes.Buffer
				enc := png.Encoder{}
				if !store {
					enc.CompressionLevel = png.BestSpeed // live frames: speed over size
				}
				if err := enc.Encode(&buf, img); err != nil {
					return Shot{}, err
				}
				if !store {
					return Shot{Width: w, Height: h, Wire: wire, PNG: buf.Len(), Seconds: time.Since(t0).Seconds(), Data: buf.Bytes()}, nil
				}
				name := fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
				sha, err := l.PutBlob("screenshot", name, map[string]any{"width": w, "height": h, "which": which}, buf.Bytes())
				if err != nil {
					return Shot{}, err
				}
				return Shot{Blob: sha, Width: w, Height: h, Wire: wire, PNG: buf.Len(), Seconds: time.Since(t0).Seconds()}, nil
			}
		}
	}
}

// decodeRows expands the RLE (phlink.cpp RleRow) of rows starting at row into img.
func decodeRows(img *image.NRGBA, row, rows int, rle []byte) error {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if row+rows > h {
		return fmt.Errorf("rows %d+%d past the bottom (%d)", row, rows, h)
	}
	i := 0
	put := func(x, y int) error {
		if i+3 > len(rle) {
			return fmt.Errorf("rle truncated at row %d", y)
		}
		img.SetNRGBA(x, y, color.NRGBA{rle[i], rle[i+1], rle[i+2], 255})
		return nil
	}
	for y := row; y < row+rows; y++ {
		for x := 0; x < w; {
			if i >= len(rle) {
				return fmt.Errorf("rle truncated at row %d", y)
			}
			t := rle[i]
			i++
			if t < 0x80 {
				n := int(t) + 1
				if x+n > w {
					return fmt.Errorf("rle literal past the row end at %d,%d", x, y)
				}
				for k := 0; k < n; k++ {
					if err := put(x+k, y); err != nil {
						return err
					}
					i += 3
				}
				x += n
			} else {
				n := int(t) - 0x80 + 2
				if x+n > w {
					return fmt.Errorf("rle run past the row end at %d,%d", x, y)
				}
				for k := 0; k < n; k++ {
					if err := put(x+k, y); err != nil {
						return err
					}
				}
				i += 3
				x += n
			}
		}
	}
	if i != len(rle) {
		return fmt.Errorf("rle: %d bytes left over", len(rle)-i)
	}
	return nil
}

func keyCode(k string) (uint32, error) {
	k = strings.ToLower(strings.TrimSpace(k))
	if v, ok := Keys[k]; ok {
		return v, nil
	}
	if len(k) == 1 {
		c := k[0]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z':
			return uint32(strings.ToUpper(k)[0]), nil // scan codes for digits/letters are their ASCII
		case c == '*':
			return 0x2a, nil
		case c == '#':
			return 0x7f, nil
		}
	}
	v, err := strconv.ParseUint(k, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("unknown key %q", k)
	}
	return uint32(v), nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Driver) press(ctx context.Context, l device.Link, code uint32, action string, hold time.Duration) error {
	switch action {
	case "down":
		return l.Write(ctx, cmd9('K', code, 0))
	case "up":
		return l.Write(ctx, cmd9('K', code, 1))
	case "", "press":
		if err := l.Write(ctx, cmd9('K', code, 0)); err != nil {
			return err
		}
		if err := sleep(ctx, hold); err != nil {
			l.Write(context.WithoutCancel(ctx), cmd9('K', code, 1))
			return err
		}
		return l.Write(ctx, cmd9('K', code, 1))
	}
	return fmt.Errorf("action must be press, down or up")
}

func clampXY(x, y uint64) (uint32, error) {
	if x >= ScreenW || y >= ScreenH {
		return 0, fmt.Errorf("%d,%d is off the %dx%d screen", x, y, ScreenW, ScreenH)
	}
	return uint32(x) | uint32(y)<<16, nil
}

func (d *Driver) touch(ctx context.Context, l device.Link, xy uint32, action string, hold time.Duration, fl uint32) error {
	switch action {
	case "down":
		return l.Write(ctx, cmd9('T', xy, 0|fl))
	case "move":
		return l.Write(ctx, cmd9('T', xy, 1|fl))
	case "up":
		return l.Write(ctx, cmd9('T', xy, 2|fl))
	case "", "tap":
		if err := l.Write(ctx, cmd9('T', xy, 0|fl)); err != nil {
			return err
		}
		if err := sleep(ctx, hold); err != nil {
			l.Write(context.WithoutCancel(ctx), cmd9('T', xy, 2|fl))
			return err
		}
		return l.Write(ctx, cmd9('T', xy, 2|fl))
	}
	return fmt.Errorf("action must be tap, down, move or up")
}

func (d *Driver) execRemote(ctx context.Context, l device.Link, op string, a device.Args) (any, bool, error) {
	str := func(k string) string { s, _ := a[k].(string); return s }
	ms := func(k string, def uint64) (time.Duration, error) {
		v, err := a.UintOr(k, 32, def)
		return time.Duration(min(v, 10000)) * time.Millisecond, err
	}
	if res, ok, err := d.execFS(ctx, l, op, a); ok {
		return res, true, err
	}
	switch op {
	case "profile":
		top, err := a.UintOr("top", 16, 25)
		if err != nil {
			return nil, true, err
		}
		win, err := ms("window_ms", 1000)
		if err != nil {
			return nil, true, err
		}
		p, err := d.profile(ctx, l, int(top), win)
		return p, true, err
	case "hotspots":
		action := "run"
		if a.Has("action") {
			s, err := a.String("action")
			if err != nil {
				return nil, true, err
			}
			action = s
		}
		if action != "run" && action != "start" && action != "stop" {
			return nil, true, fmt.Errorf("action must be run, start or stop")
		}
		secs, err := a.UintOr("seconds", 16, 10)
		if err != nil {
			return nil, true, err
		}
		if secs < 1 || secs > 300 {
			return nil, true, fmt.Errorf("seconds must be 1..300")
		}
		top, err := a.UintOr("top", 16, 40)
		if err != nil {
			return nil, true, err
		}
		syms := ""
		if a.Has("symbols") {
			if syms, err = a.String("symbols"); err != nil {
				return nil, true, err
			}
		}
		r, err := d.hotspots(ctx, l, action, time.Duration(secs)*time.Second, int(top), syms)
		return r, true, err
	case "threads":
		syms, filter := "", ""
		if a.Has("symbols") {
			s, err := a.String("symbols")
			if err != nil {
				return nil, true, err
			}
			syms = s
		}
		if a.Has("filter") {
			s, err := a.String("filter")
			if err != nil {
				return nil, true, err
			}
			filter = strings.ToLower(s)
		}
		r, err := d.threads(ctx, l, syms)
		if err == nil && filter != "" {
			var keep []ThreadInfo
			for _, t := range r.Threads {
				if strings.Contains(strings.ToLower(t.Thread), filter) {
					keep = append(keep, t)
				}
			}
			r.Threads = keep
		}
		return r, true, err
	case "version":
		v, err := d.version(ctx, l)
		return v, true, err
	case "screenshot":
		which, err := a.UintOr("which", 32, 0)
		if err != nil {
			return nil, true, err
		}
		store := true
		if v, ok := a["store"].(bool); ok {
			store = v
		}
		if live, _ := a["live"].(bool); live && !store {
			s, err := d.liveShot(ctx, l, uint32(which))
			return s, true, err
		}
		s, err := d.screenshot(ctx, l, uint32(which), store)
		return s, true, err
	case "key":
		code, err := keyCode(str("key"))
		if err != nil {
			return nil, true, err
		}
		hold, err := ms("hold_ms", 100)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"scan_code": fmt.Sprintf("0x%02x", code)}, true, d.press(ctx, l, code, str("action"), hold)
	case "text":
		t := str("text")
		for _, r := range t {
			var k string
			switch r {
			case ' ':
				k = "space"
			case '\n':
				k = "enter"
			default:
				k = string(r)
			}
			code, err := keyCode(k)
			if err != nil {
				return nil, true, fmt.Errorf("cannot type %q: %w", r, err)
			}
			if err := d.press(ctx, l, code, "press", 60*time.Millisecond); err != nil {
				return nil, true, err
			}
			if err := sleep(ctx, 60*time.Millisecond); err != nil {
				return nil, true, err
			}
		}
		return map[string]any{"typed": len(t)}, true, nil
	case "touch":
		x, err := a.Uint("x", 16)
		if err != nil {
			return nil, true, err
		}
		y, err := a.Uint("y", 16)
		if err != nil {
			return nil, true, err
		}
		panel, _ := a["panel"].(bool)
		var fl uint32
		var xy uint32
		if panel {
			if x >= 720 || y >= 1440 {
				return nil, true, fmt.Errorf("%d,%d is off the 720x1440 panel", x, y)
			}
			fl, xy, err = 0x10, uint32(x)|uint32(y)<<16, nil
		} else {
			xy, err = clampXY(x, y)
		}
		if err != nil {
			return nil, true, err
		}
		hold, err := ms("hold_ms", 200)
		if err != nil {
			return nil, true, err
		}
		return nil, true, d.touch(ctx, l, xy, str("action"), hold, fl)
	case "swipe":
		var p [4]uint64
		for i, k := range []string{"x1", "y1", "x2", "y2"} {
			v, err := a.Uint(k, 16)
			if err != nil {
				return nil, true, err
			}
			p[i] = v
		}
		panel, _ := a["panel"].(bool)
		var fl uint32
		if panel {
			fl = 0x10
			if p[0] >= 720 || p[2] >= 720 || p[1] >= 1440 || p[3] >= 1440 {
				return nil, true, fmt.Errorf("swipe off the 720x1440 panel")
			}
		} else {
			if _, err := clampXY(p[0], p[1]); err != nil {
				return nil, true, err
			}
			if _, err := clampXY(p[2], p[3]); err != nil {
				return nil, true, err
			}
		}
		dur, err := ms("ms", 300)
		if err != nil {
			return nil, true, err
		}
		const steps = 12
		at := func(i int) uint32 {
			x := int(p[0]) + (int(p[2])-int(p[0]))*i/steps
			y := int(p[1]) + (int(p[3])-int(p[1]))*i/steps
			return uint32(x) | uint32(y)<<16
		}
		if err := l.Write(ctx, cmd9('T', at(0), 0|fl)); err != nil {
			return nil, true, err
		}
		for i := 1; i <= steps; i++ {
			if err := sleep(ctx, dur/steps); err != nil {
				l.Write(context.WithoutCancel(ctx), cmd9('T', at(i), 2|fl))
				return nil, true, err
			}
			if err := l.Write(ctx, cmd9('T', at(i), 1|fl)); err != nil {
				return nil, true, err
			}
		}
		return nil, true, l.Write(ctx, cmd9('T', at(steps), 2|fl))
	}
	return nil, false, nil
}
