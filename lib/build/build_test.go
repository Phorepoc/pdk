// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package build

import (
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

func TestFindBoard(t *testing.T) {
	if b, err := FindBoard("pc"); err != nil || b.Arch != "x32" {
		t.Errorf("pc: %v %v", b, err)
	}
	if _, err := FindBoard("nope"); err == nil {
		t.Error("an unknown board must fail")
	}
}
