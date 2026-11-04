// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes a phe command line against the service at url.
func run(t *testing.T, url string, argv ...string) error {
	t.Helper()
	for _, c := range newApp().Commands {
		if c.Path() != argv[0]+" "+argv[1] {
			continue
		}
		fs := c.FlagSet()
		if err := fs.Parse(append([]string{"-url", url, "-d", "phone"}, argv[2:]...)); err != nil {
			t.Fatal(err)
		}
		return c.Run(fs, fs.Args())
	}
	t.Fatalf("no command %v", argv)
	return nil
}

// "device push <local file>" uploads the file to the service and pushes it by blob, so it works against a service
// that cannot see the caller's disk (Docker, another machine); -s and -B name a server path / an uploaded blob.
func TestPushUploadsLocalImage(t *testing.T) {
	var uploaded string
	var pushed []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/blobs":
			b, _ := io.ReadAll(r.Body)
			uploaded = string(b)
			w.Write([]byte(`{"sha":"` + strings.Repeat("ab", 32) + `","kind":"image","name":"x.img","size":3}`))
		case strings.HasSuffix(r.URL.Path, "/ops/push"):
			var a map[string]any
			json.NewDecoder(r.Body).Decode(&a)
			pushed = append(pushed, a)
			w.Write([]byte(`{"result":{"ok":true}}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, 404)
		}
	}))
	defer ts.Close()
	img := filepath.Join(t.TempDir(), "x.img")
	os.WriteFile(img, []byte("IMG"), 0o644)

	if err := run(t, ts.URL, "device", "push", img); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uploaded, "IMG") || pushed[0]["blob"] != strings.Repeat("ab", 32) || pushed[0]["file"] != nil {
		t.Fatalf("uploaded %q, push args %v", uploaded, pushed)
	}
	if err := run(t, ts.URL, "device", "push", "-s", "build/x.img"); err != nil || pushed[1]["file"] != "build/x.img" {
		t.Fatalf("-s: %v %v", err, pushed)
	}
	if err := run(t, ts.URL, "device", "push", "-B", "cafe"); err != nil || pushed[2]["blob"] != "cafe" {
		t.Fatalf("-B: %v %v", err, pushed)
	}
	// not a local file: a path on the service's disk, as before
	if err := run(t, ts.URL, "device", "push", "nowhere/y.img"); err != nil || pushed[3]["file"] != "nowhere/y.img" {
		t.Fatalf("server path: %v %v", err, pushed)
	}
}
