// Package lz4 decodes LZ4 *blocks* (the format the phone's phlz4.h encoder writes; no frame format, the size is
// known from context). Pure Go, no dependency.
package lz4

import "errors"

var ErrCorrupt = errors.New("lz4: corrupt block")

// Decode expands src into dst, which must be exactly the decoded size (the live view knows it: tile w*h*3).
func Decode(dst, src []byte) error {
	si, di := 0, 0
	for {
		if si >= len(src) {
			return ErrCorrupt
		}
		tok := int(src[si])
		si++
		lit := tok >> 4
		if lit == 15 {
			for {
				if si >= len(src) {
					return ErrCorrupt
				}
				b := int(src[si])
				si++
				lit += b
				if b != 255 {
					break
				}
			}
		}
		if lit > len(src)-si || lit > len(dst)-di {
			return ErrCorrupt
		}
		copy(dst[di:], src[si:si+lit])
		si += lit
		di += lit
		if si == len(src) { // the last sequence has literals only
			if di != len(dst) {
				return ErrCorrupt
			}
			return nil
		}
		if si+2 > len(src) {
			return ErrCorrupt
		}
		off := int(src[si]) | int(src[si+1])<<8
		si += 2
		if off == 0 || off > di {
			return ErrCorrupt
		}
		ml := tok & 15
		if ml == 15 {
			for {
				if si >= len(src) {
					return ErrCorrupt
				}
				b := int(src[si])
				si++
				ml += b
				if b != 255 {
					break
				}
			}
		}
		ml += 4
		if ml > len(dst)-di {
			return ErrCorrupt
		}
		for i := 0; i < ml; i++ { // byte by byte: matches may overlap their own output
			dst[di+i] = dst[di+i-off]
		}
		di += ml
	}
}

// Literals wraps src in a valid LZ4 block made of literals only (no matches). For tests and the mock phone, which
// need a block without the real (C) encoder.
func Literals(src []byte) []byte {
	n := len(src)
	var out []byte
	if n < 15 {
		out = append(out, byte(n<<4))
	} else {
		out = append(out, 15<<4)
		rem := n - 15
		for ; rem >= 255; rem -= 255 {
			out = append(out, 255)
		}
		out = append(out, byte(rem))
	}
	return append(out, src...)
}
