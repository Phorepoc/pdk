// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package transport

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWriteWithDeadlineStall(t *testing.T) {
	block := make(chan struct{})
	var order []string
	n, err := writeWithDeadline(
		func(b []byte) (int, error) {
			<-block
			order = append(order, "write-returned")
			return 0, errors.New("aborted")
		},
		func() error { order = append(order, "cancel"); close(block); return nil },
		func() error { order = append(order, "close"); return nil },
		[]byte("abc"), 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "stalled") || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// the handle is closed only after the stuck write has returned (else Windows keeps the file object: "Serial port busy")
	if got := strings.Join(order, ","); got != "cancel,write-returned,close" {
		t.Fatalf("order %s", got)
	}
}

// A write that ignores the cancel must not hang the session: the port is closed after the grace.
func TestWriteWithDeadlineStuckAfterCancel(t *testing.T) {
	old := writeGrace
	writeGrace = 50 * time.Millisecond
	defer func() { writeGrace = old }()
	block := make(chan struct{})
	defer close(block)
	closed := false
	start := time.Now()
	_, err := writeWithDeadline(func(b []byte) (int, error) { <-block; return 0, nil },
		func() error { return nil }, func() error { closed = true; return nil }, []byte("a"), 30*time.Millisecond)
	if err == nil || !closed || !strings.Contains(err.Error(), "did not return") || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v closed=%v after %v", err, closed, time.Since(start))
	}
}

func TestWriteWithDeadlineOK(t *testing.T) {
	n, err := writeWithDeadline(func(b []byte) (int, error) { return len(b), nil },
		func() error { t.Fatal("cancelled a good write"); return nil },
		func() error { t.Fatal("closed after a good write"); return nil }, []byte("abc"), time.Second)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestWriteDeadlineScales(t *testing.T) {
	if WriteDeadline(0) != 10*time.Second || WriteDeadline(64<<10) < 13*time.Second {
		t.Fatalf("%v %v", WriteDeadline(0), WriteDeadline(64<<10))
	}
}
