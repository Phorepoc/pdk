// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package mp4 writes an H.264 elementary stream into an MP4 file (ISO/IEC 14496-12 + 14496-15): one video track with
// variable frame timing, every sample with its own duration. Enough for a screen recording to play in a browser or
// any player; no audio, no fragments, no B-frames (composition time = decode time).
package mp4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Sample is one access unit in Annex B form (start codes) with its start time.
type Sample struct {
	AnnexB []byte
	TimeUs int64
	Key    bool // an IDR picture
}

// NALs splits an Annex B buffer into NAL units (without start codes).
func NALs(b []byte) [][]byte {
	var out [][]byte
	start := -1
	for i := 0; i+2 < len(b); {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if start >= 0 {
				end := i
				for end > start && b[end-1] == 0 { // a 4-byte start code's leading zero belongs to it
					end--
				}
				out = append(out, b[start:end])
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// Write muxes samples (in decode order, the first one an IDR with SPS + PPS) of a width x height picture. endUs is
// the end of the last sample (its duration = endUs - its time).
func Write(samples []Sample, width, height int, endUs int64) ([]byte, error) {
	if len(samples) == 0 {
		return nil, errors.New("mp4: no samples")
	}
	var sps, pps []byte
	for _, n := range NALs(samples[0].AnnexB) {
		switch n[0] & 0x1F {
		case 7:
			sps = n
		case 8:
			pps = n
		}
	}
	if len(sps) < 4 || len(pps) == 0 {
		return nil, errors.New("mp4: the first sample has no SPS / PPS")
	}
	const timescale = 90000
	ts := func(us int64) uint64 { return uint64((us*timescale + 500000) / 1000000) }
	var mdat bytes.Buffer
	sizes := make([]uint32, len(samples))
	durs := make([]uint32, len(samples))
	var keys []uint32
	for i, s := range samples {
		n0 := mdat.Len()
		for _, n := range NALs(s.AnnexB) {
			t := n[0] & 0x1F
			if t == 7 || t == 8 || t == 9 { // parameter sets live in avcC; access unit delimiters are not needed
				continue
			}
			binary.Write(&mdat, binary.BigEndian, uint32(len(n)))
			mdat.Write(n)
		}
		sizes[i] = uint32(mdat.Len() - n0)
		next := endUs
		if i+1 < len(samples) {
			next = samples[i+1].TimeUs
		}
		d := int64(ts(next)) - int64(ts(s.TimeUs))
		if d <= 0 {
			d = 1
		}
		durs[i] = uint32(d)
		if s.Key {
			keys = append(keys, uint32(i+1))
		}
	}
	var total uint64
	for _, d := range durs {
		total += uint64(d)
	}

	ftyp := box("ftyp", cat([]byte("isom"), u32(0x200), []byte("isomiso2avc1mp41")))
	// sample table
	stts := fullBox("stts", 0, 0, sttsBody(durs))
	stss := fullBox("stss", 0, 0, cat(u32(uint32(len(keys))), u32s(keys)))
	stsc := fullBox("stsc", 0, 0, cat(u32(1), u32(1), u32(uint32(len(samples))), u32(1))) // one chunk with every sample
	stsz := fullBox("stsz", 0, 0, cat(u32(0), u32(uint32(len(samples))), u32s(sizes)))
	avcC := box("avcC", cat([]byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1}, u16(uint16(len(sps))), sps, []byte{1}, u16(uint16(len(pps))), pps))
	avc1 := box("avc1", cat(make([]byte, 6), u16(1), make([]byte, 16), u16(uint16(width)), u16(uint16(height)),
		u32(0x00480000), u32(0x00480000), u32(0), u16(1), make([]byte, 32), u16(0x18), u16(0xFFFF), avcC))
	stsd := fullBox("stsd", 0, 0, cat(u32(1), avc1))
	// stco is written last: its offset depends on the size of everything in front of mdat
	build := func(chunkOff uint32) []byte {
		stco := fullBox("stco", 0, 0, cat(u32(1), u32(chunkOff)))
		stbl := box("stbl", cat(stsd, stts, stss, stsc, stsz, stco))
		dref := fullBox("dref", 0, 0, cat(u32(1), fullBox("url ", 0, 1, nil)))
		minf := box("minf", cat(fullBox("vmhd", 0, 1, make([]byte, 8)), box("dinf", dref), stbl))
		hdlr := fullBox("hdlr", 0, 0, cat(u32(0), []byte("vide"), make([]byte, 12), []byte("phonelab screen\x00")))
		mdhd := fullBox("mdhd", 0, 0, cat(u32(0), u32(0), u32(timescale), u32(uint32(total)), u16(0x55C4), u16(0)))
		mdia := box("mdia", cat(mdhd, hdlr, minf))
		matrix := cat(u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000))
		tkhd := fullBox("tkhd", 0, 3, cat(u32(0), u32(0), u32(1), u32(0), u32(uint32(total)), make([]byte, 8), u16(0), u16(0),
			u16(0), u16(0), matrix, u32(uint32(width)<<16), u32(uint32(height)<<16)))
		trak := box("trak", cat(tkhd, mdia))
		mvhd := fullBox("mvhd", 0, 0, cat(u32(0), u32(0), u32(timescale), u32(uint32(total)), u32(0x10000), u16(0x100),
			make([]byte, 10), matrix, make([]byte, 24), u32(2)))
		return box("moov", cat(mvhd, trak))
	}
	moov := build(0)
	off := uint64(len(ftyp)+len(moov)) + 8
	if off+uint64(mdat.Len()) > 0xFFFFFFFF {
		return nil, fmt.Errorf("mp4: %d bytes of video is more than a 32-bit file holds", mdat.Len())
	}
	moov = build(uint32(off))
	out := cat(ftyp, moov, u32(uint32(8+mdat.Len())), []byte("mdat"), mdat.Bytes())
	return out, nil
}

func sttsBody(durs []uint32) []byte {
	var runs [][2]uint32
	for _, d := range durs {
		if n := len(runs); n > 0 && runs[n-1][1] == d {
			runs[n-1][0]++
			continue
		}
		runs = append(runs, [2]uint32{1, d})
	}
	b := u32(uint32(len(runs)))
	for _, r := range runs {
		b = append(b, cat(u32(r[0]), u32(r[1]))...)
	}
	return b
}

func box(typ string, body []byte) []byte { return cat(u32(uint32(8+len(body))), []byte(typ), body) }
func fullBox(typ string, version byte, flags uint32, body []byte) []byte {
	return box(typ, cat([]byte{version, byte(flags >> 16), byte(flags >> 8), byte(flags)}, body))
}
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32s(v []uint32) []byte {
	var b []byte
	for _, x := range v {
		b = binary.BigEndian.AppendUint32(b, x)
	}
	return b
}
func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
