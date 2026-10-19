package core_test

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }

func writeTemp(t *testing.T, b []byte) string {
	p := filepath.Join(t.TempDir(), "img.bin")
	if err := writeFile(p, b); err != nil {
		t.Fatal(err)
	}
	return p
}
