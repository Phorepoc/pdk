// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerArgs(t *testing.T) {
	t.Setenv("FAST", "1")
	e := &Env{PDK: "/p", Core: "/c", OSRoot: "/nonexistent-os", Symbian: "/nonexistent-sym", Build: "vol", Image: "img:1"}
	a, err := e.dockerArgs(false, []string{"python3", "x.py", "k"})
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(a, " ")
	for _, want := range []string{"-v /p:/pdk", "-v /c:/core", "-w /pdk", "-e PHOREPOC_CORE=/core", "-v vol:/build",
		"-e FAST=1", "img:1 python3 x.py k"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
	if strings.Contains(s, ":/os:ro") {
		t.Errorf("a missing overlay must not be mounted: %s", s)
	}
}

func TestIsPath(t *testing.T) {
	for in, want := range map[string]bool{"vol": false, "./b": true, "../b": true, "/b": true} {
		if isPath(in) != want {
			t.Errorf("isPath(%q) != %v", in, want)
		}
	}
}

func TestImageOptions(t *testing.T) {
	pass := map[string]bool{}
	for _, v := range passThrough {
		pass[v] = true
	}
	seen := map[string]bool{}
	for _, o := range ImageOptions {
		if !pass[o.Env] {
			t.Errorf("image option %s: %s is not handed to the container (passThrough)", o.Name, o.Env)
		}
		if seen[o.Name] {
			t.Errorf("image option %s twice", o.Name)
		}
		seen[o.Name] = true
		if o.Help == "" || o.Values == "" {
			t.Errorf("image option %s: no values or help", o.Name)
		}
	}
	if err := SetImageOption("nope=1"); err == nil {
		t.Error("an unknown image option must fail")
	}
}

// Boards come from the board.conf files of the checkouts next to core; the pdk has no list of its own.
func TestFindBoard(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "core"), 0o755)
	os.MkdirAll(filepath.Join(root, "myboard"), 0o755)
	os.WriteFile(filepath.Join(root, "myboard", "board.conf"), []byte(
		"# a board\nNAME=myboard\nSUMMARY=\"a test board\"   # comment\nARCH=arm\nREPOS=\"mysoc myboard tls\"\nRELEASE=recipes/release.sh\n"), 0o644)
	e := &Env{Core: filepath.Join(root, "core")}
	b, err := e.FindBoard("myboard")
	if err != nil || b.Arch != "arm" || b.Summary != "a test board" || strings.Join(b.Adaptations, " ") != "mysoc myboard tls" ||
		b.Release != "recipes/release.sh" {
		t.Fatalf("myboard: %+v %v", b, err)
	}
	if _, err := e.FindBoard("nope"); err == nil {
		t.Error("an unknown board must fail")
	}
}

// The recipe settings of "ph image recipe kernel -O trace=..." reach the container.
func TestKernelOptionsPassed(t *testing.T) {
	t.Setenv("TRACE", "") // restored when the test ends
	if err := SetImageOption("trace=0x80000002"); err != nil {
		t.Fatal(err)
	}
	e := &Env{PDK: "/p", Core: "/c", OSRoot: "/nonexistent-os", Symbian: "/nonexistent-sym", Build: "vol", Image: "img:1"}
	a, err := e.dockerArgs(false, []string{"bash", "recipes/kernel.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if s := strings.Join(a, " "); !strings.Contains(s, "-e TRACE=0x80000002") || !strings.HasSuffix(s, "img:1 bash recipes/kernel.sh") {
		t.Errorf("TRACE not handed over: %s", s)
	}
}
