// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"bytes"
	"encoding/binary"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

// Generic binary framing inside the text stream (device.Frame documents the format).

var frameMagic = []byte{0xA5, 0x5A, 'P', 'F'}

const (
	frameHdr = 16
	frameMax = 64 << 10 // a bigger length is a false magic: the bytes go back to the log
	// a frame that does not complete within this is abandoned (its bytes go to the log) so a bad header can
	// never swallow the log for long
	frameStale = 2 * time.Second
)

type frameState struct {
	buf   []byte
	since time.Time // when buf started holding an incomplete frame

	mu   sync.Mutex
	subs map[chan device.Frame]struct{}

	frames, errors atomic.Int64
}

// ingest splits raw bytes into log text (-> feed) and frames (-> subscribers).
func (s *Session) ingest(b []byte, now time.Time) {
	f := &s.fr
	if len(f.buf) == 0 && bytes.IndexByte(b, frameMagic[0]) < 0 {
		s.feed(b, now) // the common case: plain log
		return
	}
	f.buf = append(f.buf, b...)
	for {
		i := bytes.Index(f.buf, frameMagic)
		if i < 0 {
			// keep a tail that could be the start of a magic split across reads
			keep := 0
			for k := min(len(frameMagic)-1, len(f.buf)); k > 0; k-- {
				if bytes.Equal(f.buf[len(f.buf)-k:], frameMagic[:k]) {
					keep = k
					break
				}
			}
			if n := len(f.buf) - keep; n > 0 {
				s.feed(f.buf[:n], now)
			}
			f.buf = append(f.buf[:0], f.buf[len(f.buf)-keep:]...)
			f.since = time.Time{}
			return
		}
		if i > 0 {
			s.feed(f.buf[:i], now)
			f.buf = append(f.buf[:0], f.buf[i:]...)
		}
		if len(f.buf) >= frameHdr {
			n := int(binary.LittleEndian.Uint32(f.buf[8:]))
			if n > frameMax {
				s.dropMagic(now, "length %d", n)
				continue
			}
			if len(f.buf) >= frameHdr+n {
				s.deliver(f.buf[:frameHdr+n])
				f.buf = append(f.buf[:0], f.buf[frameHdr+n:]...)
				f.since = time.Time{}
				continue
			}
		}
		// incomplete: wait for more, but not forever
		if f.since.IsZero() {
			f.since = now
		} else if now.Sub(f.since) > frameStale {
			s.dropMagic(now, "incomplete after %v (%d bytes)", frameStale, len(f.buf))
			continue
		}
		return
	}
}

// dropMagic gives a false/abandoned frame start back to the log.
func (s *Session) dropMagic(now time.Time, why string, args ...any) {
	f := &s.fr
	f.errors.Add(1)
	log.Printf("[%s] frame: "+why, append([]any{s.ID}, args...)...)
	s.feed(f.buf[:len(frameMagic)], now)
	f.buf = append(f.buf[:0], f.buf[len(frameMagic):]...)
	f.since = time.Time{}
}

func (s *Session) deliver(raw []byte) {
	n := len(raw) - frameHdr
	p := make([]byte, n)
	copy(p, raw[frameHdr:])
	var sum uint32
	for _, c := range p {
		sum += uint32(c)
	}
	fr := device.Frame{Type: raw[4], Seq: binary.LittleEndian.Uint16(raw[6:]), Payload: p,
		SumOK: sum == binary.LittleEndian.Uint32(raw[12:])}
	s.fr.frames.Add(1)
	if !fr.SumOK {
		s.fr.errors.Add(1)
	}
	s.fr.mu.Lock()
	for c := range s.fr.subs {
		select {
		case c <- fr:
		default:
		}
	}
	s.fr.mu.Unlock()
}

// flushStaleFrame is called on read timeouts: a frame that stopped arriving releases its bytes.
func (s *Session) flushStaleFrame(now time.Time) {
	if len(s.fr.buf) > 0 && !s.fr.since.IsZero() && now.Sub(s.fr.since) > frameStale {
		s.ingest(nil, now)
	}
}

func (s *Session) subscribeFrames() (<-chan device.Frame, func()) {
	c := make(chan device.Frame, 4096)
	s.fr.mu.Lock()
	if s.fr.subs == nil {
		s.fr.subs = map[chan device.Frame]struct{}{}
	}
	s.fr.subs[c] = struct{}{}
	s.fr.mu.Unlock()
	return c, func() {
		s.fr.mu.Lock()
		delete(s.fr.subs, c)
		s.fr.mu.Unlock()
	}
}

func (l *link) Frames() (<-chan device.Frame, func()) { return l.s.subscribeFrames() }

func (l *link) PutBlob(kind, name string, meta any, data []byte) (string, error) {
	h := l.s.Health()
	b, err := l.s.st.PutBlob(kind, name, l.s.ID, h.Boot, meta, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	l.s.event(device.Event{Kind: "blob." + kind, Data: map[string]any{"sha": b.SHA, "name": name, "size": b.Size}})
	return b.SHA, nil
}
