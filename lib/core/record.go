// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

// The phone's screen encoder (a device.Recorder driver) serves two independent users:
//   - live_video: the live view's video transport. A viewer leases the encoder (each call renews it for liveLease);
//     PhoneLab keeps only the stream since the last IDR and hands it out as fragmented MP4 that a browser's Media
//     Source Extensions play on plain http. Nothing is stored; the encoder stops once no viewer renews the lease.
//   - record_start / record_stop: a recording, off unless asked for. record_stop stores the video (MP4) and its
//     per-frame index as blobs of the boot. A recording started while a viewer watches starts at the last IDR.
// Either one starts the encoder and a poller that drains it with short quiet rec_read ops, so key / touch /
// screenshot ops interleave. Frame times are relative to the encoder's start; pc_start_ms (the PC clock when the
// device answered rec_start) lines them up with the log's line times.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/media/h264"
	"github.com/Phorepoc/pdk/lib/media/mp4"
)

const (
	recPoll     = 50 * time.Millisecond // a live viewer waits for this too: short reads, the op lock is free between them
	recMaxBytes = 256 << 20
	liveLease   = 6 * time.Second
	liveKeep    = 8 << 20 // a live-only session drops what lies before the last IDR once it holds this much
	recHead     = 32
	liveTick    = 3000 // 1/30 s at 90 kHz: each picture's length on a live viewer's timeline
)

var recordOps = []device.OpSpec{
	{Name: "record_start", Capability: "record",
		Description: "Start recording the screen as video (hardware H.264 where the phone has it, only changed pictures; " +
			"needs the screen recorder package installed from Catalogue: the encoder is not in the image). Other ops (key, touch, input, " +
			"screenshot) run while it records. Stop with record_stop.",
		Params: []device.Param{
			{Name: "fps", Type: "int", Description: "pictures per second, 1..60 (default 30)"},
			{Name: "qp", Type: "int", Description: "H.264 quantizer 10..46 (default 28; lower = sharper, bigger)"},
		}},
	{Name: "record_stop", Capability: "record",
		Description: "Stop the recording; stores the MP4 (blob kind \"video\") and the per-frame index (kind \"video-index\": " +
			"time, IDR, mean luma, white permille, bytes) and returns their sha256s, the device's statistics and a summary " +
			"with the mostly-white pictures and how long each stayed on screen."},
	{Name: "record_status", Capability: "record", Description: "Whether a recording runs (and a live video viewer), and how much the encoder has sent."},
	{Name: "live_video", Capability: "record",
		Description: "The live view's video transport (records nothing): starts the screen encoder if needed and keeps it " +
			"running for a few seconds per call. Without from: init (an MP4 initialization segment) + a fragment from the last " +
			"IDR; with from = the previous next: a fragment of the newer pictures, paced to 30 pictures a second (repeats of the " +
			"last picture fill the gaps). Returns codec, width, height, init, frag, next, reset (a new init came), white " +
			"(permille of the last picture), recording.",
		Params: []device.Param{
			{Name: "from", Type: "int", Description: "the previous reply's next (omit to start)"},
			{Name: "viewer", Description: "an id of the viewer (its own 30-pictures-a-second timeline)"},
		}},
}

type recording struct {
	mu        sync.Mutex
	buf       []byte // the encoder's records from absolute offset base
	base      int64
	next      uint32 // the device ring's next byte
	state     int32
	reads     int
	errs      int
	lastErr   string
	start     time.Time
	boot      int64
	fps, qp   uint64
	cancel    context.CancelFunc
	done      chan struct{}
	store     bool      // a recording runs
	storeFrom int64     // its first byte (an IDR record)
	liveUntil time.Time // a live viewer's lease
	fragSeq   uint32
	viewers   map[string]*liveViewer
}

var (
	recMu   sync.Mutex
	recBySs = map[*Session]*recording{}
)

func (s *Session) recorder() (device.Recorder, bool) {
	r, ok := s.Driver.(device.Recorder)
	return r, ok
}

// recRecord is one record of the encoder's stream (lib/device/phlink/rec.go).
type recRecord struct {
	off    int // in the scanned slice
	timeUs int64
	flags  uint32
	white  uint16
	body   []byte
}

// scanRecords: the complete records of b, and the offset after the last one.
func scanRecords(b []byte) (out []recRecord, end int) {
	p := 0
	for p+recHead <= len(b) {
		if !bytes.Equal(b[p:p+4], []byte("PREC")) {
			n := bytes.Index(b[p+1:], []byte("PREC"))
			if n < 0 {
				break
			}
			p += n + 1
			continue
		}
		n := int(binary.LittleEndian.Uint32(b[p+24:]))
		if p+recHead+n > len(b) {
			break
		}
		out = append(out, recRecord{off: p, timeUs: int64(binary.LittleEndian.Uint64(b[p+8:])),
			flags: binary.LittleEndian.Uint32(b[p+16:]), white: binary.LittleEndian.Uint16(b[p+22:]), body: b[p+recHead : p+recHead+n]})
		p += recHead + n
	}
	return out, p
}

// lastIDR: the offset of the last complete IDR record in b, or 0
func lastIDR(b []byte) int {
	recs, _ := scanRecords(b)
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].flags&1 != 0 {
			return recs[i].off
		}
	}
	return 0
}

// append: one rec_read result into the session (lock held by the caller).
func (r *recording) append(res any) (n int) {
	m, ok := res.(map[string]any)
	if !ok {
		return 0
	}
	b, _ := m["bytes"].([]byte)
	seq, _ := m["seq"].(uint32)
	st, _ := m["state"].(int32)
	r.state = st
	r.reads++
	if seq != r.next && len(r.buf) > 0 {
		r.lastErr = fmt.Sprintf("the device's ring overran: bytes %d..%d lost", r.next, seq)
	}
	if len(r.buf)+len(b) <= recMaxBytes {
		r.buf = append(r.buf, b...)
	}
	r.next = seq + uint32(len(b))
	if !r.store && len(r.buf) > liveKeep { // live only: nothing before the last IDR is needed
		if cut := lastIDR(r.buf); cut > 0 {
			r.buf = append([]byte(nil), r.buf[cut:]...)
			r.base += int64(cut)
		}
	}
	return len(b)
}

// startEncoder starts the device's encoder and its poller. locked = the caller holds the op lock (an op of its own).
func (s *Session) startEncoder(ctx context.Context, fps, qp uint64, locked bool) (*recording, error) {
	args := device.Args{"fps": fps, "qp": qp}
	run := func(op string, a device.Args) error {
		if locked {
			_, err := s.Driver.Exec(ctx, &link{s: s, op: op}, op, a)
			return err
		}
		_, err := s.Exec(ctx, QuietCaller, op, a)
		return err
	}
	err := run("rec_start", args)
	if err != nil && strings.Contains(err.Error(), "(-14)") {
		// KErrInUse: an encoder no session of this service drains (left by a PhoneLab that was restarted): this
		// service owns the link, so stop that one and start again
		_ = run("rec_stop", nil)
		err = run("rec_start", args)
	}
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithCancel(context.Background())
	r := &recording{start: time.Now(), boot: s.Health().Boot, fps: fps, qp: qp, cancel: cancel, done: make(chan struct{})}
	recMu.Lock()
	recBySs[s] = r
	recMu.Unlock()
	go s.recordPoll(pctx, r)
	return r, nil
}

func (s *Session) record(ctx context.Context, op string, a device.Args) (any, error) {
	rec, ok := s.recorder()
	if !ok {
		return nil, fmt.Errorf("device %s cannot record its screen", s.ID)
	}
	recMu.Lock()
	cur := recBySs[s]
	recMu.Unlock()
	switch op {
	case "live_video":
		return s.liveVideo(ctx, cur, a)
	case "record_status":
		if cur == nil {
			return map[string]any{"recording": false, "live": false}, nil
		}
		cur.mu.Lock()
		defer cur.mu.Unlock()
		return map[string]any{"recording": cur.store, "live": time.Now().Before(cur.liveUntil), "state": cur.state,
			"bytes": cur.base + int64(len(cur.buf)), "reads": cur.reads, "errors": cur.errs, "last_error": cur.lastErr,
			"seconds": time.Since(cur.start).Seconds()}, nil
	case "record_start":
		fps, _ := a.UintOr("fps", 32, 30)
		qp, _ := a.UintOr("qp", 32, 28)
		if cur != nil {
			cur.mu.Lock()
			defer cur.mu.Unlock()
			if cur.store {
				return nil, fmt.Errorf("a recording is running (record_stop first)")
			}
			// the encoder already runs for a live viewer: the recording starts at its last IDR
			cur.store = true
			cur.storeFrom = cur.base + int64(lastIDR(cur.buf))
			return map[string]any{"recording": true, "fps": cur.fps, "qp": cur.qp, "pc_start_ms": cur.start.UnixMilli(),
				"note": "the encoder was running for the live view: the recording starts at its last IDR"}, nil
		}
		r, err := s.startEncoder(ctx, fps, qp, true)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.store = true
		r.mu.Unlock()
		return map[string]any{"recording": true, "fps": fps, "qp": qp, "pc_start_ms": r.start.UnixMilli()}, nil
	case "record_stop":
		if cur == nil {
			return nil, fmt.Errorf("no recording is running")
		}
		cur.mu.Lock()
		running, live := cur.store, time.Now().Before(cur.liveUntil)
		cur.mu.Unlock()
		if !running {
			return nil, fmt.Errorf("no recording is running")
		}
		l := &link{s: s, op: "rec_stop"}
		var stopRes any
		var stopErr error
		if !live {
			// nobody watches: stop the encoder. The poller waits for the op lock this op holds: cancel it, drain here
			cur.cancel()
			<-cur.done
			defer func() {
				recMu.Lock()
				delete(recBySs, s)
				recMu.Unlock()
			}()
			stopRes, stopErr = s.Driver.Exec(ctx, l, "rec_stop", nil)
			for i := 0; i < 2000; i++ { // what the recorder wrote since the last poll, and its end record
				res, err := s.Driver.Exec(ctx, &link{s: s, op: "rec_read"}, "rec_read", device.Args{"from": uint64(cur.next)})
				if err != nil {
					return nil, fmt.Errorf("draining the recording: %w", err)
				}
				cur.mu.Lock()
				n := cur.append(res)
				cur.mu.Unlock()
				if n == 0 {
					break
				}
			}
		}
		cur.mu.Lock()
		from := cur.storeFrom - cur.base
		if from < 0 {
			from = 0
		}
		stream := append([]byte(nil), cur.buf[from:]...)
		cur.store = false
		errs, lastErr := cur.errs, cur.lastErr
		cur.mu.Unlock()
		video, index, summary, err := rec.FinishRecording(stream)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("rec-b%d-%s", cur.boot, time.Now().UTC().Format("20060102-150405"))
		meta := map[string]any{"pc_start_ms": cur.start.UnixMilli(), "fps": cur.fps, "qp": cur.qp, "summary": summary}
		vsha, err := l.PutBlob("video", name+".mp4", meta, video)
		if err != nil {
			return nil, err
		}
		ij, _ := json.Marshal(map[string]any{"pc_start_ms": cur.start.UnixMilli(), "video": vsha, "frames": index})
		isha, err := l.PutBlob("video-index", name+".json", map[string]any{"video": vsha}, ij)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"video": vsha, "index": isha, "pc_start_ms": cur.start.UnixMilli(), "summary": summary,
			"bytes": len(stream), "poll_errors": errs}
		if lastErr != "" {
			out["warning"] = lastErr
		}
		if live {
			out["note"] = "the encoder keeps running for the live view"
		}
		if m, ok := stopRes.(map[string]any); ok {
			out["device_stats"] = m["stats"]
		}
		if stopErr != nil {
			out["stop_error"] = stopErr.Error()
		}
		return out, nil
	}
	return nil, fmt.Errorf("record: unknown op %q", op)
}

// recordingSizer: a Recorder that knows its picture size (else the recorder's 360x720)
type recordingSizer interface{ RecordingSize() (w, h int) }

var liveStartMu sync.Mutex // two viewers asking at once start one encoder

func (s *Session) liveVideo(ctx context.Context, cur *recording, a device.Args) (any, error) {
	if cur == nil {
		liveStartMu.Lock()
		recMu.Lock()
		cur = recBySs[s]
		recMu.Unlock()
		if cur == nil {
			r, err := s.startEncoder(ctx, 30, 28, false)
			if err != nil {
				liveStartMu.Unlock()
				return nil, err
			}
			cur = r
		}
		liveStartMu.Unlock()
	}
	w, h := 360, 720
	if z, ok := s.Driver.(recordingSizer); ok {
		w, h = z.RecordingSize()
	}
	cur.mu.Lock()
	defer cur.mu.Unlock()
	cur.liveUntil = time.Now().Add(liveLease)
	end := cur.base + int64(len(cur.buf))
	from := int64(-1)
	var err error
	if a.Has("from") {
		var f uint64
		f, err = a.UintOr("from", 63, 0)
		if err != nil {
			return nil, err
		}
		from = int64(f)
	}
	out := map[string]any{"recording": cur.store, "width": w, "height": h}
	id, _ := a["viewer"].(string)
	v := cur.viewers[id]
	reset := v == nil || from < cur.base || from > end // a new viewer, or one whose position was trimmed away
	if reset {
		from = cur.base + int64(lastIDR(cur.buf))
	}
	recs, used := scanRecords(cur.buf[from-cur.base:])
	var samples []mp4.Sample
	var white uint16
	for _, r := range recs {
		if r.flags&2 != 0 || len(r.body) == 0 {
			continue
		}
		if reset && len(samples) == 0 && r.flags&1 == 0 {
			continue // a stream decodes from an IDR
		}
		samples = append(samples, mp4.Sample{AnnexB: r.body, TimeUs: r.timeUs, Key: r.flags&1 != 0})
		white = r.white
	}
	now := time.Now()
	if reset {
		if len(samples) == 0 {
			out["waiting"] = true // no IDR yet: ask again without from
			return out, nil
		}
		sps, pps := mp4.ParamSets(samples[0].AnnexB)
		init, err := mp4.Init(sps, pps, w, h)
		if err != nil {
			return nil, err
		}
		v = &liveViewer{start: now}
		v.params, v.perr = h264.ParseParams(sps, pps)
		if cur.viewers == nil {
			cur.viewers = map[string]*liveViewer{}
		}
		cur.viewers[id] = v
		out["init"], out["codec"], out["reset"] = init, mp4.Codec(sps), true
	}
	v.seen = now
	for k, x := range cur.viewers { // viewers that went away
		if now.Sub(x.seen) > 2*liveLease {
			delete(cur.viewers, k)
		}
	}
	samples = v.pace(samples, now)
	if len(samples) > 0 {
		cur.fragSeq++
		out["frag"] = mp4.FragmentAt(cur.fragSeq, samples, v.t, liveTick)
		out["white"] = white
		v.t += uint64(len(samples)) * liveTick
	}
	out["next"] = from + int64(used)
	if v.perr != nil {
		out["pace_error"] = v.perr.Error()
	}
	return out, nil
}

// liveViewer: one live view's timeline. The encoder sends only changed pictures; a Media Source Extensions player
// needs media time that keeps up with the clock or it stalls and rebuffers. So each viewer gets a constant 30 pictures a
// second: the real ones (frame_num renumbered) plus P pictures of skipped macroblocks that repeat the last one.
type liveViewer struct {
	start, seen time.Time
	t           uint64 // the next picture's decode time (90 kHz)
	pics        int64  // pictures sent
	fn          uint32 // the next frame_num
	params      h264.Params
	perr        error // the stream cannot be paced (an encoder this package does not handle): real pictures only
}

func (v *liveViewer) pace(real []mp4.Sample, now time.Time) []mp4.Sample {
	if v.perr != nil {
		v.pics += int64(len(real))
		return real
	}
	maxFn := uint32(1) << v.params.Log2MaxFrameNum
	out := make([]mp4.Sample, 0, len(real)+8)
	for _, s := range real {
		if s.Key {
			v.fn = 0
		}
		var au []byte
		for _, nal := range mp4.NALs(s.AnnexB) {
			n, err := h264.SetFrameNum(nal, v.params, v.fn%maxFn)
			if err != nil {
				v.perr = err
				return append(out, real...)
			}
			au = append(append(au, 0, 0, 0, 1), n...)
		}
		s.AnnexB = au
		out = append(out, s)
		v.fn++
	}
	v.pics += int64(len(real))
	target := int64(now.Sub(v.start) * 30 / time.Second)
	if fill := target - v.pics; fill > 0 {
		if fill > 30 { // more than a second short (a stalled poll): restart the clock rather than send a burst
			v.start = now.Add(-time.Duration(v.pics) * time.Second / 30)
			fill = 1
		}
		for k := int64(0); k < fill; k++ {
			out = append(out, mp4.Sample{AnnexB: append([]byte{0, 0, 0, 1}, h264.SkipP(v.params, v.fn%maxFn)...)})
			v.fn++
		}
		v.pics += fill
	}
	return out
}

// recordPoll drains the device's ring until record_stop cancels it, the device stopped on its own, or (no recording)
// the last live viewer's lease ran out.
func (s *Session) recordPoll(ctx context.Context, r *recording) {
	defer close(r.done)
	t := time.NewTicker(recPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.mu.Lock()
		idle := !r.store && time.Now().After(r.liveUntil)
		r.mu.Unlock()
		if idle {
			recMu.Lock()
			if recBySs[s] == r {
				delete(recBySs, s)
			}
			recMu.Unlock()
			sc, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = s.Exec(sc, QuietCaller, "rec_stop", nil)
			cancel()
			return
		}
		for { // drain: a read returns at most one reply's worth
			r.mu.Lock()
			from := r.next
			r.mu.Unlock()
			res, err := s.Exec(ctx, QuietCaller, "rec_read", device.Args{"from": uint64(from)})
			if ctx.Err() != nil {
				return
			}
			r.mu.Lock()
			if err != nil {
				r.errs++
				r.lastErr = err.Error()
				r.mu.Unlock()
				break
			}
			n := r.append(res)
			r.mu.Unlock()
			if n == 0 {
				break
			}
		}
	}
}
