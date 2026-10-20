// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package core runs one Session per device: it alone owns the port, reconnects after every reset, splits the
// stream into lines, records everything, and runs ops one at a time.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
	"github.com/Phorepoc/pdk/lib/transport"
)

const (
	ringSize     = 20000
	partialFlush = 400 * time.Millisecond // a line without '\n' is flushed after this much silence
	aliveEvery   = time.Second
)

type Progress struct {
	Op    string `json:"op"`
	Stage string `json:"stage"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// Health is the device's live status.
type Health struct {
	Device      string         `json:"device"`
	Driver      string         `json:"driver"`
	Transport   string         `json:"transport"`
	State       string         `json:"state"` // connected | disconnected
	Port        string         `json:"port,omitempty"`
	ConnectedAt time.Time      `json:"connected_at,omitzero"`
	LastByte    time.Time      `json:"last_byte,omitzero"`
	LastByteAge float64        `json:"last_byte_age_s"`
	Reconnects  int            `json:"reconnects"`
	LastError   string         `json:"last_error,omitempty"`
	Boot        int64          `json:"boot"`
	BootStarted time.Time      `json:"boot_started,omitzero"`
	Slot        string         `json:"slot,omitempty"`
	BusyOp      string         `json:"busy_op,omitempty"`
	Monitor     map[string]any `json:"monitor,omitempty"` // latest mon.* event data by kind
	Lines       uint64         `json:"lines"`
	Frames      int64          `json:"frames"`
	FrameErrors int64          `json:"frame_errors"`
}

type ringLine struct {
	seq  uint64
	text string
}

type waiter struct {
	re *regexp.Regexp
	ch chan []string
}

type Session struct {
	ID     string
	Driver device.Driver
	opener transport.Opener
	st     *store.Store
	bus    *Bus

	mu        sync.Mutex
	port      transport.Port
	connected chan struct{} // closed while connected
	h         Health
	seq       uint64
	ring      []ringLine
	waiters   map[*waiter]struct{}
	mon       map[string]any
	partial   []byte
	lastByte  time.Time

	wmu  sync.Mutex    // serializes writes to the port
	opCh chan struct{} // one op at a time (a semaphore that respects ctx)

	lines chan store.Line

	fr frameState // only touched by the reader goroutine, except subs (fr.mu)
}

func NewSession(id string, d device.Driver, o transport.Opener, st *store.Store, bus *Bus) *Session {
	s := &Session{ID: id, Driver: d, opener: o, st: st, bus: bus,
		connected: make(chan struct{}), waiters: map[*waiter]struct{}{}, mon: map[string]any{},
		opCh: make(chan struct{}, 1), lines: make(chan store.Line, 8192)}
	s.h = Health{Device: id, Driver: d.Name(), Transport: o.Describe(), State: "disconnected"}
	// resume the current boot after a service restart: its id, slot and start time come from the store (otherwise the
	// slot and the uptime stay empty until the phone boots again)
	if bs, err := st.Boots(id, 0, 1); err == nil && len(bs) > 0 {
		s.h.Boot, s.h.Slot = bs[0].ID, bs[0].Slot
		s.h.BootStarted = time.UnixMilli(bs[0].Started)
	} else if b, err := st.LastBoot(id); err == nil {
		s.h.Boot = b
	}
	return s
}

// Run keeps the device connected until ctx ends.
func (s *Session) Run(ctx context.Context) {
	go s.lineWriter(ctx)
	backoff := 250 * time.Millisecond
	logged := false
	for ctx.Err() == nil {
		p, err := s.opener.Open(ctx)
		if err != nil {
			s.mu.Lock()
			s.h.LastError = err.Error()
			s.mu.Unlock()
			if !errors.Is(err, transport.ErrNotFound) && !logged {
				log.Printf("[%s] open: %v", s.ID, err)
				logged = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 2*time.Second)
			continue
		}
		backoff, logged = 250*time.Millisecond, false
		s.setConnected(p)
		err = s.readLoop(ctx, p)
		p.Close()
		s.setDisconnected(err)
	}
}

func (s *Session) setConnected(p transport.Port) {
	s.mu.Lock()
	s.port = p
	s.h.State, s.h.Port, s.h.ConnectedAt, s.h.LastError = "connected", p.Name(), time.Now(), ""
	close(s.connected)
	h := s.healthLocked()
	s.mu.Unlock()
	log.Printf("[%s] connected on %s", s.ID, p.Name())
	s.bus.Publish(Msg{Type: "state", Device: s.ID, Data: h})
	s.event(device.Event{Kind: "link.connected", Data: map[string]any{"port": p.Name()}})
}

func (s *Session) setDisconnected(err error) {
	s.fr.buf = nil // a frame cut by the reset is gone
	s.flushPartial()
	s.mu.Lock()
	s.port = nil
	s.connected = make(chan struct{})
	s.h.State = "disconnected"
	s.h.Reconnects++
	if err != nil {
		s.h.LastError = err.Error()
	}
	h := s.healthLocked()
	s.mu.Unlock()
	log.Printf("[%s] disconnected: %v", s.ID, err)
	s.bus.Publish(Msg{Type: "state", Device: s.ID, Data: h})
	why := ""
	if err != nil {
		why = err.Error()
	}
	s.event(device.Event{Kind: "link.disconnected", Data: map[string]any{"reason": why}})
}

var errGone = errors.New("port vanished")

func (s *Session) readLoop(ctx context.Context, p transport.Port) error {
	buf := make([]byte, 16384)
	lastAlive := time.Now()
	stop := context.AfterFunc(ctx, func() { p.Close() })
	defer stop()
	for {
		n, err := p.Read(buf)
		now := time.Now()
		if n > 0 {
			s.ingest(buf[:n], now)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return errGone
			}
			return err
		}
		if n == 0 {
			s.mu.Lock()
			stale := len(s.partial) > 0 && now.Sub(s.lastByte) > partialFlush
			s.mu.Unlock()
			if stale {
				s.flushPartial()
			}
		}
		if now.Sub(lastAlive) > aliveEvery {
			lastAlive = now
			if !p.Alive() {
				return errGone
			}
		}
	}
}

// feed splits bytes into lines.
func (s *Session) feed(b []byte, now time.Time) {
	s.mu.Lock()
	s.lastByte = now
	s.h.LastByte = now
	var out []string
	for len(b) > 0 {
		i := indexByte(b, '\n')
		if i < 0 {
			s.partial = append(s.partial, b...)
			break
		}
		s.partial = append(s.partial, b[:i]...)
		out = append(out, clean(s.partial))
		s.partial = s.partial[:0]
		b = b[i+1:]
	}
	if len(s.partial) > 8192 { // never let a binary blob grow without bound
		out = append(out, clean(s.partial))
		s.partial = s.partial[:0]
	}
	s.mu.Unlock()
	for _, l := range out {
		s.handleLine(l, now)
	}
}

func (s *Session) flushPartial() {
	s.mu.Lock()
	if len(s.partial) == 0 {
		s.mu.Unlock()
		return
	}
	l := clean(s.partial)
	s.partial = s.partial[:0]
	s.mu.Unlock()
	s.handleLine(l, time.Now())
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func clean(b []byte) string {
	t := strings.TrimRight(string(b), "\r")
	if !utf8.ValidString(t) {
		t = strings.ToValidUTF8(t, "?")
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' {
			return -1
		}
		return r
	}, t)
}

func (s *Session) handleLine(text string, now time.Time) {
	if text == "" {
		return
	}
	if s.Driver.BootStart(text) {
		id, err := s.st.NewBoot(s.ID, now)
		if err != nil {
			log.Printf("[%s] new boot: %v", s.ID, err)
		}
		s.mu.Lock()
		s.h.Boot, s.h.BootStarted, s.h.Slot = id, now, ""
		s.mon = map[string]any{}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.seq++
	seq := s.seq
	s.h.Lines = seq
	if len(s.ring) >= ringSize {
		copy(s.ring, s.ring[ringSize/4:])
		s.ring = s.ring[:len(s.ring)-ringSize/4]
	}
	s.ring = append(s.ring, ringLine{seq, text})
	boot := s.h.Boot
	for w := range s.waiters {
		if m := w.re.FindStringSubmatch(text); m != nil {
			select {
			case w.ch <- append([]string{text}, m...):
			default:
			}
			delete(s.waiters, w)
		}
	}
	s.mu.Unlock()

	ln := store.Line{Device: s.ID, Boot: boot, TS: now.UnixMilli(), Text: text}
	select {
	case s.lines <- ln:
	default:
		log.Printf("[%s] line writer behind; dropped a line from the store", s.ID)
	}
	s.bus.Publish(Msg{Type: "line", Device: s.ID, Data: ln})
	for _, ev := range s.Driver.Classify(text) {
		s.event(ev)
	}
}

// lineWriter batches line inserts.
func (s *Session) lineWriter(ctx context.Context) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	var batch []store.Line
	flush := func() {
		if err := s.st.AddLines(batch); err != nil {
			log.Printf("[%s] store lines: %v", s.ID, err)
		}
		batch = batch[:0]
	}
	for {
		select {
		case l := <-s.lines:
			batch = append(batch, l)
			if len(batch) >= 2000 {
				flush()
			}
		case <-t.C:
			flush()
		case <-ctx.Done():
			for {
				select {
				case l := <-s.lines:
					batch = append(batch, l)
				default:
					flush()
					return
				}
			}
		}
	}
}

// event stores + publishes ev and folds it into the health state.
func (s *Session) event(ev device.Event) {
	now := time.Now()
	s.mu.Lock()
	boot := s.h.Boot
	switch {
	case ev.Kind == "boot.slot":
		if v, ok := ev.Data["slot"].(string); ok {
			s.h.Slot = v
			go s.st.SetBoot(boot, "slot", v)
		}
	case ev.Kind == "boot.healthy":
		go s.st.SetBoot(boot, "outcome", "healthy")
	case ev.Kind == "fault" || ev.Kind == "panic":
		go s.st.SetBoot(boot, "outcome", "fault")
	case strings.HasPrefix(ev.Kind, "mon."):
		s.mon[ev.Kind] = ev.Data
	}
	s.mu.Unlock()
	id, err := s.st.AddEvent(s.ID, boot, now, ev.Kind, ev.Data)
	if err != nil {
		log.Printf("[%s] store event: %v", s.ID, err)
	}
	data, _ := json.Marshal(ev.Data)
	if string(data) == "null" {
		data = []byte("{}")
	}
	s.bus.Publish(Msg{Type: "event", Device: s.ID, Data: store.Event{ID: id, Device: s.ID, Boot: boot, TS: now.UnixMilli(), Kind: ev.Kind, Data: data}})
}

func (s *Session) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.healthLocked()
}

func (s *Session) healthLocked() Health {
	h := s.h
	if !h.LastByte.IsZero() {
		h.LastByteAge = time.Since(h.LastByte).Seconds()
	}
	h.Frames, h.FrameErrors = s.fr.frames.Load(), s.fr.errors.Load()
	h.Monitor = map[string]any{}
	for k, v := range s.mon {
		h.Monitor[k] = v
	}
	return h
}

// ---- the device.Link given to drivers

type link struct {
	s  *Session
	op string
}

func (l *link) Write(ctx context.Context, b []byte) error { return l.s.write(ctx, b) }
func (l *link) Mark() device.Mark                         { return l.s.Mark() }
func (l *link) WaitLine(ctx context.Context, m device.Mark, re *regexp.Regexp) ([]string, error) {
	return l.s.WaitLine(ctx, m, re)
}
func (l *link) LinesSince(m device.Mark, max int) []string { return l.s.LinesSince(m, max) }
func (l *link) Blob(sha string) (io.ReadCloser, int64, error) {
	return l.s.st.OpenBlob(sha)
}
func (l *link) Progress(stage string, done, total int64) {
	l.s.bus.Publish(Msg{Type: "progress", Device: l.s.ID, Data: Progress{Op: l.op, Stage: stage, Done: done, Total: total}})
}
func (l *link) Emit(ev device.Event) { l.s.event(ev) }

func (s *Session) Mark() device.Mark {
	s.mu.Lock()
	defer s.mu.Unlock()
	return device.Mark(s.seq)
}

func (s *Session) LinesSince(m device.Mark, max int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.ring {
		if r.seq > uint64(m) {
			out = append(out, r.text)
			if len(out) >= max {
				break
			}
		}
	}
	return out
}

// WaitLine returns the first line after m matching re (already received, or arriving before ctx ends).
func (s *Session) WaitLine(ctx context.Context, m device.Mark, re *regexp.Regexp) ([]string, error) {
	r, err := s.waitLine(ctx, m, re)
	if err != nil {
		return nil, err
	}
	return r[1:], nil
}

// waitLine returns the whole line followed by the submatches.
func (s *Session) waitLine(ctx context.Context, m device.Mark, re *regexp.Regexp) ([]string, error) {
	s.mu.Lock()
	for _, r := range s.ring {
		if r.seq > uint64(m) {
			if sm := re.FindStringSubmatch(r.text); sm != nil {
				s.mu.Unlock()
				return append([]string{r.text}, sm...), nil
			}
		}
	}
	w := &waiter{re: re, ch: make(chan []string, 1)}
	s.waiters[w] = struct{}{}
	s.mu.Unlock()
	select {
	case r := <-w.ch:
		return r, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.waiters, w)
		s.mu.Unlock()
		return nil, fmt.Errorf("waiting for /%s/: %w", re, ctx.Err())
	}
}

// write sends b, waiting (within ctx) for the device to be connected.
func (s *Session) write(ctx context.Context, b []byte) error {
	for {
		s.mu.Lock()
		p, ch := s.port, s.connected
		s.mu.Unlock()
		if p != nil {
			s.wmu.Lock()
			_, err := p.Write(b)
			s.wmu.Unlock()
			if err != nil {
				return fmt.Errorf("write %s: %w", p.Name(), err)
			}
			return nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return fmt.Errorf("device not connected: %w", ctx.Err())
		}
	}
}

// WaitConnected blocks until the device is connected or ctx ends.
func (s *Session) WaitConnected(ctx context.Context) error {
	s.mu.Lock()
	ch := s.connected
	s.mu.Unlock()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
