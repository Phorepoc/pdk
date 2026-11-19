// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package transport

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

// SerialConfig selects a USB serial (CDC ACM) port: by explicit name, or by USB VID/PID (portable: COMn on
// Windows, /dev/ttyACMn on Linux, /dev/cu.usbmodem* on macOS all report the same IDs).
type SerialConfig struct {
	Port string // explicit name; overrides VID/PID
	VID  string // hex, e.g. "0E22"
	PID  string // hex, e.g. "0666"
	Baud int    // nominal on CDC ACM; 115200
}

type SerialOpener struct{ Cfg SerialConfig }

func (o *SerialOpener) Describe() string {
	if o.Cfg.Port != "" {
		return "serial " + o.Cfg.Port
	}
	return fmt.Sprintf("serial vid=%s pid=%s", o.Cfg.VID, o.Cfg.PID)
}

func (o *SerialOpener) find() (string, error) {
	if o.Cfg.Port != "" {
		return o.Cfg.Port, nil
	}
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return "", err
	}
	for _, p := range ports {
		if p.IsUSB && strings.EqualFold(p.VID, o.Cfg.VID) && strings.EqualFold(p.PID, o.Cfg.PID) {
			return p.Name, nil
		}
	}
	return "", ErrNotFound
}

func (o *SerialOpener) Open(ctx context.Context) (Port, error) {
	name, err := o.find()
	if err != nil {
		return nil, err
	}
	baud := o.Cfg.Baud
	if baud == 0 {
		baud = 115200
	}
	sp, err := serial.Open(name, &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	_ = sp.SetDTR(true) // the phone starts streaming on SET_CONTROL_LINE_STATE
	_ = sp.SetReadTimeout(250 * time.Millisecond)
	return &serialPort{sp: sp, name: name, o: o}, nil
}

type serialPort struct {
	sp   serial.Port
	name string
	o    *SerialOpener
}

func (p *serialPort) Name() string { return p.name }

// Read returns (0, nil) on a read timeout; callers loop.
func (p *serialPort) Read(b []byte) (int, error) { return p.sp.Read(b) }

// Write never waits forever: a phone whose command side stopped reading (a user-side call it was making hung) lets the
// bulk-OUT endpoint fill, and a plain serial write then blocks until the phone is reset (2026-11-19: a push stuck 57 minutes,
// "write COM5: A device attached to the system is not functioning" when the phone was restarted). After the deadline the port is
// closed, which aborts the pending write and drops the link; the session reconnects and every op queued behind it runs again.
func (p *serialPort) Write(b []byte) (int, error) {
	return writeWithDeadline(p.sp.Write, p.sp.Close, b, WriteDeadline(len(b)))
}

// WriteDeadline is the longest a write of n bytes may take: 10 s plus n at 20 KB/s (a 64 KB push block is allowed ~13 s).
func WriteDeadline(n int) time.Duration {
	return 10*time.Second + time.Duration(n)*time.Second/(20<<10)
}

// writeWithDeadline runs write; if it has not returned within d it calls abort (closing the port unblocks the write) and
// reports the stall.
func writeWithDeadline(write func([]byte) (int, error), abort func() error, b []byte, d time.Duration) (int, error) {
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		n, err := write(b)
		done <- result{n, err}
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case r := <-done:
		return r.n, r.err
	case <-t.C:
		_ = abort()
		return 0, fmt.Errorf("write of %d bytes stalled for %s: the phone stopped reading; the link was closed to re-sync", len(b), d)
	}
}
func (p *serialPort) Close() error { return p.sp.Close() }

func (p *serialPort) Alive() bool {
	names, err := serial.GetPortsList()
	if err != nil {
		return true
	}
	for _, n := range names {
		if n == p.name {
			return true
		}
	}
	return false
}
