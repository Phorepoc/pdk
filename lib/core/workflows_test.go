// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core_test

import (
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Phorepoc/pdk/lib/core"
	"github.com/Phorepoc/pdk/lib/device"
)

func TestInputScript(t *testing.T) {
	r := newRig(t, 10)
	res, err := r.s.Exec(ctx(t, 10*time.Second), "test", "input", device.Args{
		"script": "key lsk 10\ntap 100 200; swipe up\n# a comment\nwait 20\ntext a1\nshot 0"})
	if err != nil {
		t.Fatal(err)
	}
	steps := res.([]core.InputStep)
	if len(steps) != 6 {
		t.Fatalf("%d steps: %+v", len(steps), steps)
	}
	time.Sleep(100 * time.Millisecond)
	got := strings.Join(r.ph.Input, "|")
	for _, want := range []string{"K a4 0|K a4 1", "T c80064 0", "K 41 0|K 41 1|K 31 0"} {
		if !strings.Contains(got, want) {
			t.Errorf("input %q missing %q", got, want)
		}
	}
	if _, err := r.s.Exec(ctx(t, 5*time.Second), "test", "input", device.Args{"script": "key lsk 10; jump 3"}); err == nil ||
		!strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("bad step accepted: %v", err)
	}
}

func TestPushWaitReportsTheNewBoot(t *testing.T) {
	r := newRig(t, 30)
	img := make([]byte, 200000)
	rand.Read(img)
	p := filepath.Join(t.TempDir(), "symbian.img")
	if err := writeFile(p, img); err != nil {
		t.Fatal(err)
	}
	// the rig's first boot (slot A) registers asynchronously: read "before" only once it has, or the wait takes
	// boot #1 itself for the new boot and never sees slot B
	for deadline := time.Now().Add(10 * time.Second); r.s.Health().Boot == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the rig never booted")
		}
	}
	before := r.s.Health().Boot
	res, err := r.m.Exec(ctx(t, 60*time.Second), r.s, "test", "push", device.Args{"file": p, "wait": "running slot B", "wait_timeout_s": 30})
	if err != nil {
		t.Fatal(err)
	}
	rep := res.(map[string]any)["boot"].(core.BootReport)
	if rep.Boot <= before || !strings.Contains(rep.Matched, "running slot B") {
		t.Fatalf("report %+v (before #%d)", rep, before)
	}

	// the two boots differ: the new one ran slot B
	d, err := r.s.Exec(ctx(t, 5*time.Second), "test", "diff_boots", device.Args{"a": before, "b": rep.Boot})
	if err != nil {
		t.Fatal(err)
	}
	diff := d.(core.BootDiff)
	if !anyContains(diff.OnlyB, "slot B") {
		t.Fatalf("diff %+v", diff)
	}
	br, err := r.s.Exec(ctx(t, 5*time.Second), "test", "boot_report", device.Args{"boot": 0})
	if err != nil || br.(core.BootReport).Boot != rep.Boot {
		t.Fatalf("boot_report %+v %v", br, err)
	}
}

func anyContains(ls []string, s string) bool {
	for _, l := range ls {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
