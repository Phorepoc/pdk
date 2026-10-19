package core

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/store"
)

// Built-in ops every device gets on top of its driver's.
var builtins = []device.OpSpec{
	{Name: "wait_for", Capability: "log",
		Description: "Wait for a log line matching a regular expression. By default only lines arriving after the " +
			"call count; set since_line to also search lines already received after that line number.",
		Params: []device.Param{
			{Name: "pattern", Type: "string", Required: true, Description: "RE2 regular expression"},
			{Name: "timeout_s", Type: "int", Description: "seconds to wait (default 60, max 900)"},
			{Name: "since_line", Type: "int", Description: "line sequence number to search from (health.lines of an earlier call)"},
		}},
	{Name: "send", Capability: "raw",
		Description: "Escape hatch: send raw bytes (text, or hex with hex=true). Prefer adding a proper op.",
		Params: []device.Param{
			{Name: "data", Type: "string", Required: true, Description: "text, or hex digits"},
			{Name: "hex", Type: "bool", Description: "data is hex"},
		}},
}

func (s *Session) Ops() []device.OpSpec {
	var out []device.OpSpec
	for _, o := range s.Driver.Ops() {
		if o.Reboots { // push ... wait=<pattern>: the new boot's report comes back with the result (Manager.Exec)
			o.Params = append(append([]device.Param{}, o.Params...), waitParams...)
		}
		out = append(out, o)
	}
	out = append(out, builtins...)
	return append(out, workflowOps...)
}

// logOnly: built-in ops that read the store or wait for lines; they never touch the port, so they do not take the
// op lock (they can watch an op in progress, e.g. wait_boot during a push)
var logOnly = map[string]bool{"wait_for": true, "wait_boot": true, "boot_report": true, "diff_boots": true}

// QuietCaller marks high-rate calls (the live view) that are not written to the op history when they succeed.
const QuietCaller = "live"

type WaitResult struct {
	Line   string   `json:"line"`
	Match  string   `json:"match"`
	Groups []string `json:"groups,omitempty"`
}

// Exec runs op as caller ("api", "mcp", "web", ...). Ops are serialized; every call is recorded.
func (s *Session) Exec(ctx context.Context, caller, op string, args device.Args) (any, error) {
	var spec *device.OpSpec
	for _, o := range s.Ops() {
		if o.Name == op {
			o := o
			spec = &o
			break
		}
	}
	if spec == nil {
		return nil, fmt.Errorf("device %s has no op %q", s.ID, op)
	}
	for _, p := range spec.Params {
		if p.Required && !args.Has(p.Name) {
			return nil, fmt.Errorf("%s: missing required %q", op, p.Name)
		}
	}
	// wait_for does not touch the port, so it does not take the op lock (it can watch an op in progress)
	exclusive := !logOnly[op]
	if exclusive {
		select {
		case s.opCh <- struct{}{}:
		case <-ctx.Done():
			return nil, fmt.Errorf("device busy with %q: %w", s.Health().BusyOp, ctx.Err())
		}
		defer func() { <-s.opCh }()
		// the busy op is shown live (status bar, device card): announce it at start and end. Live-view frames
		// (the quiet caller, several a second) are not announced; they would only flicker.
		if caller != QuietCaller {
			s.setBusy(op)
			defer s.setBusy("")
		}
	}
	t0 := time.Now()
	var res any
	var err error
	switch op {
	case "wait_for":
		res, err = s.waitFor(ctx, args)
	case "send":
		res, err = nil, s.send(ctx, args)
	case "wait_boot", "boot_report", "diff_boots", "input":
		res, _, err = s.workflow(ctx, op, args)
	default:
		wc, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = s.WaitConnected(wc)
		cancel()
		if err != nil {
			err = fmt.Errorf("device not connected: %w", err)
		} else {
			res, err = s.Driver.Exec(ctx, &link{s: s, op: op}, op, args)
		}
	}
	if caller == QuietCaller && err == nil {
		return res, err // live-view frames: one a second would drown the history (failures are still recorded)
	}
	rec := store.Op{Device: s.ID, TS: t0.UnixMilli(), Caller: caller, Op: op, MS: time.Since(t0).Milliseconds()}
	rec.Args, _ = json.Marshal(args)
	if res != nil {
		rec.Result, _ = json.Marshal(res)
		if len(rec.Result) > 64<<10 {
			rec.Result = json.RawMessage(`{"truncated":true}`)
		}
	}
	if err != nil {
		rec.Error = err.Error()
	}
	if id, e := s.st.AddOp(rec); e == nil {
		rec.ID = id
	}
	s.bus.Publish(Msg{Type: "op", Device: s.ID, Data: rec})
	return res, err
}

func (s *Session) waitFor(ctx context.Context, a device.Args) (any, error) {
	pat, err := a.String("pattern")
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	secs, err := a.UintOr("timeout_s", 32, 60)
	if err != nil {
		return nil, err
	}
	secs = min(secs, 900)
	m := s.Mark()
	if a.Has("since_line") {
		v, err := a.Uint("since_line", 64)
		if err != nil {
			return nil, err
		}
		m = device.Mark(v)
	}
	c, cancel := context.WithTimeout(ctx, time.Duration(secs)*time.Second)
	defer cancel()
	sm, err := s.waitLine(c, m, re)
	if err != nil {
		return nil, err
	}
	return WaitResult{Line: sm[0], Match: sm[1], Groups: sm[2:]}, nil
}

func (s *Session) send(ctx context.Context, a device.Args) error {
	d, err := a.String("data")
	if err != nil {
		return err
	}
	b := []byte(d)
	if h, _ := a["hex"].(bool); h {
		b = b[:0]
		for i := 0; i+1 < len(d); i += 2 {
			v, err := strconv.ParseUint(d[i:i+2], 16, 8)
			if err != nil {
				return fmt.Errorf("bad hex at %d", i)
			}
			b = append(b, byte(v))
		}
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.write(c, b)
}

func (s *Session) setBusy(op string) {
	s.mu.Lock()
	s.h.BusyOp = op
	h := s.healthLocked()
	s.mu.Unlock()
	s.bus.Publish(Msg{Type: "state", Device: s.ID, Data: h})
}
