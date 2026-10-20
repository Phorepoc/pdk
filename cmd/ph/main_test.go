// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Phorepoc/pdk/lib/cli"
)

// Every command is documented: a summary (no final full stop: it is a title), a description unless it is trivial, at
// least one example that names the command itself, and a help text for each option.
func TestCommandsDocumented(t *testing.T) {
	app := newApp()
	seen := map[string]bool{}
	for _, c := range app.Commands {
		p := c.Path()
		if seen[p] {
			t.Errorf("%s: defined twice", p)
		}
		seen[p] = true
		if c.Summary == "" || strings.HasSuffix(c.Summary, ".") {
			t.Errorf("%s: summary %q (wanted: a title, no full stop)", p, c.Summary)
		}
		if c.Description == "" && c.Name != "version" {
			t.Errorf("%s: no description", p)
		}
		if len(c.Examples) == 0 {
			t.Errorf("%s: no example", p)
		}
		for _, e := range c.Examples {
			if !strings.HasPrefix(e.Cmd, p) {
				t.Errorf("%s: example %q runs another command", p, e.Cmd)
			}
		}
		c.FlagSet().VisitAll(func(f *flag.Flag) {
			if strings.TrimSpace(f.Usage) == "" {
				t.Errorf("%s: option -%s has no help", p, f.Name)
			}
		})
	}
}

// Aliases do not collide with each other or with a command.
func TestAliasesUnique(t *testing.T) {
	app := newApp()
	names := map[string]string{}
	for _, c := range app.Commands {
		if c.Group == "" {
			names[c.Name] = c.Path()
		}
	}
	for _, c := range app.Commands {
		for _, al := range c.Aliases {
			n, _, _ := strings.Cut(al, "=")
			if other, ok := names[n]; ok {
				t.Errorf("alias %q of %s is already %s", n, c.Path(), other)
			}
			names[n] = c.Path()
		}
	}
}

// docs/ph is what the definitions generate (regenerate: go run ./cmd/ph help -markdown docs/ph).
func TestDocsUpToDate(t *testing.T) {
	dir := t.TempDir()
	if err := newApp().WriteMarkdown(dir); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	for _, f := range files {
		want, _ := os.ReadFile(f)
		got, err := os.ReadFile(filepath.Join("..", "..", "docs", "ph", filepath.Base(f)))
		if err != nil || strings.ReplaceAll(string(got), "\r\n", "\n") != string(want) {
			t.Errorf("docs/ph/%s is stale: go run ./cmd/ph help -markdown docs/ph", filepath.Base(f))
		}
	}
	old, _ := filepath.Glob(filepath.Join("..", "..", "docs", "ph", "*.md"))
	if len(old) != len(files) {
		t.Errorf("docs/ph has %d pages, the commands make %d: regenerate", len(old), len(files))
	}
}

var _ = cli.Width
