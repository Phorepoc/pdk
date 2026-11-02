// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

import (
	"os"
	"testing"
)

// The GitHub side of the base check against the live public repositories (PHLINK_NET=1; needs the network).
func TestGithubBaseLive(t *testing.T) {
	if os.Getenv("PHLINK_NET") != "1" {
		t.Skip("PHLINK_NET=1 runs it against github.com")
	}
	g := newGithubBase()
	lock, err := g.BoardLock("pinephone")
	if err != nil || lock["core"] == "" || lock["pdk"] == "" {
		t.Fatalf("board.lock: %v %v", lock, err)
	}
	// pin contains itself
	if in, err := g.Contains("core", lock["core"], lock["core"]); err != nil || !in {
		t.Fatalf("pin vs itself: %v %v", in, err)
	}
	// core's first release-era commit 593889a is an ancestor of every later pin: it does not contain a newer one
	if lock["core"][:7] != "593889a" {
		if in, err := g.Contains("core", lock["core"], "593889a9cede33c17be12367731dd6f3fc774316"); err != nil || in {
			t.Fatalf("an older commit reported as containing the pin: %v %v", in, err)
		}
	}
}
