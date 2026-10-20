// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package mock

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Frame + RLE encoders, ported 1:1 from phlink.cpp (ShotFrame, RleRow) so the host decoder is tested against
// the same byte layout the phone sends.

const frameMax = 4096

func frame(typ byte, seq uint16, payload []byte) []byte {
	f := make([]byte, 16+len(payload))
	copy(f, []byte{0xA5, 0x5A, 'P', 'F'})
	f[4] = typ
	binary.LittleEndian.PutUint16(f[6:], seq)
	binary.LittleEndian.PutUint32(f[8:], uint32(len(payload)))
	var sum uint32
	for _, c := range payload {
		sum += uint32(c)
	}
	binary.LittleEndian.PutUint32(f[12:], sum)
	copy(f[16:], payload)
	return f
}

// RleRow encodes one row (phlink.cpp RleRow).
func RleRow(pix []uint32, out []byte) []byte {
	w := len(pix)
	put := func(p uint32) { out = append(out, byte(p>>16), byte(p>>8), byte(p)) }
	for i := 0; i < w; {
		p := pix[i] & 0xFFFFFF
		run := 1
		for i+run < w && run < 129 && pix[i+run]&0xFFFFFF == p {
			run++
		}
		if run >= 2 {
			out = append(out, byte(0x80+run-2))
			put(p)
			i += run
			continue
		}
		j := i + 1
		for j < w && j-i < 128 && !(j+1 < w && pix[j]&0xFFFFFF == pix[j+1]&0xFFFFFF) {
			j++
		}
		out = append(out, byte(j-i-1))
		for k := i; k < j; k++ {
			put(pix[k])
		}
		i = j
	}
	return out
}

// ShotFrames returns the whole frame sequence of one screenshot.
func ShotFrames(fb []uint32, w, h int, id uint32) [][]byte {
	var out [][]byte
	var seq uint16
	add := func(t byte, p []byte) { out = append(out, frame(t, seq, p)); seq++ }
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b, id)
	binary.LittleEndian.PutUint32(b[4:], uint32(h)<<16|uint32(w))
	binary.LittleEndian.PutUint32(b[8:], uint32(w*4))
	binary.LittleEndian.PutUint32(b[12:], 1)
	add(1, b)
	worst := w*3 + (w+127)/128 + 1
	for row := 0; row < h; {
		p := []byte{0, 0, 0, 0}
		first, rows := row, 0
		for row < h && 16+len(p)+worst <= frameMax {
			p = RleRow(fb[row*w:(row+1)*w], p)
			row++
			rows++
		}
		binary.LittleEndian.PutUint16(p, uint16(first))
		binary.LittleEndian.PutUint16(p[2:], uint16(rows))
		add(2, p)
	}
	e := make([]byte, 8)
	binary.LittleEndian.PutUint32(e, id)
	binary.LittleEndian.PutUint32(e[4:], uint32(h))
	add(3, e)
	return out
}

// ProfileText fakes a64_glue.cpp A64ProfileSnapshot for snapshot n: 1 s apart (timestamp at 1 MHz), 4 cores;
// core 0 is 50% busy, Telephone's main thread uses 25% of a core, eshell 10%.
func ProfileText(n uint64) string {
	const hz = 1000000
	now := n * hz
	var b strings.Builder
	fmt.Fprintf(&b, "N %x %x 4 \n", now, uint64(hz))
	for c := 0; c < 4; c++ {
		idle := now
		if c == 0 {
			idle = now / 2
		}
		fmt.Fprintf(&b, "I %x %x \n", c, idle)
	}
	b.WriteString("P 1 ekern.exe\nP 2 Telephone[100058b3]0001\nP 3 eshell.exe[10000000]0001\n")
	for c := 0; c < 4; c++ {
		fmt.Fprintf(&b, "T %x 1 %x Null%d\n", 10+c, now/2, c)
	}
	fmt.Fprintf(&b, "T 20 2 %x Telephone\n", now/4)
	fmt.Fprintf(&b, "T 21 2 %x PhoneRingingToneThread\n", uint64(5))
	fmt.Fprintf(&b, "T 30 3 %x Main\n", now/10)
	b.WriteString("C 2 300000 $HEAP\nC 2 100000 $DAT\nC 3 80000 $HEAP\nC 0 1000000 ScreenBuffer\n")
	b.WriteString("M c8000 24b\n")
	return b.String()
}

// HotText fakes A64HotStop: 100 busy samples; Wserv spends 60 in two pcs of one function (0x80001010, 0x80001020),
// Calendar 30, an ekern DFC thread 10.
const HotText = "S 64 12c 0 2 \n" +
	"X a000 80001010 28 80002008 \nX a000 80001020 14 80002008 \nX b000 80002004 1e \nX c000 12345678 a \n" +
	"W a000 EWSRV.EXE / Wserv\nW b000 Calendar[10005901]0001 / Calendar\nW c000 ekern.exe / USB_DFC\n"

// sendBlob streams a blob the way phlink BlobFrame does (data frames of up to 4080 bytes, then the end frame).
func (p *port) sendBlob(kind uint32, data []byte) {
	var seq uint16
	for off := 0; off < len(data); off += frameMax - 16 {
		end := min(off+frameMax-16, len(data))
		if p.emit(string(frame(6, seq, data[off:end]))) {
			return
		}
		seq++
	}
	e := make([]byte, 8)
	binary.LittleEndian.PutUint32(e, kind)
	binary.LittleEndian.PutUint32(e[4:], uint32(len(data)))
	p.emit(string(frame(7, seq, e)))
}

// sendShot interleaves the frames with log text, the way Kick alternates them.
func (p *port) sendShot(fb []uint32, w, h int) {
	for i, f := range ShotFrames(fb, w, h, 1) {
		if p.emit(string(f)) {
			return
		}
		if i%7 == 3 {
			p.emit("[EKERN][mon] free RAM KB=0x000cd1b8\n")
		}
	}
}
