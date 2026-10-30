// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

// The threads op: phlink 'D' prints the kernel's thread dump into the log (the device side of phlink): per thread
// "[dump] <process>::<thread> ms=<iMState>" then, as continuation fragments (ekseam_puthex ends each value with a
// newline), " waits on <object>" and " pc=<hex>" " lr=<hex>" " sp=<hex>" + " s=<hex>" per word of the top of its user
// stack for a user thread blocked in an exec call. The op collects the block, parses it and names pc/lr (and the
// stack words that are code addresses) with the symbol file.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

var (
	reDumpEnd  = regexp.MustCompile(`\[dump\] ===== end =====`)
	reDumpHead = regexp.MustCompile(`^\[dump\] (\S.*?) ms=(0x[0-9a-fA-F]+)`)
	reDumpWait = regexp.MustCompile(`waits on (\S+)`)
	reDumpPC   = regexp.MustCompile(`pc=(0x[0-9a-fA-F]+)`)
	reDumpLR   = regexp.MustCompile(`lr=(0x[0-9a-fA-F]+)`)
	reDumpSP   = regexp.MustCompile(`sp=(0x[0-9a-fA-F]+)`)
	reDumpStk  = regexp.MustCompile(` s=(0x[0-9a-fA-F]+)`)
)

// DThread::iMState (kern_priv.h)
var mStates = []string{"created", "dead", "ready (or blocked in the nanokernel)", "waitsem", "waitsem-suspended", "waitmutex",
	"waitmutex-suspended", "holdmutex-pending", "waitcondvar", "waitcondvar-suspended"}

type ThreadInfo struct {
	Thread  string   `json:"thread"` // process::thread
	State   string   `json:"state"`
	WaitsOn string   `json:"waits_on,omitempty"`
	PC      string   `json:"pc,omitempty"`    // user pc of the exec call it is blocked in, "function [image]"
	LR      string   `json:"lr,omitempty"`    // its caller
	SP      string   `json:"sp,omitempty"`    // its user stack pointer
	Stack   []string `json:"stack,omitempty"` // the top words of its user stack; the ones in code are named (callers)
}

type Threads struct {
	Count   int          `json:"count"`
	Symbols string       `json:"symbols"`
	Threads []ThreadInfo `json:"threads"`
}

func parseDump(lines []string, tab *symTab) []ThreadInfo {
	var out []ThreadInfo
	in := false
	var cur *ThreadInfo
	var rest strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		s := rest.String()
		if m := reDumpWait.FindStringSubmatch(s); m != nil {
			cur.WaitsOn = m[1]
		}
		name := func(h string) string {
			v, _ := strconv.ParseUint(strings.TrimPrefix(h, "0x"), 16, 64)
			img, fn := tab.lookup(v &^ 1)
			switch {
			case fn != "":
				return fmt.Sprintf("%s [%s] (%s)", fn, img, h)
			case img != "":
				return fmt.Sprintf("%s+? (%s)", img, h)
			}
			return h
		}
		if m := reDumpPC.FindStringSubmatch(s); m != nil {
			cur.PC = name(m[1])
		}
		if m := reDumpLR.FindStringSubmatch(s); m != nil {
			cur.LR = name(m[1])
		}
		if m := reDumpSP.FindStringSubmatch(s); m != nil {
			cur.SP = m[1]
		}
		for _, m := range reDumpStk.FindAllStringSubmatch(s, -1) {
			cur.Stack = append(cur.Stack, name(m[1]))
		}
		out = append(out, *cur)
		cur = nil
		rest.Reset()
	}
	for _, l := range lines {
		switch {
		case strings.Contains(l, "[dump] ===== thread dump"):
			in, out = true, nil
			continue
		case reDumpEnd.MatchString(l):
			flush()
			return out
		case !in:
			continue
		}
		if m := reDumpHead.FindStringSubmatch(l); m != nil {
			flush()
			ms, _ := strconv.ParseUint(strings.TrimPrefix(m[2], "0x"), 16, 32)
			st := fmt.Sprintf("state %d", ms)
			if int(ms) < len(mStates) {
				st = mStates[ms]
			}
			cur = &ThreadInfo{Thread: m[1], State: st}
			rest.WriteString(l[len(m[0]):])
			continue
		}
		if cur != nil && strings.HasPrefix(l, " ") {
			rest.WriteString(l)
		}
	}
	flush()
	return out
}

func (d *Driver) threads(ctx context.Context, l device.Link, syms string) (Threads, error) {
	m := l.Mark()
	if err := l.Write(ctx, []byte{'D'}); err != nil {
		return Threads{}, err
	}
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := l.WaitLine(c, m, reDumpEnd); err != nil {
		return Threads{}, fmt.Errorf("no thread dump (needs caps 'threads'): %w", err)
	}
	var tab *symTab
	note := "none"
	if syms != "" {
		var err error
		if tab, err = loadSyms(syms); err != nil {
			note, tab = "unreadable: "+err.Error(), nil
		} else {
			note = syms
		}
	}
	ts := parseDump(l.LinesSince(m, 20000), tab)
	return Threads{Count: len(ts), Symbols: note, Threads: ts}, nil
}
