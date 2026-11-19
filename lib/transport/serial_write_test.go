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
	aborted := false
	n, err := writeWithDeadline(
		func(b []byte) (int, error) { <-block; return 0, errors.New("closed") },
		func() error { aborted = true; close(block); return nil },
		[]byte("abc"), 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "stalled") || n != 0 || !aborted {
		t.Fatalf("n=%d err=%v aborted=%v", n, err, aborted)
	}
}

func TestWriteWithDeadlineOK(t *testing.T) {
	n, err := writeWithDeadline(func(b []byte) (int, error) { return len(b), nil },
		func() error { t.Fatal("aborted a good write"); return nil }, []byte("abc"), time.Second)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestWriteDeadlineScales(t *testing.T) {
	if WriteDeadline(0) != 10*time.Second || WriteDeadline(64<<10) < 13*time.Second {
		t.Fatalf("%v %v", WriteDeadline(0), WriteDeadline(64<<10))
	}
}
