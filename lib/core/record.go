// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

// Screen recording (a device.Recorder driver): record_start starts the device's recorder and a poller that drains it
// with short quiet rec_read ops, so key / touch / screenshot ops interleave with a recording in progress;
// record_stop stops it, drains the rest, and stores the video (MP4) and its per-frame index as blobs of the boot.
// Each frame's time is relative to the start; pc_start_ms (the PC clock when the device answered rec_start) lines it
// up with the log's line times.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const (
	recPoll     = 200 * time.Millisecond
	recMaxBytes = 256 << 20
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
	{Name: "record_status", Capability: "record", Description: "Whether a recording runs, and how much it has collected."},
}

type recording struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	next    uint32
	state   int32
	reads   int
	errs    int
	lastErr string
	start   time.Time
	boot    int64
	fps, qp uint64
	cancel  context.CancelFunc
	done    chan struct{}
}

var (
	recMu   sync.Mutex
	recBySs = map[*Session]*recording{}
)

func (s *Session) recorder() (device.Recorder, bool) {
	r, ok := s.Driver.(device.Recorder)
	return r, ok
}

// recordAppend: one rec_read result into the recording (lock held by the caller).
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
	if seq != r.next && r.buf.Len() > 0 {
		r.lastErr = fmt.Sprintf("the device's ring overran: bytes %d..%d lost", r.next, seq)
	}
	if r.buf.Len()+len(b) <= recMaxBytes {
		r.buf.Write(b)
	}
	r.next = seq + uint32(len(b))
	return len(b)
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
	case "record_status":
		if cur == nil {
			return map[string]any{"recording": false}, nil
		}
		cur.mu.Lock()
		defer cur.mu.Unlock()
		return map[string]any{"recording": cur.cancel != nil, "state": cur.state, "bytes": cur.buf.Len(), "reads": cur.reads,
			"errors": cur.errs, "last_error": cur.lastErr, "seconds": time.Since(cur.start).Seconds()}, nil
	case "record_start":
		if cur != nil {
			return nil, fmt.Errorf("a recording is running (record_stop first)")
		}
		fps, _ := a.UintOr("fps", 32, 30)
		qp, _ := a.UintOr("qp", 32, 28)
		if _, err := s.Driver.Exec(ctx, &link{s: s, op: "rec_start"}, "rec_start", device.Args{"fps": fps, "qp": qp}); err != nil {
			return nil, err
		}
		pctx, cancel := context.WithCancel(context.Background())
		r := &recording{start: time.Now(), boot: s.Health().Boot, fps: fps, qp: qp, cancel: cancel, done: make(chan struct{})}
		recMu.Lock()
		recBySs[s] = r
		recMu.Unlock()
		go s.recordPoll(pctx, r)
		return map[string]any{"recording": true, "fps": fps, "qp": qp, "pc_start_ms": r.start.UnixMilli()}, nil
	case "record_stop":
		if cur == nil {
			return nil, fmt.Errorf("no recording is running")
		}
		// the poller waits for the op lock this op holds: cancel it, then drain here
		cur.cancel()
		<-cur.done
		defer func() {
			recMu.Lock()
			delete(recBySs, s)
			recMu.Unlock()
		}()
		l := &link{s: s, op: "rec_stop"}
		stopRes, stopErr := s.Driver.Exec(ctx, l, "rec_stop", nil)
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
		video, index, summary, err := rec.FinishRecording(cur.buf.Bytes())
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("rec-b%d-%s", cur.boot, cur.start.UTC().Format("20060102-150405"))
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
			"bytes": cur.buf.Len(), "poll_errors": cur.errs}
		if cur.lastErr != "" {
			out["warning"] = cur.lastErr
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

// recordPoll drains the device's ring until record_stop cancels it (or the device stopped on its own).
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
