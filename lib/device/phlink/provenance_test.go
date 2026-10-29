// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

import "testing"

func img(record string) []byte {
	return append(append([]byte("\x00shim and ROM bytes\x00"), record...), "\x00more ROM"...)
}

func TestProvenance(t *testing.T) {
	clean := "PHOREPOC-BUILD v1\nboard exampleboard\ndate 2026-10-28T23:00:00Z\nrepo core 7c9ac28\nrepo pdk daeaec7\nEND\n"
	p, err := checkProvenance(img(clean), false)
	if err != nil || p.Board != "exampleboard" || p.Repos["core"] != "7c9ac28" || len(p.Repos) != 2 {
		t.Fatalf("clean record: %+v %v", p, err)
	}
	if _, err := checkProvenance(img("no record here"), false); err == nil {
		t.Fatal("an image without a record was accepted")
	}
	dirty := "PHOREPOC-BUILD v1\nrepo core 7c9ac28+dirty\nEND\n"
	if _, err := checkProvenance(img(dirty), false); err == nil {
		t.Fatal("a dirty build was accepted")
	}
	if _, err := checkProvenance(img("no record here"), true); err != nil {
		t.Fatalf("unverified=true must pass: %v", err)
	}
	m, ok := withProvenance(PushResult{Bytes: 3}, p).(PushResult)
	if !ok || m.Provenance != p {
		t.Fatalf("result without provenance: %#v", m)
	}
}
