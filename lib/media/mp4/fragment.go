// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package mp4

// Fragmented MP4 (ISO/IEC 14496-12 movie fragments) for a live stream: Init once (ftyp + moov with mvex), then one
// Fragment (moof + mdat) per batch of access units. What a browser's Media Source Extensions play on plain http --
// the live video view, which needs neither WebCodecs nor a secure context.

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const fragTimescale = 90000

// ParamSets returns the SPS and PPS of an Annex B access unit (an IDR carries both).
func ParamSets(au []byte) (sps, pps []byte) {
	for _, n := range NALs(au) {
		switch n[0] & 0x1F {
		case 7:
			sps = n
		case 8:
			pps = n
		}
	}
	return
}

// Codec is the RFC 6381 codec string of an SPS ("avc1.PPCCLL"), what MediaSource.addSourceBuffer wants.
func Codec(sps []byte) string {
	const hex = "0123456789abcdef"
	b := []byte("avc1.")
	for _, x := range sps[1:4] {
		b = append(b, hex[x>>4], hex[x&15])
	}
	return string(b)
}

// Init is the initialization segment of a width x height H.264 track.
func Init(sps, pps []byte, width, height int) ([]byte, error) {
	if len(sps) < 4 || len(pps) == 0 {
		return nil, errors.New("mp4: no SPS / PPS")
	}
	ftyp := box("ftyp", cat([]byte("iso5"), u32(0x200), []byte("iso5iso6avc1mp41")))
	avcC := box("avcC", cat([]byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1}, u16(uint16(len(sps))), sps, []byte{1}, u16(uint16(len(pps))), pps))
	avc1 := box("avc1", cat(make([]byte, 6), u16(1), make([]byte, 16), u16(uint16(width)), u16(uint16(height)),
		u32(0x00480000), u32(0x00480000), u32(0), u16(1), make([]byte, 32), u16(0x18), u16(0xFFFF), avcC))
	stbl := box("stbl", cat(fullBox("stsd", 0, 0, cat(u32(1), avc1)), fullBox("stts", 0, 0, u32(0)),
		fullBox("stsc", 0, 0, u32(0)), fullBox("stsz", 0, 0, cat(u32(0), u32(0))), fullBox("stco", 0, 0, u32(0))))
	dref := fullBox("dref", 0, 0, cat(u32(1), fullBox("url ", 0, 1, nil)))
	minf := box("minf", cat(fullBox("vmhd", 0, 1, make([]byte, 8)), box("dinf", dref), stbl))
	hdlr := fullBox("hdlr", 0, 0, cat(u32(0), []byte("vide"), make([]byte, 12), []byte("phonelab screen\x00")))
	mdhd := fullBox("mdhd", 0, 0, cat(u32(0), u32(0), u32(fragTimescale), u32(0), u16(0x55C4), u16(0)))
	matrix := cat(u32(0x10000), u32(0), u32(0), u32(0), u32(0x10000), u32(0), u32(0), u32(0), u32(0x40000000))
	tkhd := fullBox("tkhd", 0, 3, cat(u32(0), u32(0), u32(1), u32(0), u32(0), make([]byte, 8), u16(0), u16(0),
		u16(0), u16(0), matrix, u32(uint32(width)<<16), u32(uint32(height)<<16)))
	trak := box("trak", cat(tkhd, box("mdia", cat(mdhd, hdlr, minf))))
	mvhd := fullBox("mvhd", 0, 0, cat(u32(0), u32(0), u32(fragTimescale), u32(0), u32(0x10000), u16(0x100),
		make([]byte, 10), matrix, make([]byte, 24), u32(2)))
	mvex := box("mvex", fullBox("trex", 0, 0, cat(u32(1), u32(1), u32(0), u32(0), u32(0))))
	return cat(ftyp, box("moov", cat(mvhd, trak, mvex))), nil
}

// Fragment is one moof + mdat of samples (decode order). Each sample lasts until the next one; the last one lastUs.
// seq counts fragments from 1.
func Fragment(seq uint32, samples []Sample, lastUs int64) []byte {
	if len(samples) == 0 {
		return nil
	}
	durs := make([]uint32, len(samples))
	for i, s := range samples {
		d := fragTicks(lastUs)
		if i+1 < len(samples) {
			d = fragTicks(samples[i+1].TimeUs) - fragTicks(s.TimeUs)
		}
		durs[i] = uint32(max(d, 1))
	}
	return fragment(seq, samples, uint64(fragTicks(samples[0].TimeUs)), durs)
}

// FragmentAt is Fragment on a continuous timeline: the first sample decodes at baseTicks (90 kHz), every sample lasts
// durTicks. A live view's player sees no gaps between fragments however irregularly the pictures came (a gap reads
// as a discontinuity to Media Source Extensions, which then drop everything up to the next IDR).
func FragmentAt(seq uint32, samples []Sample, baseTicks uint64, durTicks uint32) []byte {
	if len(samples) == 0 {
		return nil
	}
	durs := make([]uint32, len(samples))
	for i := range durs {
		durs[i] = durTicks
	}
	return fragment(seq, samples, baseTicks, durs)
}

func fragTicks(us int64) int64 { return (us*fragTimescale + 500000) / 1000000 }

// fragment: samples from decode time base (90 kHz), each lasting its durs entry
func fragment(seq uint32, samples []Sample, base uint64, durs []uint32) []byte {
	var mdat bytes.Buffer
	var entries []byte
	for i, s := range samples {
		n0 := mdat.Len()
		for _, n := range NALs(s.AnnexB) {
			if t := n[0] & 0x1F; t == 7 || t == 8 || t == 9 {
				continue // parameter sets live in avcC
			}
			binary.Write(&mdat, binary.BigEndian, uint32(len(n)))
			mdat.Write(n)
		}
		flags := uint32(0x01010000) // depends on others, a non-sync sample
		if s.Key {
			flags = 0x02000000
		}
		entries = append(entries, cat(u32(durs[i]), u32(uint32(mdat.Len()-n0)), u32(flags))...)
	}
	mfhd := fullBox("mfhd", 0, 0, u32(seq))
	tfhd := fullBox("tfhd", 0, 0x020000, u32(1)) // default-base-is-moof
	tfdt := fullBox("tfdt", 1, 0, binary.BigEndian.AppendUint64(nil, base))
	build := func(dataOff uint32) []byte {
		trun := fullBox("trun", 0, 0x000701, cat(u32(uint32(len(samples))), u32(dataOff), entries)) // offset, duration, size, flags
		return box("moof", cat(mfhd, box("traf", cat(tfhd, tfdt, trun))))
	}
	moof := build(0)
	moof = build(uint32(len(moof) + 8))
	return cat(moof, u32(uint32(8+mdat.Len())), []byte("mdat"), mdat.Bytes())
}
