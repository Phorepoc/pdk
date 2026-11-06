// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

// Screen recording: the phone's recorder server pherec.exe (the SoC repository's src/pherec, reached through phfs.exe) encodes
// the screen as the user sees it (360x720) into a byte ring with the H.264 encoder plug-in from Catalogue (pherecenc.dll:
// not in the image, H.264 is patent-encumbered); rec_read polls the ring like a tty. The byte stream is a run of
// records: {"PREC", frame u32, time us u64 (since the start), flags u32 (1 = IDR, 2 = end), mean luma u16, white
// permille u16, length u32, reserved u32} + an Annex B access unit. FinishRecording (device.Recorder) turns the
// collected bytes into an MP4 and a per-frame index.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/media/mp4"
)

const (
	recOpStart = 11
	recOpRead  = 12
	recOpStop  = 13

	recHead    = 32
	recFlagIDR = 1
	recFlagEnd = 2
	recW, recH = 360, 720
)

func recOps() []device.OpSpec {
	return []device.OpSpec{
		{Name: "rec_start", Capability: "record",
			Description: "Low level (use record_start): start phfs.exe's screen recorder (H.264).",
			Params:      []device.Param{{Name: "fps", Type: "int", Description: "1..60 (default 30)"}, {Name: "qp", Type: "int", Description: "10..46 (default 28)"}}},
		{Name: "rec_read", Capability: "record",
			Description: "Low level (use record_stop): the recorder's bytes from sequence number from. Returns {state, seq, next, bytes}.",
			Params:      []device.Param{{Name: "from", Type: "int"}}},
		{Name: "rec_stop", Capability: "record",
			Description: "Low level (use record_stop): stop the recorder; returns its statistics line."},
	}
}

func (d *Driver) execRec(ctx context.Context, l device.Link, op string, a device.Args) (any, bool, error) {
	num := func(k string, def uint64) uint32 { v, _ := a.UintOr(k, 32, def); return uint32(v) }
	var code uint32
	var payload []byte
	switch op {
	case "rec_start":
		code, payload = recOpStart, append(append(le32(num("fps", 30)), le32(num("qp", 28))...), le32(0)...)
	case "rec_read":
		code, payload = recOpRead, le32(num("from", 0))
	case "rec_stop":
		code = recOpStop
	default:
		return nil, false, nil
	}
	data, err := d.fsRequest(ctx, l, code, payload)
	if err != nil {
		if strings.HasPrefix(err.Error(), "phone: not supported") {
			return nil, true, fmt.Errorf("the phone's image predates the screen recorder (phfs.exe without it): push a newer image")
		}
		if op == "rec_start" && strings.HasPrefix(err.Error(), "phone: not found") {
			// H.264 is patent-encumbered: the encoder is not in the image but a Catalogue package (pherecenc.dll)
			return nil, true, fmt.Errorf("the phone has no H.264 encoder: install the screen recorder package from Catalogue (the encoder plug-in pherecenc.dll)")
		}
		return nil, true, err
	}
	switch op {
	case "rec_read":
		if len(data) < 8 {
			return nil, true, fmt.Errorf("rec_read: short reply")
		}
		seq := binary.LittleEndian.Uint32(data[4:])
		b := data[8:]
		return map[string]any{"state": int32(binary.LittleEndian.Uint32(data)), "seq": seq, "next": seq + uint32(len(b)), "bytes": b}, true, nil
	case "rec_stop":
		return map[string]any{"stats": string(data)}, true, nil
	}
	return map[string]any{"ok": true}, true, nil
}

// RecFrame is one picture of a recording (the index next to the MP4).
type RecFrame struct {
	Frame  uint32  `json:"frame"`
	TimeMs float64 `json:"t_ms"`
	IDR    bool    `json:"idr,omitempty"`
	Luma   int     `json:"luma"`  // mean, 16..235
	White  int     `json:"white"` // permille of near-white pixels (luma >= 230)
	Bytes  int     `json:"bytes"`
}

// FinishRecording (device.Recorder): the collected record stream -> an MP4 + the per-frame index and a summary.
func (*Driver) FinishRecording(stream []byte) (video []byte, index any, summary map[string]any, err error) {
	var samples []mp4.Sample
	var frames []RecFrame
	endUs := int64(-1)
	skipped := 0
	for p := 0; p+recHead <= len(stream); {
		if !bytes.Equal(stream[p:p+4], []byte("PREC")) {
			// a reader that fell behind lands on a record boundary, so this is damage: find the next record
			n := bytes.Index(stream[p+1:], []byte("PREC"))
			if n < 0 {
				break
			}
			skipped += n + 1
			p += n + 1
			continue
		}
		h := stream[p : p+recHead]
		frame := binary.LittleEndian.Uint32(h[4:])
		us := int64(binary.LittleEndian.Uint64(h[8:]))
		flags := binary.LittleEndian.Uint32(h[16:])
		n := int(binary.LittleEndian.Uint32(h[24:]))
		if p+recHead+n > len(stream) {
			break // the last record is incomplete
		}
		body := stream[p+recHead : p+recHead+n]
		p += recHead + n
		if flags&recFlagEnd != 0 {
			endUs = us
			continue
		}
		if len(samples) == 0 && flags&recFlagIDR == 0 {
			continue // a stream decodes from an IDR
		}
		samples = append(samples, mp4.Sample{AnnexB: body, TimeUs: us, Key: flags&recFlagIDR != 0})
		frames = append(frames, RecFrame{Frame: frame, TimeMs: float64(us) / 1000, IDR: flags&recFlagIDR != 0,
			Luma: int(binary.LittleEndian.Uint16(h[20:])), White: int(binary.LittleEndian.Uint16(h[22:])), Bytes: n})
	}
	if len(samples) == 0 {
		return nil, nil, nil, fmt.Errorf("the recording holds no picture (%d bytes)", len(stream))
	}
	if endUs < samples[len(samples)-1].TimeUs {
		endUs = samples[len(samples)-1].TimeUs + 33333
	}
	video, err = mp4.Write(samples, recW, recH, endUs)
	if err != nil {
		return nil, nil, nil, err
	}
	// the pictures that are mostly white: what a "white frame" bug looks like, with how long each one stayed
	var flashes []map[string]any
	for i, f := range frames {
		if f.White < 600 {
			continue
		}
		next := float64(endUs) / 1000
		if i+1 < len(frames) {
			next = frames[i+1].TimeMs
		}
		flashes = append(flashes, map[string]any{"frame": f.Frame, "t_ms": f.TimeMs, "shown_ms": next - f.TimeMs, "white": f.White})
	}
	summary = map[string]any{"frames": len(frames), "duration_ms": float64(endUs) / 1000, "width": recW, "height": recH,
		"mp4_bytes": len(video), "skipped_bytes": skipped, "white_pictures": flashes}
	return video, frames, summary, nil
}

// RecordingSize is the recorder's picture size (the live video view's track).
func (*Driver) RecordingSize() (w, h int) { return recW, recH }
