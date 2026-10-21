// Copyright (c) Nokia Corporation and/or its subsidiary(-ies) (ThemeInstaller); Copyright The Phorepoc Authors (the Go port)
// SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

package odt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestIn finds the .dat naming <FileXML> in a testdata configuration directory.
func manifestIn(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(strings.ToLower(e.Name()), ".dat") {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(b), "<FileXML>") {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	t.Fatalf("%s: no manifest", dir)
	return ""
}

// Each configuration is compiled and compared byte for byte with the file ThemeInstaller produced from the same
// sources.
func TestCompileMatchesThemeInstaller(t *testing.T) {
	refs, err := filepath.Glob("testdata/expected/*.o0000")
	if err != nil || len(refs) == 0 {
		t.Fatalf("no reference files: %v", err)
	}
	for _, ref := range refs {
		name := strings.TrimSuffix(filepath.Base(ref), ".o0000")
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(ref)
			if err != nil {
				t.Fatal(err)
			}
			m, err := ReadManifest(manifestIn(t, filepath.Join("testdata", name)))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Compile(m, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				n := 0
				for n < len(got) && n < len(want) && got[n] == want[n] {
					n++
				}
				t.Fatalf("output differs: %d bytes, want %d; first difference at offset %d (%#x)", len(got), len(want), n, n)
			}
		})
	}
}

func TestJavaFloatString(t *testing.T) {
	for _, c := range []struct {
		f    float32
		want string
	}{{3.5, "3.5"}, {10, "10.0"}, {0.2, "0.2"}, {0, "0.0"}, {1e7, "1.0E7"}, {0.0001, "1.0E-4"}, {123456.7, "123456.7"}} {
		if got := javaFloatString(c.f); got != c.want {
			t.Errorf("javaFloatString(%v) = %q, want %q", c.f, got, c.want)
		}
	}
}

func TestHashMapOrder(t *testing.T) {
	// "a".."e" hash to their char codes, so they iterate in code order whatever the insertion order
	got := javaHashMapOrder([]string{"e", "a", "d", "b", "c"})
	if strings.Join(got, "") != "abcde" {
		t.Errorf("order = %v", got)
	}
}
