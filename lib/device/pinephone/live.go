// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pinephone

// The live view (protocol v3, 'L'; phlink.cpp "Protocol v3", docs/phonelab.md): the phone sends only the
// 32x32 tiles that changed since the last frame it sent, each LZ4-compressed. This keeps the picture the phone
// thinks we hold (fb); any doubt (an error, a gap in the frame sequence, a size change, every liveKeyEvery frames)
// makes the next request a KEYFRAME (flag bit 0 = every tile again).

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/lz4"
)

const (
	frameLiveBegin = 8
	frameLiveTiles = 9
	frameLiveEnd   = 10

	liveKeyEvery = 100 // a keyframe at least this often: bounds any drift (a hash collision on the phone) and a lost ack
)

var reLiveErr = regexp.MustCompile(`phlink: (L (?:busy|no picture).*|rx 'L')`)

type liveState struct {
	mu          sync.Mutex
	fb          *image.NRGBA
	have        bool // fb is exactly what the phone's reference describes
	which       uint32
	since       int // frames since the last keyframe
	png         []byte
	noLiveUntil time.Time // an image without 'L': plain screenshots until then
}

type noLiveError struct{}

func (noLiveError) Error() string { return "the running image has no live view (phlink protocol < 3)" }

var errNoLive error = noLiveError{}

// liveShot is the screenshot op's "live" mode (not stored): the PNG of the current screen, built from changed tiles.
func (d *Driver) liveShot(ctx context.Context, l device.Link, which uint32) (Shot, error) {
	st := &d.live // one driver per device
	st.mu.Lock()
	defer st.mu.Unlock()
	if time.Now().Before(st.noLiveUntil) {
		return d.screenshot(ctx, l, which, false)
	}
	s, err := d.liveFrame(ctx, l, st, which)
	if err != nil {
		st.have = false // whatever went wrong, the next request asks for everything again
		if err == errNoLive {
			st.noLiveUntil = time.Now().Add(30 * time.Second)
			return d.screenshot(ctx, l, which, false)
		}
	}
	return s, err
}

func (d *Driver) liveFrame(ctx context.Context, l device.Link, st *liveState, which uint32) (Shot, error) {
	key := !st.have || st.which != which || st.since >= liveKeyEvery
	frames, stop := l.Frames()
	defer stop()
	m := l.Mark()
	t0 := time.Now()
	var flags uint32
	if key {
		flags = 1
	}
	if err := l.Write(ctx, cmd9('L', which, flags)); err != nil {
		return Shot{}, err
	}
	errc := make(chan string, 1)
	ec, ecancel := context.WithCancel(ctx)
	defer ecancel()
	go func() {
		if r, err := l.WaitLine(ec, m, reLiveErr); err == nil {
			errc <- r[1]
		}
	}()
	var w, h, tile, wire, tiles int
	var gotKey, inFrame, changed bool
	var seq uint16
	idle := time.NewTimer(10 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return Shot{}, ctx.Err()
		case e := <-errc:
			if e == "rx 'L'" {
				return Shot{}, errNoLive
			}
			return Shot{}, fmt.Errorf("phone: %s", e)
		case <-idle.C:
			return Shot{}, fmt.Errorf("live frame stalled (no frame for 10 s)")
		case f := <-frames:
			idle.Reset(10 * time.Second)
			if !f.SumOK {
				return Shot{}, fmt.Errorf("live frame %d: checksum mismatch", f.Seq)
			}
			if inFrame && f.Seq != seq+1 {
				return Shot{}, fmt.Errorf("live view: frame sequence gap (%d after %d)", f.Seq, seq)
			}
			seq = f.Seq
			p := f.Payload
			switch f.Type {
			case frameLiveBegin:
				if len(p) < 16 {
					return Shot{}, fmt.Errorf("short live begin frame")
				}
				wh := binary.LittleEndian.Uint32(p[4:])
				w, h = int(wh&0xffff), int(wh>>16)
				tile = int(binary.LittleEndian.Uint32(p[8:]))
				gotKey = binary.LittleEndian.Uint32(p[12:])&1 != 0
				if w <= 0 || h <= 0 || w > 4096 || h > 4096 || tile <= 0 || tile > 64 {
					return Shot{}, fmt.Errorf("bad live frame %dx%d tile %d", w, h, tile)
				}
				if !gotKey && (st.fb == nil || !st.have || st.fb.Rect.Dx() != w || st.fb.Rect.Dy() != h) {
					return Shot{}, fmt.Errorf("live: a diff frame without a matching picture")
				}
				if gotKey || st.fb == nil || st.fb.Rect.Dx() != w || st.fb.Rect.Dy() != h {
					st.fb = image.NewNRGBA(image.Rect(0, 0, w, h))
					changed = true
				}
				inFrame, tiles, wire = true, 0, 16+len(p)
				st.have = false // until the end frame: the picture is half-updated
			case frameLiveTiles:
				if !inFrame {
					continue // the tail of a frame from before we asked
				}
				wire += 16 + len(p)
				n, err := applyTiles(st.fb, tile, p)
				if err != nil {
					return Shot{}, err
				}
				tiles += n
				if n > 0 {
					changed = true
				}
			case frameLiveEnd:
				if !inFrame {
					continue
				}
				wire += 16 + len(p)
				if len(p) < 8 || int(binary.LittleEndian.Uint32(p[4:])) != tiles {
					return Shot{}, fmt.Errorf("live: tile count mismatch (%d received)", tiles)
				}
				st.have, st.which = true, which
				if gotKey {
					st.since = 0
				} else {
					st.since++
				}
				if changed || st.png == nil {
					var buf bytes.Buffer
					enc := png.Encoder{CompressionLevel: png.BestSpeed}
					if err := enc.Encode(&buf, st.fb); err != nil {
						return Shot{}, err
					}
					st.png = buf.Bytes()
				}
				return Shot{Width: w, Height: h, Wire: wire, PNG: len(st.png), Seconds: time.Since(t0).Seconds(),
					Tiles: tiles, Key: gotKey, Data: st.png}, nil
			}
		}
	}
}

// applyTiles decodes one tiles frame payload {count LE16, 0 LE16, count x {tx, ty, clen LE16, data}} into img.
func applyTiles(img *image.NRGBA, tile int, p []byte) (int, error) {
	if len(p) < 4 {
		return 0, fmt.Errorf("short live tiles frame")
	}
	count := int(binary.LittleEndian.Uint16(p))
	w, h := img.Rect.Dx(), img.Rect.Dy()
	i := 4
	var raw [64 * 64 * 3]byte
	for k := 0; k < count; k++ {
		if i+4 > len(p) {
			return 0, fmt.Errorf("live tile %d: truncated header", k)
		}
		tx, ty := int(p[i])*tile, int(p[i+1])*tile
		cl := int(binary.LittleEndian.Uint16(p[i+2:]))
		i += 4
		stored := cl&0x8000 != 0
		cl &^= 0x8000
		if tx >= w || ty >= h || i+cl > len(p) {
			return 0, fmt.Errorf("live tile %d at %d,%d: out of range", k, tx, ty)
		}
		tw, th := min(tile, w-tx), min(tile, h-ty)
		r := raw[:tw*th*3]
		if stored {
			if cl != len(r) {
				return 0, fmt.Errorf("live tile %d: stored size %d, want %d", k, cl, len(r))
			}
			copy(r, p[i:i+cl])
		} else if err := lz4.Decode(r, p[i:i+cl]); err != nil {
			return 0, fmt.Errorf("live tile %d at %d,%d: %w", k, tx, ty, err)
		}
		i += cl
		for y := 0; y < th; y++ {
			o := img.PixOffset(tx, ty+y)
			for x := 0; x < tw; x++ {
				s := (y*tw + x) * 3
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r[s], r[s+1], r[s+2], 255
				o += 4
			}
		}
	}
	if i != len(p) {
		return 0, fmt.Errorf("live tiles: %d bytes left over", len(p)-i)
	}
	return count, nil
}
