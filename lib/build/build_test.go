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

func TestIsCommand(t *testing.T) {
	if !IsCommand("rom") || IsCommand("health") {
		t.Error("IsCommand")
	}
}
