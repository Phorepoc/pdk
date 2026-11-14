// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// A read still holding its rows must not block the writer: with one shared connection a slow search stalled line
// ingestion (2026-11-05).
func TestWriteDuringOpenRead(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	boot, err := s.NewBoot("phone", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddLines([]Line{{Device: "phone", Boot: boot, TS: 1, Text: "a"}, {Device: "phone", Boot: boot, TS: 2, Text: "b"}}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.rd.Query(`SELECT id FROM lines`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no rows")
	}
	done := make(chan error, 1)
	go func() { done <- s.AddLines([]Line{{Device: "phone", Boot: boot, TS: 3, Text: "c"}}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the write waited for the open read")
	}
	got, err := s.Lines(LineQuery{Device: "phone", Boot: boot, Grep: "c"})
	if err != nil || len(got) != 1 {
		t.Fatalf("Lines after the write: %v %v", got, err)
	}
}
