// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package phlink

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func recRecord(frame uint32, us int64, flags uint32, white uint16, au []byte) []byte {
	h := make([]byte, recHead)
	copy(h, "PREC")
	binary.LittleEndian.PutUint32(h[4:], frame)
	binary.LittleEndian.PutUint64(h[8:], uint64(us))
	binary.LittleEndian.PutUint32(h[16:], flags)
	binary.LittleEndian.PutUint16(h[20:], 120)
	binary.LittleEndian.PutUint16(h[22:], white)
	binary.LittleEndian.PutUint32(h[24:], uint32(len(au)))
	return append(h, au...)
}

// findBox: the payload of the first box of type typ inside b (boxes nested along path)
func findBox(t *testing.T, b []byte, path ...string) (payload []byte, at int) {
	off := 0
	for _, typ := range path {
		found := false
		for p := 0; p+8 <= len(b); {
			n := int(binary.BigEndian.Uint32(b[p:]))
			if n < 8 || p+n > len(b) {
				t.Fatalf("bad box size %d at %d", n, p)
			}
			if string(b[p+4:p+8]) == typ {
				off += p + 8
				b = b[p+8 : p+n]
				found = true
				break
			}
			p += n
		}
		if !found {
			t.Fatalf("no box %q", typ)
		}
	}
	return b, off
}

func TestFinishRecording(t *testing.T) {
	sps := []byte{0, 0, 0, 1, 0x67, 77, 0, 41, 0xAA}
	pps := []byte{0, 0, 0, 1, 0x68, 0xCE}
	idr := []byte{0, 0, 0, 1, 0x65, 1, 2, 3, 4}
	p1 := []byte{0, 0, 0, 1, 0x41, 9, 9}
	var st []byte
	st = append(st, recRecord(0, 0, recFlagIDR, 10, append(append(append([]byte{}, sps...), pps...), idr...))...)
	st = append(st, recRecord(1, 40000, 0, 900, p1)...) // a white picture, on screen 60 ms
	st = append(st, recRecord(2, 100000, 0, 5, p1)...)
	st = append(st, recRecord(3, 200000, recFlagEnd, 0, nil)...)
	video, index, summary, err := (&Driver{}).FinishRecording(st)
	if err != nil {
		t.Fatal(err)
	}
	if fr := index.([]RecFrame); len(fr) != 3 || !fr[0].IDR || fr[1].White != 900 {
		t.Fatalf("index %+v", fr)
	}
	fl := summary["white_pictures"].([]map[string]any)
	if len(fl) != 1 || fl[0]["frame"].(uint32) != 1 || fl[0]["shown_ms"].(float64) != 60 {
		t.Fatalf("white pictures %+v", fl)
	}
	// the MP4: avcC holds the SPS, the samples are length-prefixed without parameter sets, stco points at the first one
	avcC, _ := findBox(t, video, "moov", "trak", "mdia", "minf", "stbl", "stsd")
	if !bytes.Contains(avcC, sps[4:]) || !bytes.Contains(avcC, pps[4:]) {
		t.Fatal("avcC without SPS / PPS")
	}
	stco, _ := findBox(t, video, "moov", "trak", "mdia", "minf", "stbl", "stco")
	off := binary.BigEndian.Uint32(stco[8:])
	want := append([]byte{0, 0, 0, 5}, idr[4:]...)
	if !bytes.Equal(video[off:off+uint32(len(want))], want) {
		t.Fatalf("stco %d does not point at the first sample: % x", off, video[off:off+9])
	}
	stsz, _ := findBox(t, video, "moov", "trak", "mdia", "minf", "stbl", "stsz")
	if n := binary.BigEndian.Uint32(stsz[8:]); n != 3 || binary.BigEndian.Uint32(stsz[12:]) != 9 || binary.BigEndian.Uint32(stsz[16:]) != 7 {
		t.Fatalf("stsz % x", stsz)
	}
	// durations at 90 kHz: 40 ms, 60 ms, 100 ms
	stts, _ := findBox(t, video, "moov", "trak", "mdia", "minf", "stbl", "stts")
	if binary.BigEndian.Uint32(stts[4:]) != 3 || binary.BigEndian.Uint32(stts[12:]) != 3600 || binary.BigEndian.Uint32(stts[20:]) != 5400 ||
		binary.BigEndian.Uint32(stts[28:]) != 9000 {
		t.Fatalf("stts % x", stts)
	}
}

func TestFinishRecordingSkipsToAnIDR(t *testing.T) {
	// a reader that lost the start: P pictures before the first IDR are dropped, damage before a record is skipped
	idr := []byte{0, 0, 1, 0x67, 77, 0, 41, 0, 0, 1, 0x68, 1, 0, 0, 1, 0x65, 7}
	st := append([]byte("junk"), recRecord(5, 0, 0, 0, []byte{0, 0, 1, 0x41, 1})...)
	st = append(st, recRecord(6, 33000, recFlagIDR, 0, idr)...)
	_, index, summary, err := (&Driver{}).FinishRecording(st)
	if err != nil {
		t.Fatal(err)
	}
	if fr := index.([]RecFrame); len(fr) != 1 || fr[0].Frame != 6 || summary["skipped_bytes"].(int) != 4 {
		t.Fatalf("index %+v summary %v", fr, summary)
	}
}
