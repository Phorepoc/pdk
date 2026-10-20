// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"path/filepath"
	"time"

	"context"
	"fmt"
	"github.com/Phorepoc/pdk/lib/device"
	"sort"
	"sync"

	"github.com/Phorepoc/pdk/lib/store"
)

// Manager holds every device's Session.
type Manager struct {
	Store *store.Store
	Bus   *Bus
	// Root: the workspace relative "file" op params resolve against (e.g. build/symbian.img)
	Root string
	// Version and Started describe the running service (health)
	Version string
	Started time.Time

	mu       sync.Mutex
	sessions map[string]*Session
	order    []string
}

func NewManager(st *store.Store) *Manager {
	return &Manager{Store: st, Bus: NewBus(), sessions: map[string]*Session{}}
}

func (m *Manager) Add(s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; ok {
		return fmt.Errorf("duplicate device %q", s.ID)
	}
	m.sessions[s.ID] = s
	m.order = append(m.order, s.ID)
	return nil
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) All() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := append([]string{}, m.order...)
	sort.Strings(ids)
	var out []*Session
	for _, id := range ids {
		out = append(out, m.sessions[id])
	}
	return out
}

// Run starts every session; returns when ctx ends and all have stopped.
func (m *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, s := range m.All() {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.Run(ctx)
		}(s)
	}
	wg.Wait()
}

// Exec runs an op for any front end (REST, MCP, the CLI). File params resolve against the workspace root. Device ops are
// detached from the caller's cancellation (a closed browser tab must not abort an upload half way); only wait_for
// follows the caller.
func (m *Manager) Exec(ctx context.Context, ss *Session, caller, op string, args device.Args) (any, error) {
	if args == nil {
		args = device.Args{}
	}
	for _, p := range ss.Ops() {
		if p.Name != op {
			continue
		}
		for _, prm := range p.Params {
			if prm.Type == "file" {
				if v, ok := args[prm.Name].(string); ok && v != "" && !filepath.IsAbs(v) && m.Root != "" {
					args[prm.Name] = filepath.Join(m.Root, v)
				}
			}
		}
	}
	if caller == QuietCaller {
		// a live frame is worth nothing late: give up quickly (e.g. while a push holds the link)
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		ctx = c
	} else if op != "wait_for" {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Minute)
		defer cancel()
		ctx = c
	}
	// op wait=<pattern> on an op that reboots: after it, wait for the new boot (outside the op lock, so the live view
	// and the rest keep working) and return its report with the op's result
	wait, hasWait := args["wait"].(string)
	waitS, _ := args.UintOr("wait_timeout_s", 32, 240)
	delete(args, "wait")
	delete(args, "wait_timeout_s")
	before := ss.Health().Boot
	res, err := ss.Exec(ctx, caller, op, args)
	// a screenshot can miss its slot while the link is busy: try again a couple of times before failing the caller
	for i := 0; err != nil && op == "screenshot" && caller != QuietCaller && i < 2 && ctx.Err() == nil; i++ {
		time.Sleep(400 * time.Millisecond)
		res, err = ss.Exec(ctx, caller, op, args)
	}
	if err != nil || !hasWait || wait == "" {
		return res, err
	}
	pat := wait
	if pat == "." {
		pat = ""
	}
	rep, werr := ss.Exec(ctx, caller, "wait_boot", device.Args{"after": before, "pattern": pat, "timeout_s": waitS})
	return map[string]any{"result": res, "boot": rep}, werr
}

// HealthReport: the service and every device.
type HealthReport struct {
	OK      bool     `json:"ok"`
	Version string   `json:"version"`
	Uptime  float64  `json:"uptime_s"`
	Dropped int64    `json:"bus_dropped"`
	Devices []Health `json:"devices"`
}

// Health: ok = every device connected.
func (m *Manager) Health() HealthReport {
	h := HealthReport{OK: true, Version: m.Version, Uptime: time.Since(m.Started).Seconds(), Dropped: m.Bus.Dropped.Load()}
	for _, ss := range m.All() {
		dh := ss.Health()
		if dh.State != "connected" {
			h.OK = false
		}
		h.Devices = append(h.Devices, dh)
	}
	return h
}
