// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func testApp(got *[]string) *App {
	run := func(fs *flag.FlagSet, args []string) error {
		*got = append([]string{fs.Lookup("v").Value.String()}, args...)
		return nil
	}
	return &App{
		Summary: "Test.",
		Groups:  []Group{{Name: "image", Title: "Images"}},
		Commands: []*Command{{
			Group: "image", Name: "rom", Operands: "[<x>...]", Summary: "Build it", Description: "Builds it.",
			Flags: func(fs *flag.FlagSet) {
				fs.Bool("v", false, "verbose")
				fs.Bool("H", false, "script mode")
				fs.String("b", "", "the `board`")
			},
			Examples: []Example{{What: "Build:", Cmd: "image rom"}},
			Aliases:  []string{"rom", "romv=-v"},
			Run:      run,
		}},
		Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
	}
}

func TestSynopsis(t *testing.T) {
	var got []string
	a := testApp(&got)
	if s := Synopsis(a.Commands[0]); s != "ph image rom [-Hv] [-b <board>] [<x>...]" {
		t.Errorf("synopsis %q", s)
	}
}

func TestOptionsAfterOperands(t *testing.T) {
	var got []string
	a := testApp(&got)
	if st := a.Run([]string{"image", "rom", "one", "-v", "two", "--", "-three"}); st != 0 {
		t.Fatalf("status %d: %s", st, a.Err)
	}
	if strings.Join(got, " ") != "true one two -three" {
		t.Errorf("got %q", got)
	}
}

func TestAliasWithArguments(t *testing.T) {
	var got []string
	a := testApp(&got)
	if st := a.Run([]string{"romv", "x"}); st != 0 {
		t.Fatalf("status %d", st)
	}
	if strings.Join(got, " ") != "true x" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(a.Err.(*bytes.Buffer).String(), `"ph romv" is now "ph image rom -v"`) {
		t.Errorf("no move notice: %s", a.Err)
	}
}

func TestUnknown(t *testing.T) {
	var got []string
	a := testApp(&got)
	if a.Run([]string{"nope"}) != 2 || a.Run([]string{"image"}) != 2 || a.Run([]string{"image", "rom", "-q"}) != 2 {
		t.Error("an unknown command, a bare group or an unknown option must exit 2")
	}
}

func TestWrap(t *testing.T) {
	out := wrap(strings.Repeat("word ", 40), "    ", 30)
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 30 || !strings.HasPrefix(l, "    word") {
			t.Errorf("line %q", l)
		}
	}
}

func TestHelpLayout(t *testing.T) {
	var got []string
	a := testApp(&got)
	var b bytes.Buffer
	a.Help(&b, a.Commands[0])
	want := "NAME\n    ph image rom - Build it\n\nSYNOPSIS\n    ph image rom [-Hv] [-b <board>] [<x>...]\n\n" +
		"DESCRIPTION\n    Builds it.\n\nOPTIONS\n    -H\n        Script mode.\n\n    -b <board>\n        The board.\n\n" +
		"    -v\n        Verbose.\n\nEXAMPLES\n    Build:\n\n        ph image rom\n\n"
	if !strings.HasPrefix(b.String(), want) {
		t.Errorf("help:\n%s\nwant prefix:\n%s", b.String(), want)
	}
}
