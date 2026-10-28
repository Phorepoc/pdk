// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

// The delta push (2026-10-09, user idea): re-send only the 64 KB blocks of the image that differ from what slot B
// already holds. Device side: the delta-upload handler of phlink 'Y' / 'X' / 'Z' (caps "delta").
//
//	'Y' len 0      -> blob kind 3: the hash of each 64 KB block of slot B over [0, len)
//	'G' total 0    -> the bytes the 'X' runs will carry: the phone's top bar shows one DELTA progress over all runs
//	'X' off len .. -> write len bytes at byte offset off (a multiple of 512): "phlink: PATCH OK|FAILED off= len="
//	'Z' len hash   -> the phone re-hashes slot B; hash of the block hashes == hash -> PENDING:
//	                  "phlink: COMMIT OK|FAILED len= hash="
//
// The ROM's word sum is the same for every image (rombuild adjusts the ROM header checksum), so it cannot tell what
// slot B holds -- the block hashes (FNV-1a over little-endian words) can, and the phone computes them itself: no
// trust in what PhoneLab last pushed.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"regexp"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const (
	deltaBlock = 64 << 10 // = the device's upload run, in sectors
	blobHashes = 3
	deltaRun   = 2 << 20 // the longest 'X' run (changed blocks next to each other are sent as one)
)

var (
	reYErr   = regexp.MustCompile(`phlink: (Y busy|rx 'Y')`)
	rePatch  = regexp.MustCompile(`phlink: PATCH (OK|FAILED) off=([0-9a-fA-F]+)`)
	reCommit = regexp.MustCompile(`phlink: COMMIT (OK|FAILED) len=([0-9a-fA-F]+) hash=([0-9a-fA-F]+)`)
)

// BlockHash mirrors the device's BlockHash: FNV-1a over the little-endian 32-bit words of b (len(b) % 4 == 0).
func BlockHash(b []byte) uint32 {
	h := uint32(2166136261)
	for i := 0; i+4 <= len(b); i += 4 {
		h = (h ^ binary.LittleEndian.Uint32(b[i:])) * 16777619
	}
	return h
}

// pad512 = the image as slot B stores it: whole sectors, the last one zero-padded.
func pad512(data []byte) []byte {
	if len(data)%512 == 0 {
		return data
	}
	p := make([]byte, (len(data)+511)&^511)
	copy(p, data)
	return p
}

// BlockHashes of a padded image: one per 64 KB block, the last block shorter.
func BlockHashes(padded []byte) []uint32 {
	var hs []uint32
	for off := 0; off < len(padded); off += deltaBlock {
		hs = append(hs, BlockHash(padded[off:min(off+deltaBlock, len(padded))]))
	}
	return hs
}

// CombinedHash = the hash of the block hashes (what 'Z' checks).
func CombinedHash(hs []uint32) uint32 {
	b := make([]byte, 4*len(hs))
	for i, h := range hs {
		binary.LittleEndian.PutUint32(b[4*i:], h)
	}
	return BlockHash(b)
}

// deltaRuns: the changed block ranges [first, last) as runs of at most deltaRun bytes.
func deltaRuns(have, want []uint32) [][2]int {
	var runs [][2]int
	maxBlocks := deltaRun / deltaBlock
	for i := 0; i < len(want); {
		if i < len(have) && have[i] == want[i] {
			i++
			continue
		}
		j := i + 1
		for j < len(want) && j-i < maxBlocks && (j >= len(have) || have[j] != want[j]) {
			j++
		}
		runs = append(runs, [2]int{i, j})
		i = j
	}
	return runs
}

// slotHashes asks the phone for the block hashes of slot B over [0, n).
func (d *Driver) slotHashes(ctx context.Context, l device.Link, n int) ([]uint32, error) {
	frames, stop := l.Frames()
	defer stop()
	m := l.Mark()
	if err := l.Write(ctx, cmd9('Y', uint32(n), 0)); err != nil {
		return nil, err
	}
	errc := make(chan string, 1)
	ec, ecancel := context.WithCancel(ctx)
	defer ecancel()
	go func() {
		if r, err := l.WaitLine(ec, m, reYErr); err == nil {
			errc <- r[1]
		}
	}()
	var buf bytes.Buffer
	idle := time.NewTimer(90 * time.Second) // the phone reads up to 112 MB of slot B before the first frame
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case e := <-errc:
			if e == "rx 'Y'" {
				return nil, errNoDelta
			}
			return nil, fmt.Errorf("phone: %s", e)
		case <-idle.C:
			return nil, fmt.Errorf("slot B hashes: no answer")
		case f := <-frames:
			idle.Reset(30 * time.Second)
			if !f.SumOK {
				return nil, fmt.Errorf("hash frame %d: checksum mismatch", f.Seq)
			}
			switch f.Type {
			case frameBlobData:
				buf.Write(f.Payload)
			case frameBlobEnd:
				if len(f.Payload) < 8 || binary.LittleEndian.Uint32(f.Payload) != blobHashes {
					continue
				}
				if total := int(binary.LittleEndian.Uint32(f.Payload[4:])); total != buf.Len() || total%4 != 0 {
					return nil, fmt.Errorf("slot B hashes: got %d of %d bytes", buf.Len(), total)
				}
				hs := make([]uint32, buf.Len()/4)
				for i := range hs {
					hs[i] = binary.LittleEndian.Uint32(buf.Bytes()[4*i:])
				}
				return hs, nil
			}
		}
	}
}

var errNoDelta = fmt.Errorf("the running image has no delta upload (no 'delta' in its caps)")

type DeltaResult struct {
	Mode       string      `json:"mode"`
	Bytes      int         `json:"bytes"`
	Sent       int         `json:"sent_bytes"`
	Blocks     int         `json:"blocks"`
	Changed    int         `json:"changed_blocks"`
	Runs       int         `json:"runs"`
	Hash       string      `json:"hash"`
	Seconds    float64     `json:"seconds"`
	Rebooted   bool        `json:"rebooted"`
	Note       string      `json:"note,omitempty"`
	Provenance *Provenance `json:"provenance,omitempty"` // the image's build record (nil = an unverified push)
}

// DeltaNote explains a delta that cannot help: when (nearly) every block differs the image layout has shifted -- a
// file early in the ROM changed size, so everything behind it moved by a few bytes and no 64 KB block still matches
// (measured 2026-10-23: 3275 of 3439 blocks differ between two builds of the same size, while ~87% of the bytes
// still exist in the old image at a constant offset). The write volume (flash wear) is then the whole image whatever
// the push mode; keeping the ROM file placement stable is the cure, not a smarter diff.
func DeltaNote(changed, blocks int) string {
	if blocks >= 32 && changed*10 >= blocks*9 {
		return fmt.Sprintf("%d of %d blocks differ: the image layout shifted (a file early in the ROM changed size); the delta saves nothing", changed, blocks)
	}
	return ""
}

func (d *Driver) pushDelta(ctx context.Context, l device.Link, data []byte, reboot bool) (any, error) {
	t0 := time.Now()
	padded := pad512(data)
	want := BlockHashes(padded)
	l.Progress("hash", 0, 1)
	have, err := d.slotHashes(ctx, l, len(data))
	if err != nil {
		return nil, err
	}
	runs := deltaRuns(have, want)
	res := DeltaResult{Mode: "delta", Bytes: len(data), Blocks: len(want), Runs: len(runs)}
	total := 0
	for _, r := range runs {
		total += min(r[1]*deltaBlock, len(padded)) - r[0]*deltaBlock
		res.Changed += r[1] - r[0]
	}
	res.Note = DeltaNote(res.Changed, res.Blocks)
	// 'G': the phone's top bar shows one DELTA progress over all runs (images before it echo "rx 'G'": harmless)
	if total > 0 {
		if err := l.Write(ctx, cmd9('G', uint32(total), 0)); err != nil {
			return res, err
		}
	}
	for _, r := range runs {
		off, end := r[0]*deltaBlock, min(r[1]*deltaBlock, len(padded))
		m := l.Mark()
		if err := l.Write(ctx, cmd9('X', uint32(off), uint32(end-off))); err != nil {
			return res, err
		}
		for p := off; p < end; p += 16384 {
			if err := l.Write(ctx, padded[p:min(p+16384, end)]); err != nil {
				return res, fmt.Errorf("delta write at %d: %w", p, err)
			}
			res.Sent += min(p+16384, end) - p
			l.Progress("send", int64(res.Sent), int64(total))
		}
		c, cancel := context.WithTimeout(ctx, 60*time.Second)
		pr, err := l.WaitLine(c, m, rePatch)
		cancel()
		if err != nil {
			return res, fmt.Errorf("no PATCH verdict for offset %#x: %w", off, err)
		}
		if pr[1] != "OK" || hexU(pr[2]) != uint64(off) {
			return res, fmt.Errorf("phone: PATCH %s off=%s (sent %#x)", pr[1], pr[2], off)
		}
	}
	hash := CombinedHash(want)
	res.Hash = fmt.Sprintf("%08x", hash)
	l.Progress("verify", 0, 1)
	m := l.Mark()
	if err := l.Write(ctx, cmd9('Z', uint32(len(data)), hash)); err != nil {
		return res, err
	}
	c, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cr, err := l.WaitLine(c, m, reCommit)
	if err != nil {
		return res, fmt.Errorf("no COMMIT verdict: %w", err)
	}
	res.Seconds = time.Since(t0).Seconds()
	if cr[1] != "OK" {
		return res, fmt.Errorf("phone: COMMIT FAILED (its hash %s, expected %s)", cr[3], res.Hash)
	}
	if reboot {
		if err := l.Write(ctx, []byte{'R'}); err != nil {
			return res, err
		}
		res.Rebooted = true
	}
	return res, nil
}
