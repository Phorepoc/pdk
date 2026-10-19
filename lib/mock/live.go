package mock

import (
	"encoding/binary"

	"github.com/Phorepoc/pdk/lib/lz4"
)

// The live view's frames (protocol v3, phlink.cpp LiveFrame): 32x32 tiles whose pixels differ from the reference
// (the picture as last sent) or all of them in a keyframe. The tile data is an all-literals LZ4 block (the real
// compressor is C: tested against the Go decoder in internal/lz4); a tile whose block would not be smaller is
// stored raw (clen bit 15), as on the phone.

const liveTile = 32

type LiveRef struct {
	ok         bool
	w, h       int
	which      uint32
	pix        []uint32
	seq        uint16 // the phone's frame counter (one per link, not per capture)
	TilesSent  int    // statistics for tests
	FramesSent int
}

// LiveFrames returns the frames of one 'L' answer and updates the reference, as the phone does once they are sent.
func (r *LiveRef) LiveFrames(fb []uint32, w, h int, which uint32, id uint32, wantKey bool) [][]byte {
	key := wantKey || !r.ok || r.which != which || r.w != w || r.h != h
	var out [][]byte
	add := func(t byte, p []byte) { out = append(out, frame(t, r.seq, p)); r.seq++ }
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b, id)
	binary.LittleEndian.PutUint32(b[4:], uint32(h)<<16|uint32(w))
	binary.LittleEndian.PutUint32(b[8:], liveTile)
	if key {
		binary.LittleEndian.PutUint32(b[12:], 1)
	}
	add(8, b)
	r.ok = false
	if len(r.pix) != w*h {
		r.pix = make([]uint32, w*h)
	}
	ntx, nty := (w+liveTile-1)/liveTile, (h+liveTile-1)/liveTile
	var cur []byte
	count, total := 0, 0
	flush := func() {
		if count == 0 {
			return
		}
		binary.LittleEndian.PutUint16(cur, uint16(count))
		add(9, cur)
		cur, count = nil, 0
	}
	for ty := 0; ty < nty; ty++ {
		for tx := 0; tx < ntx; tx++ {
			x0, y0 := tx*liveTile, ty*liveTile
			tw, th := min(liveTile, w-x0), min(liveTile, h-y0)
			same := true
			raw := make([]byte, 0, tw*th*3)
			for y := 0; y < th; y++ {
				for x := 0; x < tw; x++ {
					p := fb[(y0+y)*w+x0+x] & 0xFFFFFF
					if p != r.pix[(y0+y)*w+x0+x]&0xFFFFFF {
						same = false
					}
					raw = append(raw, byte(p>>16), byte(p>>8), byte(p))
				}
			}
			if same && !key {
				continue
			}
			data, cl := lz4.Literals(raw), 0
			if len(data) >= len(raw) {
				data, cl = raw, 0x8000
			}
			cl |= len(data) & 0x7FFF
			if cur != nil && 16+len(cur)+4+len(data) > frameMax {
				flush()
			}
			if cur == nil {
				cur = []byte{0, 0, 0, 0}
			}
			cur = append(cur, byte(tx), byte(ty), byte(cl), byte(cl>>8))
			cur = append(cur, data...)
			count++
			total++
		}
	}
	flush()
	e := make([]byte, 12)
	binary.LittleEndian.PutUint32(e, id)
	binary.LittleEndian.PutUint32(e[4:], uint32(total))
	add(10, e)
	copy(r.pix, fb)
	r.ok, r.w, r.h, r.which = true, w, h, which
	r.TilesSent += total
	r.FramesSent++
	return out
}
