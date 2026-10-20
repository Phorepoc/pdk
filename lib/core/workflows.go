// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package core

// Workflows: the multi-step things a developer (or an agent) otherwise does by hand with several calls -- wait for the
// next boot and read how it went, compare two boots, type a sequence of input. Built in for every device; the
// network front ends, the CLI and the web UI all reach them as ordinary ops.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

// Symbolizer is optional for a driver: map a code address to a function and image with a symbol file.
type Symbolizer interface {
	Symbolize(symbols string, pc uint64) (fn, image string, err error)
}

var workflowOps = []device.OpSpec{
	{Name: "wait_boot", Capability: "log",
		Description: "Wait for the device's next boot (a boot id above `after`, default the current one), then optionally for a " +
			"line matching `pattern` in it. Returns the boot_report of that boot.",
		Params: []device.Param{
			{Name: "after", Type: "int", Description: "boot id to wait past (default: the current boot)"},
			{Name: "pattern", Type: "string", Description: "RE2: also wait for this line in the new boot"},
			{Name: "timeout_s", Type: "int", Description: "seconds (default 240, max 900)"},
			{Name: "symbols", Type: "file", Description: "symbol file: faults come back with function names"},
		}},
	{Name: "boot_report", Capability: "log",
		Description: "How a boot went: slot, outcome, start, line count, faults (symbolized with `symbols`), panics with a " +
			"category, and the test lines (PASS / FAIL by default). boot 0 = the current boot.",
		Params: []device.Param{
			{Name: "boot", Type: "int", Description: "boot id (0 = current)"},
			{Name: "pass", Type: "string", Description: "RE2 for a passing test line (default \\bPASS\\b)"},
			{Name: "fail", Type: "string", Description: "RE2 for a failing test line (default \\bFAIL)"},
			{Name: "symbols", Type: "file", Description: "symbol file for the fault addresses"},
		}},
	{Name: "diff_boots", Capability: "log",
		Description: "Lines that appear in one boot and not the other (numbers and hex masked, so timestamps and addresses " +
			"do not count as differences), plus the event kinds whose counts differ.",
		Params: []device.Param{
			{Name: "a", Type: "int", Required: true, Description: "the earlier boot"},
			{Name: "b", Type: "int", Description: "the later boot (0 = current)"},
			{Name: "grep", Type: "string", Description: "only lines containing this"},
			{Name: "limit", Type: "int", Description: "lines per side (default 100)"},
		}},
	{Name: "input", Capability: "input",
		Description: "Run an input script, one step per line: `key <name> [hold_ms]`, `text <text>`, `tap <x> <y>`, " +
			"`long <x> <y>`, `swipe up|down|left|right` or `swipe <x1> <y1> <x2> <y2> [ms]`, `wait <ms>`, `shot [which]`. " +
			"Stops at the first failing step. Returns each step's result; shot steps return the screenshot.",
		Params: []device.Param{{Name: "script", Type: "string", Required: true, Description: "steps, one per line (or ';' separated)"}}},
}

// waitParams are added to every op that reboots the device: push ... wait=<pattern> returns the new boot's report.
var waitParams = []device.Param{
	{Name: "wait", Type: "string", Description: "after the reboot, wait for the new boot (and a line matching this RE2; \".\" = any line) and return its report"},
	{Name: "wait_timeout_s", Type: "int", Description: "seconds to wait for it (default 240)"},
}

// BootReport: boot_report's result.
type BootReport struct {
	Boot    int64          `json:"boot"`
	Slot    string         `json:"slot,omitempty"`
	Outcome string         `json:"outcome,omitempty"`
	Started time.Time      `json:"started"`
	Lines   int64          `json:"lines"`
	Matched string         `json:"matched,omitempty"`
	Faults  []FaultInfo    `json:"faults"`
	Panics  []string       `json:"panics"`
	Tests   TestSummary    `json:"tests"`
	Events  map[string]int `json:"events"`
}

type FaultInfo struct {
	Line     string `json:"line"`
	PC       string `json:"pc,omitempty"`
	Function string `json:"function,omitempty"`
	Image    string `json:"image,omitempty"`
}

type TestSummary struct {
	Pass  int      `json:"pass"`
	Fail  int      `json:"fail"`
	Lines []string `json:"lines"` // the failing lines first, then the passing ones (capped)
}

func (s *Session) workflow(ctx context.Context, op string, a device.Args) (any, bool, error) {
	switch op {
	case "wait_boot":
		after, _ := a.UintOr("after", 63, uint64(s.Health().Boot))
		secs, _ := a.UintOr("timeout_s", 32, 240)
		pat, _ := a["pattern"].(string)
		syms, _ := a["symbols"].(string)
		r, err := s.waitBoot(ctx, int64(after), pat, time.Duration(min(secs, 900))*time.Second, syms)
		return r, true, err
	case "boot_report":
		b, _ := a.UintOr("boot", 63, 0)
		pass, _ := a["pass"].(string)
		fail, _ := a["fail"].(string)
		syms, _ := a["symbols"].(string)
		r, err := s.bootReport(int64(b), pass, fail, syms)
		return r, true, err
	case "diff_boots":
		x, err := a.Uint("a", 63)
		if err != nil {
			return nil, true, err
		}
		y, _ := a.UintOr("b", 63, 0)
		g, _ := a["grep"].(string)
		n, _ := a.UintOr("limit", 32, 100)
		r, err := s.diffBoots(int64(x), int64(y), g, int(n))
		return r, true, err
	case "input":
		sc, err := a.String("script")
		if err != nil {
			return nil, true, err
		}
		r, err := s.runInput(ctx, sc)
		return r, true, err
	}
	return nil, false, nil
}

// waitBoot: a boot id above after, then (optionally) a line matching pattern in that boot.
func (s *Session) waitBoot(ctx context.Context, after int64, pattern string, timeout time.Duration, syms string) (BootReport, error) {
	var re *regexp.Regexp
	if pattern != "" {
		var err error
		if re, err = regexp.Compile(pattern); err != nil {
			return BootReport{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	var boot, lastLine int64
	matched := ""
	for {
		if boot == 0 {
			if b := s.Health().Boot; b > after {
				boot = b
			}
		}
		if boot != 0 && re == nil {
			break
		}
		if boot != 0 {
			ls, err := s.st.Lines(store.LineQuery{Device: s.ID, Boot: boot, After: lastLine, Limit: 10000})
			if err != nil {
				return BootReport{}, err
			}
			for _, l := range ls {
				lastLine = l.ID
				if matched == "" && re.MatchString(l.Text) {
					matched = l.Text
				}
			}
			if matched != "" {
				break
			}
		}
		select {
		case <-ctx.Done():
			if boot == 0 {
				return BootReport{}, fmt.Errorf("no new boot after #%d within %s", after, timeout)
			}
			r, _ := s.bootReport(boot, "", "", syms)
			return r, fmt.Errorf("boot #%d: no line matching %q within %s", boot, pattern, timeout)
		case <-tick.C:
		}
	}
	time.Sleep(time.Second) // let the line writer store what arrived with the match
	r, err := s.bootReport(boot, "", "", syms)
	r.Matched = matched
	return r, err
}

func (s *Session) bootReport(boot int64, pass, fail, syms string) (BootReport, error) {
	if boot <= 0 {
		boot = s.Health().Boot
	}
	r := BootReport{Boot: boot, Faults: []FaultInfo{}, Panics: []string{}, Tests: TestSummary{Lines: []string{}}, Events: map[string]int{}}
	if bs, err := s.st.Boots(s.ID, boot+1, 1); err == nil && len(bs) == 1 && bs[0].ID == boot {
		r.Slot, r.Outcome, r.Lines = bs[0].Slot, bs[0].Outcome, bs[0].Lines
		r.Started = time.UnixMilli(bs[0].Started)
	} else {
		return r, fmt.Errorf("no boot #%d", boot)
	}
	evs, err := s.st.Events(store.EventQuery{Device: s.ID, Boot: boot, Limit: 10000})
	if err != nil {
		return r, err
	}
	sym, _ := s.Driver.(Symbolizer)
	for _, e := range evs {
		if strings.HasPrefix(e.Kind, "mon.") {
			continue
		}
		r.Events[e.Kind]++
		var d map[string]any
		_ = json.Unmarshal(e.Data, &d)
		switch e.Kind {
		case "fault":
			f := FaultInfo{}
			f.Line, _ = d["text"].(string)
			f.PC, _ = d["pc"].(string)
			if sym != nil && syms != "" && f.PC != "" {
				if pc, err := strconv.ParseUint(strings.TrimPrefix(f.PC, "0x"), 16, 64); err == nil {
					f.Function, f.Image, _ = sym.Symbolize(syms, pc)
				}
			}
			if len(r.Faults) < 50 {
				r.Faults = append(r.Faults, f)
			}
		case "panic":
			if c, _ := d["category"].(string); c != "" && len(r.Panics) < 50 {
				r.Panics = append(r.Panics, c)
			}
		}
	}
	custom := pass != "" || fail != ""
	if pass == "" {
		pass = `\bPASS\b`
	}
	if fail == "" {
		fail = `\bFAIL`
	}
	pre, err := regexp.Compile(pass)
	if err != nil {
		return r, err
	}
	fre, err := regexp.Compile(fail)
	if err != nil {
		return r, err
	}
	q := store.LineQuery{Device: s.ID, Boot: boot, Limit: 10000}
	if !custom {
		q.Any = []string{"PASS", "FAIL"} // the store narrows to these first; the patterns decide
	}
	ls, err := s.st.Lines(q)
	if err != nil {
		return r, err
	}
	var passed []string
	for _, l := range ls {
		switch {
		case fre.MatchString(l.Text):
			r.Tests.Fail++
			if len(r.Tests.Lines) < 100 {
				r.Tests.Lines = append(r.Tests.Lines, l.Text)
			}
		case pre.MatchString(l.Text):
			r.Tests.Pass++
			passed = append(passed, l.Text)
		}
	}
	for _, p := range passed {
		if len(r.Tests.Lines) >= 100 {
			break
		}
		r.Tests.Lines = append(r.Tests.Lines, p)
	}
	return r, nil
}

var reMaskHex = regexp.MustCompile(`0x[0-9a-fA-F]+|\b[0-9a-fA-F]{8,}\b`)
var reMaskNum = regexp.MustCompile(`[0-9]+`)

// maskLine: numbers and hex become '#', so timestamps, counters and addresses do not make lines differ.
func maskLine(t string) string {
	return reMaskNum.ReplaceAllString(reMaskHex.ReplaceAllString(t, "#"), "#")
}

// BootDiff: diff_boots' result.
type BootDiff struct {
	A      int64             `json:"a"`
	B      int64             `json:"b"`
	OnlyA  []string          `json:"only_a"`
	OnlyB  []string          `json:"only_b"`
	Events map[string][2]int `json:"events"` // kind -> {count in a, count in b}, only the kinds that differ
}

func (s *Session) diffBoots(a, b int64, grep string, limit int) (BootDiff, error) {
	if b <= 0 {
		b = s.Health().Boot
	}
	d := BootDiff{A: a, B: b, OnlyA: []string{}, OnlyB: []string{}, Events: map[string][2]int{}}
	get := func(boot int64) ([]string, map[string]bool, error) {
		ls, err := s.st.Lines(store.LineQuery{Device: s.ID, Boot: boot, Grep: grep, Limit: 10000})
		if err != nil {
			return nil, nil, err
		}
		var order []string
		set := map[string]bool{}
		for _, l := range ls {
			if k := maskLine(l.Text); !set[k] {
				set[k] = true
				order = append(order, l.Text)
			}
		}
		return order, set, nil
	}
	la, sa, err := get(a)
	if err != nil {
		return d, err
	}
	lb, sb, err := get(b)
	if err != nil {
		return d, err
	}
	for _, t := range la {
		if !sb[maskLine(t)] && len(d.OnlyA) < limit {
			d.OnlyA = append(d.OnlyA, t)
		}
	}
	for _, t := range lb {
		if !sa[maskLine(t)] && len(d.OnlyB) < limit {
			d.OnlyB = append(d.OnlyB, t)
		}
	}
	count := func(boot int64) map[string]int {
		m := map[string]int{}
		evs, _ := s.st.Events(store.EventQuery{Device: s.ID, Boot: boot, Limit: 10000})
		for _, e := range evs {
			if !strings.HasPrefix(e.Kind, "mon.") {
				m[e.Kind]++
			}
		}
		return m
	}
	ca, cb := count(a), count(b)
	kinds := map[string]bool{}
	for k := range ca {
		kinds[k] = true
	}
	for k := range cb {
		kinds[k] = true
	}
	ks := make([]string, 0, len(kinds))
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		if ca[k] != cb[k] {
			d.Events[k] = [2]int{ca[k], cb[k]}
		}
	}
	return d, nil
}

// InputStep: one step of an input script and what it returned.
type InputStep struct {
	Step   string `json:"step"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// swipes across the middle of the OS screen (360x640)
var swipeDirs = map[string][4]int{"up": {180, 500, 180, 140}, "down": {180, 140, 180, 500}, "left": {300, 320, 60, 320}, "right": {60, 320, 300, 320}}

func (s *Session) runInput(ctx context.Context, script string) ([]InputStep, error) {
	out := []InputStep{}
	num := func(f []string, i, def int) (int, error) {
		if i >= len(f) {
			if def >= 0 {
				return def, nil
			}
			return 0, fmt.Errorf("%s: missing argument %d", f[0], i)
		}
		return strconv.Atoi(f[i])
	}
	for _, raw := range strings.FieldsFunc(script, func(r rune) bool { return r == '\n' || r == ';' }) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		st := InputStep{Step: line}
		op, args := "", device.Args{}
		var err error
		switch f[0] {
		case "key":
			if len(f) < 2 {
				err = fmt.Errorf("key: which key?")
				break
			}
			var h int
			h, err = num(f, 2, 100)
			op, args = "key", device.Args{"key": f[1], "action": "press", "hold_ms": h}
		case "text":
			op, args = "text", device.Args{"text": strings.TrimSpace(strings.TrimPrefix(line, "text"))}
		case "tap", "long":
			x, e1 := num(f, 1, -1)
			y, e2 := num(f, 2, -1)
			hold := 80
			if f[0] == "long" {
				hold = 1200
			}
			op, args = "touch", device.Args{"x": x, "y": y, "action": "tap", "hold_ms": hold}
			if e1 != nil || e2 != nil {
				err = fmt.Errorf("%s: want x y", f[0])
			}
		case "swipe":
			if len(f) == 2 {
				c, ok := swipeDirs[f[1]]
				if !ok {
					err = fmt.Errorf("swipe: up, down, left, right or x1 y1 x2 y2 [ms]")
					break
				}
				op, args = "swipe", device.Args{"x1": c[0], "y1": c[1], "x2": c[2], "y2": c[3], "ms": 300}
				break
			}
			v := make([]int, 5)
			for i := 0; i < 4 && err == nil; i++ {
				v[i], err = num(f, i+1, -1)
			}
			v[4], _ = num(f, 5, 300)
			op, args = "swipe", device.Args{"x1": v[0], "y1": v[1], "x2": v[2], "y2": v[3], "ms": v[4]}
		case "wait":
			ms, e := num(f, 1, 500)
			if e != nil {
				err = e
				break
			}
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(time.Duration(ms) * time.Millisecond):
			}
			out = append(out, st)
			continue
		case "shot":
			var w int
			w, err = num(f, 1, 2)
			op, args = "screenshot", device.Args{"which": w, "store": true}
		default:
			err = fmt.Errorf("unknown step %q (key, text, tap, long, swipe, wait, shot)", f[0])
		}
		if err == nil {
			st.Result, err = s.Driver.Exec(ctx, &link{s: s, op: op}, op, args)
		}
		if err != nil {
			st.Error = err.Error()
			out = append(out, st)
			return out, fmt.Errorf("step %q: %w", line, err)
		}
		out = append(out, st)
	}
	return out, nil
}
