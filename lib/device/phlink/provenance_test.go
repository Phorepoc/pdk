// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package phlink

import (
	"fmt"
	"strings"
	"testing"
)

func img(record string) []byte {
	return append(append([]byte("\x00shim and ROM bytes\x00"), record...), "\x00more ROM"...)
}

// fakeBase: the board pins core at c2; core's history is c1 -> c2 -> c3
type fakeBase struct{}

func (fakeBase) BoardLock(repo string) (map[string]string, error) {
	if repo != "adapt-exampleboard" {
		return nil, fmt.Errorf("no repository %s", repo)
	}
	return map[string]string{"core": "c2", "pdk": "p1"}, nil
}

func (fakeBase) Contains(repo, pin, head string) (bool, error) {
	order := map[string]int{"c1": 1, "c2": 2, "c3": 3, "p1": 1}
	return order[head] >= order[pin], nil
}

func TestProvenance(t *testing.T) {
	clean := "PHOREPOC-BUILD v1\nboard exampleboard\ndate 2026-10-28T23:00:00Z\nrepo core c2\nrepo pdk p1\nrepo adapt-exampleboard a1\nEND\n"
	p, err := checkProvenance(img(clean), false, fakeBase{})
	if err != nil || p.Board != "exampleboard" || p.Repos["core"] != "c2" || len(p.Repos) != 3 || p.Base != "current" {
		t.Fatalf("clean record: %+v %v", p, err)
	}
	// an experiment: uncommitted changes on a current base are accepted and recorded
	exp := "PHOREPOC-BUILD v1\nboard exampleboard\nrepo core c3+dirty\ndiff core 0123456789ab\nchanged core os/a.cpp\n" +
		"changed core os/b.cpp\nrepo pdk p1\nrepo adapt-exampleboard a1\nEND\n"
	p, err = checkProvenance(img(exp), false, fakeBase{})
	if err != nil || len(p.Dirty) != 1 || p.Changes["core"] == nil || p.Changes["core"].Diff != "0123456789ab" ||
		len(p.Changes["core"].Files) != 2 {
		t.Fatalf("experiment on a current base: %+v %v", p, err)
	}
	// a stale base (core behind its pin) is refused, clean or not
	stale := "PHOREPOC-BUILD v1\nboard exampleboard\nrepo core c1\nrepo pdk p1\nrepo adapt-exampleboard a1\nEND\n"
	if _, err := checkProvenance(img(stale), false, fakeBase{}); err == nil || !strings.Contains(err.Error(), "stale base") {
		t.Fatalf("a stale base was accepted: %v", err)
	}
	if _, err := checkProvenance(img(stale), true, fakeBase{}); err == nil {
		t.Fatal("unverified=true must not let a stale base through")
	}
	// a pinned repository missing from the record
	if _, err := checkProvenance(img("PHOREPOC-BUILD v1\nboard exampleboard\nrepo core c2\nEND\n"), false, fakeBase{}); err == nil {
		t.Fatal("a record without pdk was accepted")
	}
	// no board: the base cannot be checked
	if _, err := checkProvenance(img("PHOREPOC-BUILD v1\nrepo core c2\nEND\n"), false, fakeBase{}); err == nil {
		t.Fatal("a record without a board was accepted")
	}
	// no record: refused, unless unverified (kernel builds outside the pipeline)
	if _, err := checkProvenance(img("no record here"), false, fakeBase{}); err == nil {
		t.Fatal("an image without a record was accepted")
	}
	if _, err := checkProvenance(img("no record here"), true, fakeBase{}); err != nil {
		t.Fatalf("unverified=true must pass an image without a record: %v", err)
	}
	m, ok := withProvenance(PushResult{Bytes: 3}, p).(PushResult)
	if !ok || m.Provenance != p {
		t.Fatalf("result without provenance: %#v", m)
	}
}

func TestParseLock(t *testing.T) {
	l := parseLock("# repository commit\npdk 42ea75b\ncore 32d35d6 # comment\n\n")
	if len(l) != 2 || l["pdk"] != "42ea75b" || l["core"] != "32d35d6" {
		t.Fatalf("%v", l)
	}
}
