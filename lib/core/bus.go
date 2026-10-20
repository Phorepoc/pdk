// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"sync"
	"sync/atomic"
)

// Msg is what live subscribers (WebSocket clients, MCP waiters) receive.
//
//	type "line":     Data = store.Line
//	type "event":    Data = store.Event
//	type "state":    Data = Health
//	type "progress": Data = Progress
//	type "op":       Data = store.Op (finished call)
type Msg struct {
	Type   string `json:"type"`
	Device string `json:"device"`
	Data   any    `json:"data"`
}

// Bus fans messages out. A slow subscriber loses messages (counted) instead of stalling the phone reader.
type Bus struct {
	mu      sync.Mutex
	subs    map[*Sub]struct{}
	Dropped atomic.Int64
}

type Sub struct {
	C    chan Msg
	bus  *Bus
	once sync.Once
}

func NewBus() *Bus { return &Bus{subs: map[*Sub]struct{}{}} }

func (b *Bus) Subscribe(buf int) *Sub {
	s := &Sub{C: make(chan Msg, buf), bus: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (s *Sub) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
	})
}

func (b *Bus) Publish(m Msg) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.C <- m:
		default:
			b.Dropped.Add(1)
		}
	}
}
