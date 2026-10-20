// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package pkgindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func server(t *testing.T, payload string) (*httptest.Server, string) {
	sum := sha256.Sum256([]byte(payload))
	hash := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/hello.sis", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, payload) })
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"format": 1, "generated": "x", "items": [
		  {"id": "hello", "name": "Hello", "summary": "s", "category": "app", "license": "EPL-1.0",
		   "compatibility": {"arch": ["arm"], "boards": ["pinephone"]},
		   "resolved": [{"tag": "v1", "published": "2026-01-01T00:00:00Z", "asset": "hello.sis",
		                 "url": "%[1]s/hello.sis", "size": %[2]d, "sha256": "%[3]s"},
		                {"tag": "v2", "published": "2026-02-01T00:00:00Z", "asset": "hello.sis",
		                 "url": "%[1]s/hello.sis", "size": %[2]d, "sha256": "%[3]s"}]},
		  {"id": "bad", "name": "Bad", "summary": "s", "category": "game", "license": "MIT",
		   "compatibility": {"arch": ["x32"]},
		   "resolved": [{"tag": "v1", "published": "x", "asset": "bad.sis", "url": "%[1]s/hello.sis",
		                 "size": 1, "sha256": "00"}]}]}`, srv.URL, len(payload), hash)
	})
	return srv, hash
}

func TestLoadFitsLatest(t *testing.T) {
	srv, _ := server(t, "SIS")
	items, err := New().LoadAll(context.Background(), []string{srv.URL + "/index.json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "bad" || items[1].ID != "hello" {
		t.Fatalf("items: %+v", items)
	}
	hello, _ := Find(items, "hello")
	if !hello.Fits("arm", "pinephone") || hello.Fits("arm", "pc") || hello.Fits("x32", "") {
		t.Error("Fits")
	}
	if r := hello.Latest(); r == nil || r.Tag != "v2" {
		t.Errorf("Latest: %+v", r)
	}
	if _, err := hello.Release("v9"); err == nil {
		t.Error("an unapproved tag must fail")
	}
}

func TestFetchVerifies(t *testing.T) {
	srv, _ := server(t, "SIS")
	c := New()
	items, _ := c.LoadAll(context.Background(), []string{srv.URL + "/index.json"})
	dir := t.TempDir()
	hello, _ := Find(items, "hello")
	rel, _ := hello.Release("v1")
	p, err := c.Fetch(context.Background(), rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "SIS" {
		t.Errorf("content %q", b)
	}
	// a prefilled cache with the right content counts as downloaded (the server is not asked again)
	srv.Close()
	if _, err := c.Fetch(context.Background(), rel, dir); err != nil {
		t.Errorf("cached fetch: %v", err)
	}
	// a wrong SHA-256 is refused and nothing is kept
	srv2, _ := server(t, "SIS")
	items2, _ := c.LoadAll(context.Background(), []string{srv2.URL + "/index.json"})
	bad, _ := Find(items2, "bad")
	brel, _ := bad.Release("")
	if _, err := c.Fetch(context.Background(), brel, dir); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Errorf("want a SHA-256 error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.sis")); err == nil {
		t.Error("a file with the wrong hash was kept")
	}
}

func TestEarlierIndexWins(t *testing.T) {
	srv, _ := server(t, "SIS")
	local := filepath.Join(t.TempDir(), "mine.json")
	os.WriteFile(local, []byte(`{"format": 1, "items": [{"id": "hello", "name": "Mine", "summary": "s",
	  "category": "app", "license": "MIT", "compatibility": {"arch": ["arm"]}, "resolved": []}]}`), 0o644)
	items, err := New().LoadAll(context.Background(), []string{local, srv.URL + "/index.json"})
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := Find(items, "hello")
	if hello.Name != "Mine" || hello.Index != local {
		t.Errorf("the user's own index must win: %+v", hello)
	}
}
