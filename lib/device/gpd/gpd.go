// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package gpd drives the GPD Win 2 test rig: a real PC that netboots the Phorepoc kernel from PhoneLab over HTTP (iPXE ->
// GRUB -> phorepoc.elf, all served from /gpd/ by SlotHandler) and talks back over UDP (kernel log broadcasts on :6666,
// commands on :6667; see port.go). The rig is a second kind of device next to the PinePhone: it reuses the same Session,
// boot and log store, so log_lines, wait_for, boots and boot_report work unchanged.
//
// Config, [[device]] id = "gpd-win2" driver = "gpd", [device.opts]:
//
//	listen_log     "0.0.0.0:6666"   the log + reply socket
//	cmd_port       6667
//	to             "255.255.255.255" where commands go (a directed broadcast such as 192.168.2.255 on a multi-homed host)
//	ip             accept packets only from this source address (empty = any)
//	mac            the rig adapter's MAC: health compares it with the ping reply (informational)
//	token          u32 command token (default 0 = any)
//	link_timeout_s link is down after this much silence (default 20)
//	ping_s         keep-alive ping period (default 5; the link is dropped after four silent periods, so 20 s)
//	wd_min_s       an unexpected reset after at least this much silence is a watchdog reset (default 15)
//	wd_max_s       ... and at most this (default 300)
//	slot_dir       the HTTP slot directory (default <data_dir>/gpd)
//	lab            "<lab ip>:8737": grubx64.efi pushed without it is flagged (GRUB_LAB)
package gpd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
	"github.com/Phorepoc/pdk/lib/device/phlink"
	"github.com/Phorepoc/pdk/lib/transport"
)

func init() {
	device.Register("gpd", func(cfg map[string]any) (device.Driver, error) { return New(cfg) })
}

// SlotFiles are the files the iPXE/GRUB chain fetches from /gpd/.
var SlotFiles = []string{"boot.ipxe", "grubx64.efi", "phorepoc.elf"}

const slotMax = 1 << 30

type Driver struct {
	op        *Opener
	token     uint32
	mac       string
	lab       string
	pingEvery time.Duration
	timeout   time.Duration
	slotDir   string
	id        atomic.Uint32

	mu         sync.Mutex
	pendingSha string
	pendingAt  time.Time
	fetches    map[string]int
	lastFetch  time.Time
	shaCache   map[string]shaEntry
}

type shaEntry struct {
	size  int64
	mtime time.Time
	sha   string
}

// New reads the [device.opts] section.
func New(cfg map[string]any) (*Driver, error) {
	str := func(k string) string { s, _ := cfg[k].(string); return s }
	num := func(k string, def int64) int64 {
		switch v := cfg[k].(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
		return def
	}
	d := &Driver{
		op: &Opener{Listen: str("listen_log"), CmdPort: int(num("cmd_port", 6667)), To: str("to"), IP: str("ip"),
			LinkWindow: time.Duration(num("link_timeout_s", 20)) * time.Second,
			WDMin:      time.Duration(num("wd_min_s", 15)) * time.Second,
			WDMax:      time.Duration(num("wd_max_s", 300)) * time.Second},
		token: uint32(num("token", 0)), mac: strings.ToLower(str("mac")), lab: str("lab"), slotDir: str("slot_dir"),
		pingEvery: time.Duration(num("ping_s", 5)) * time.Second, timeout: 2 * time.Second,
		fetches: map[string]int{}, shaCache: map[string]shaEntry{},
	}
	d.id.Store(uint32(time.Now().UnixMilli()))
	return d, nil
}

func (*Driver) Name() string { return "gpd" }

// Opener is the UDP link (the service uses it instead of the serial default).
func (d *Driver) Opener() transport.Opener { return d.op }

// SetDataDir picks the default slot directory (<data_dir>/gpd).
func (d *Driver) SetDataDir(dir string) {
	if d.slotDir == "" {
		d.slotDir = filepath.Join(dir, "gpd")
	}
}

// WorksOffline: ops that do not need the debug link (the image slot is plain files; push sends its reboot best-effort).
func (*Driver) WorksOffline(op string) bool { return op == "push" || op == "info" }

// KeepAlive pings the rig so a quiet kernel still shows a live link (device.KeepAliver).
func (d *Driver) KeepAlive() (time.Duration, func() []byte) {
	if d.pingEvery <= 0 {
		return 0, nil
	}
	return d.pingEvery, func() []byte { return cmdPacket(d.token, d.id.Add(1), 'P', nil) }
}

func prm(name, typ, desc string, req bool) device.Param {
	return device.Param{Name: name, Type: typ, Description: desc, Required: req}
}

func (*Driver) Ops() []device.OpSpec {
	xy := []device.Param{prm("x", "int", "screen pixels (the size screenshot returns)", true), prm("y", "int", "", true)}
	return []device.OpSpec{
		{Name: "info", Capability: "status", Description: "The rig: link state, the rig's IP, a ping (boot id, MAC), and the HTTP slot (files, sha256, fetches)."},
		{Name: "reboot", Capability: "power", Reboots: true, Description: "Reset the rig (the R command: CF9). It netboots again from the slot."},
		{Name: "hang", Capability: "debug", Description: "Stop the TCO kick and mask interrupts (the H command): the rig must be reset by its watchdog (recorded as a watchdog fault)."},
		{Name: "threads", Capability: "debug", Description: "Thread dump (the D command); the dump arrives in the kernel log and is returned.",
			Params: []device.Param{prm("wait_s", "int", "seconds to collect output (default 6)", false)}},
		{Name: "dump", Capability: "debug", Description: "Same as threads on this device (the D command).",
			Params: []device.Param{prm("wait_s", "int", "seconds to collect output (default 6)", false)}},
		{Name: "push", Capability: "slots", Reboots: true,
			Description: "Put an image into the HTTP slot the rig netboots from (phorepoc.elf = the ISO's boot/phorepoc.elf), record its sha256, then (by default) reboot the rig. Works with the link down: the rig takes the image at its next netboot.",
			Params: []device.Param{
				prm("blob", "blob", "sha256 of an uploaded blob", false),
				prm("file", "file", "path of the image on the server's disk", false),
				prm("name", "string", "slot file: phorepoc.elf (default), grubx64.efi or boot.ipxe", false),
				prm("reboot", "bool", "send the reboot command afterwards (default true)", false),
			}},
		{Name: "screenshot", Capability: "screen", Description: "The OS screen (the S command): reassembled from the pixel stream (re-asked on a gap), stored as a PNG blob.",
			Params: []device.Param{prm("out", "file", "also save the PNG to this server path", false)}},
		{Name: "key", Capability: "input", Description: "Press a key. key = a name (lsk/F5, select/F6, rsk/F7, power/F9, menu/F10, up, down, left, right, enter, 0-9, a-z ...) or a TStdScanCode number.",
			Params: []device.Param{prm("key", "string", "key name or scan code", true), prm("action", "string", "press (default) | down | up", false),
				prm("hold_ms", "int", "press duration (default 100)", false)}},
		{Name: "text", Capability: "input", Description: "Type letters, digits, space and newline as key presses.",
			Params: []device.Param{prm("text", "string", "text to type", true)}},
		{Name: "touch", Capability: "input", Description: "Pointer at x,y (the T command). action = tap (default) | down | move | up.",
			Params: append(append([]device.Param{}, xy...), prm("action", "string", "tap | down | move | up", false), prm("hold_ms", "int", "tap duration (default 100)", false))},
		{Name: "swipe", Capability: "input", Description: "Drag from x1,y1 to x2,y2 over ms milliseconds.",
			Params: []device.Param{prm("x1", "int", "", true), prm("y1", "int", "", true), prm("x2", "int", "", true), prm("y2", "int", "", true),
				prm("ms", "int", "duration (default 300)", false), prm("steps", "int", "move events (default 12)", false)}},
	}
}

// ---- log classification

var (
	reBootMark  = regexp.MustCompile(`^===== rig: boot ([0-9a-f]{8}) from (\S+) =====$`)
	reLost      = regexp.MustCompile(`^===== rig: (\d+) packets lost =====$`)
	reWatchdog  = regexp.MustCompile(`^===== rig: watchdog reset after (\d+)s of silence =====$`)
	reUnexp     = regexp.MustCompile(`^===== rig: unexpected reset after (\d+)s of silence =====$`)
	reCommanded = regexp.MustCompile(`^===== rig: commanded reset =====$`)
	reTag       = regexp.MustCompile(`^\[(tco|netlog|ure|disklog)\]`)
	// the rig's Linux side (adaptation-pc tools/gpd-rig/linux/phorepoc-collect): how the last Phorepoc boot ended
	reReport    = regexp.MustCompile(`^\[rig\] report kind=(\S+) boot=(\S+)(?: bytes=(\d+))?(?: log=(\S+))?(?: stage=(\S+))?`)
	reRigFault  = regexp.MustCompile(`^\[rig\] fault (.*)$`)
	reFatal     = regexp.MustCompile(`FATAL|\*\*\* ExcFault|kernel panic`)
	reThreads   = regexp.MustCompile(`(?i)thread dump`)
)

// BootStart: the marker the link inserts for a new PHL1 boot id.
func (*Driver) BootStart(line string) bool { return reBootMark.MatchString(line) }

func (d *Driver) Classify(line string) []device.Event {
	if strings.HasPrefix(line, "[rig] ") {
		// a report replays the failed boot's last lines ("[rig] | ..."): no fault events from those
		if m := reReport.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[3])
			return []device.Event{{Kind: "rig.report", Data: map[string]any{"kind": m[1], "boot_id": m[2], "bytes": n,
				"log": m[4], "stage": m[5], "text": line}}}
		}
		if m := reRigFault.FindStringSubmatch(line); m != nil {
			return []device.Event{{Kind: "rig.fault", Data: map[string]any{"text": m[1]}}}
		}
		return nil
	}
	if !strings.HasPrefix(line, "=====") {
		var ev []device.Event
		if m := reTag.FindStringSubmatch(line); m != nil {
			ev = append(ev, device.Event{Kind: m[1], Data: map[string]any{"text": line}})
		}
		if reFatal.MatchString(line) {
			ev = append(ev, device.Event{Kind: "fault", Data: map[string]any{"kind": "fatal", "text": line}})
		}
		if reThreads.MatchString(line) {
			ev = append(ev, device.Event{Kind: "threads.dump", Data: map[string]any{"text": line}})
		}
		return ev
	}
	if m := reBootMark.FindStringSubmatch(line); m != nil {
		ev := []device.Event{{Kind: "boot.start", Data: map[string]any{"boot_id": m[1], "ip": m[2]}}}
		d.mu.Lock()
		sha, at := d.pendingSha, d.pendingAt
		d.pendingSha = ""
		d.mu.Unlock()
		if sha != "" && time.Since(at) < 30*time.Minute {
			ev = append(ev, device.Event{Kind: "boot.slot", Data: map[string]any{"slot": sha[:12], "sha256": sha}})
		}
		return ev
	}
	if m := reLost.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return []device.Event{{Kind: "link.loss", Data: map[string]any{"count": n}}}
	}
	// the reset markers come before the new boot's own marker: the fault belongs to the boot that just ended
	if m := reWatchdog.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return []device.Event{{Kind: "fault", Data: map[string]any{"kind": "watchdog", "silence_s": n}}}
	}
	if m := reUnexp.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return []device.Event{{Kind: "fault", Data: map[string]any{"kind": "reset", "silence_s": n}}}
	}
	if reCommanded.MatchString(line) {
		return []device.Event{{Kind: "reset.commanded"}}
	}
	return nil
}

// ---- commands

func (d *Driver) nextID() uint32 { return d.id.Add(1) }

type reply struct {
	status int32
	data   []byte
}

func parseReply(p []byte, id uint32) (reply, bool) {
	if len(p) < 13 || string(p[:4]) != replyMagic || binary.LittleEndian.Uint32(p[4:]) != id {
		return reply{}, false
	}
	return reply{status: int32(binary.LittleEndian.Uint32(p[9:])), data: p[13:]}, true
}

// call sends one command and waits for its reply, asking again (same id) up to tries times.
func (d *Driver) call(ctx context.Context, l device.Link, op byte, args []byte, tries int) (reply, error) {
	frames, stop := l.Frames()
	defer stop()
	id := d.nextID()
	for t := 0; t < tries; t++ {
		if err := l.Write(ctx, cmdPacket(d.token, id, op, args)); err != nil {
			return reply{}, err
		}
		to := time.After(d.timeout)
	wait:
		for {
			select {
			case fr := <-frames:
				if fr.Type == frameReply && fr.SumOK {
					if r, ok := parseReply(fr.Payload, id); ok {
						return r, nil
					}
				}
			case <-to:
				break wait
			case <-ctx.Done():
				return reply{}, ctx.Err()
			}
		}
	}
	return reply{}, fmt.Errorf("gpd: no reply to %c from the rig after %d tries", op, tries)
}

func (d *Driver) simple(ctx context.Context, l device.Link, op byte, args []byte) error {
	r, err := d.call(ctx, l, op, args, 3)
	if err != nil {
		return err
	}
	if r.status != 0 {
		return fmt.Errorf("gpd: command %c refused, status %d", op, r.status)
	}
	return nil
}

func sleep(ctx context.Context, ms uint64) error {
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Driver) Exec(ctx context.Context, l device.Link, op string, a device.Args) (any, error) {
	switch op {
	case "info":
		return d.info(ctx, l)
	case "reboot":
		return d.reboot(ctx, l)
	case "hang":
		return map[string]any{"sent": true}, d.simple(ctx, l, 'H', nil)
	case "threads", "dump":
		return d.threads(ctx, l, a)
	case "push":
		return d.push(ctx, l, a)
	case "screenshot":
		return d.screenshot(ctx, l, a)
	case "key":
		return d.key(ctx, l, a)
	case "text":
		return d.text(ctx, l, a)
	case "touch":
		return d.touch(ctx, l, a)
	case "swipe":
		return d.swipe(ctx, l, a)
	}
	return nil, fmt.Errorf("gpd: unknown op %q", op)
}

// reboot: the rig may reset before its reply leaves, so a missing reply is not a failure.
func (d *Driver) reboot(ctx context.Context, l device.Link) (any, error) {
	r, err := d.call(ctx, l, 'R', nil, 1)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return map[string]any{"sent": true, "acked": false}, nil
	}
	if r.status != 0 {
		return nil, fmt.Errorf("gpd: reboot refused, status %d", r.status)
	}
	return map[string]any{"sent": true, "acked": true}, nil
}

func (d *Driver) threads(ctx context.Context, l device.Link, a device.Args) (any, error) {
	wait, err := a.UintOr("wait_s", 32, 6)
	if err != nil {
		return nil, err
	}
	wait = min(wait, 60)
	m := l.Mark()
	if err := d.simple(ctx, l, 'D', nil); err != nil {
		return nil, err
	}
	// collect until the output has been quiet for 1.5 s (and something came), or wait_s ends
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	last, quiet := 0, time.Now()
	for time.Now().Before(deadline) {
		if err := sleep(ctx, 150); err != nil {
			return nil, err
		}
		n := len(l.LinesSince(m, 1<<16))
		if n != last {
			last, quiet = n, time.Now()
		} else if n > 0 && time.Since(quiet) > 1500*time.Millisecond {
			break
		}
	}
	return map[string]any{"lines": l.LinesSince(m, 1<<16)}, nil
}

// ---- input

func (d *Driver) keyCode(s string) (uint16, error) {
	if v, ok := phlink.Keys[strings.ToLower(s)]; ok {
		return uint16(v), nil
	}
	if f := strings.ToLower(s); len(f) >= 2 && f[0] == 'f' { // the PC board's F-keys
		pc := map[string]string{"f5": "lsk", "f6": "select", "f7": "rsk", "f9": "power", "f10": "menu"}
		if k, ok := pc[f]; ok {
			return uint16(phlink.Keys[k]), nil
		}
	}
	if v, err := strconv.ParseUint(s, 0, 16); err == nil {
		return uint16(v), nil
	}
	if len(s) == 1 {
		c := s[0]
		switch {
		case c >= '0' && c <= '9', c >= 'A' && c <= 'Z':
			return uint16(c), nil
		case c >= 'a' && c <= 'z':
			return uint16(c - 'a' + 'A'), nil
		}
	}
	return 0, fmt.Errorf("unknown key %q", s)
}

func keyArgs(code uint16, down bool) []byte {
	b := binary.LittleEndian.AppendUint16(nil, code)
	if down {
		return append(b, 1)
	}
	return append(b, 0)
}

func (d *Driver) press(ctx context.Context, l device.Link, code uint16, action string, hold uint64) error {
	if action != "up" {
		if err := d.simple(ctx, l, 'K', keyArgs(code, true)); err != nil {
			return err
		}
	}
	if action == "" || action == "press" {
		if err := sleep(ctx, hold); err != nil {
			_ = d.simple(context.WithoutCancel(ctx), l, 'K', keyArgs(code, false))
			return err
		}
	}
	if action != "down" {
		return d.simple(ctx, l, 'K', keyArgs(code, false))
	}
	return nil
}

func (d *Driver) key(ctx context.Context, l device.Link, a device.Args) (any, error) {
	ks, err := a.String("key")
	if err != nil {
		if v, ok := a["key"].(float64); ok {
			ks = strconv.Itoa(int(v))
		} else {
			return nil, err
		}
	}
	code, err := d.keyCode(ks)
	if err != nil {
		return nil, err
	}
	hold, err := a.UintOr("hold_ms", 32, 100)
	if err != nil {
		return nil, err
	}
	act, _ := a["action"].(string)
	if act != "" && act != "press" && act != "down" && act != "up" {
		return nil, fmt.Errorf("action must be press, down or up")
	}
	if err := d.press(ctx, l, code, act, hold); err != nil {
		return nil, err
	}
	return map[string]any{"code": code}, nil
}

func (d *Driver) text(ctx context.Context, l device.Link, a device.Args) (any, error) {
	s, err := a.String("text")
	if err != nil {
		return nil, err
	}
	const shift = 0x12 // EStdKeyLeftShift
	for i, r := range s {
		var code uint16
		sh := false
		switch {
		case r == ' ':
			code = uint16(phlink.Keys["space"])
		case r == '\n':
			code = uint16(phlink.Keys["enter"])
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			code, sh = uint16(r), r >= 'A'
		case r >= 'a' && r <= 'z':
			code = uint16(r - 'a' + 'A')
		default:
			return nil, fmt.Errorf("text: character %q at %d has no key mapping here", r, i)
		}
		if sh {
			if err := d.simple(ctx, l, 'K', keyArgs(shift, true)); err != nil {
				return nil, err
			}
		}
		err := d.press(ctx, l, code, "press", 30)
		if sh {
			_ = d.simple(context.WithoutCancel(ctx), l, 'K', keyArgs(shift, false))
		}
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"typed": len([]rune(s))}, nil
}

func touchArgs(x, y uint64, act byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(x))
	b = binary.LittleEndian.AppendUint16(b, uint16(y))
	return append(b, act)
}

const (
	touchMove byte = 0
	touchDown byte = 1
	touchUp   byte = 2
)

func (d *Driver) touch(ctx context.Context, l device.Link, a device.Args) (any, error) {
	x, err := a.Uint("x", 16)
	if err != nil {
		return nil, err
	}
	y, err := a.Uint("y", 16)
	if err != nil {
		return nil, err
	}
	hold, err := a.UintOr("hold_ms", 32, 100)
	if err != nil {
		return nil, err
	}
	act, _ := a["action"].(string)
	switch act {
	case "", "tap":
		if err := d.simple(ctx, l, 'T', touchArgs(x, y, touchDown)); err != nil {
			return nil, err
		}
		if err := sleep(ctx, hold); err != nil {
			_ = d.simple(context.WithoutCancel(ctx), l, 'T', touchArgs(x, y, touchUp))
			return nil, err
		}
		return nil, d.simple(ctx, l, 'T', touchArgs(x, y, touchUp))
	case "down":
		return nil, d.simple(ctx, l, 'T', touchArgs(x, y, touchDown))
	case "move":
		return nil, d.simple(ctx, l, 'T', touchArgs(x, y, touchMove))
	case "up":
		return nil, d.simple(ctx, l, 'T', touchArgs(x, y, touchUp))
	}
	return nil, fmt.Errorf("action must be tap, down, move or up")
}

func (d *Driver) swipe(ctx context.Context, l device.Link, a device.Args) (any, error) {
	var v [4]uint64
	for i, k := range []string{"x1", "y1", "x2", "y2"} {
		var err error
		if v[i], err = a.Uint(k, 16); err != nil {
			return nil, err
		}
	}
	ms, err := a.UintOr("ms", 32, 300)
	if err != nil {
		return nil, err
	}
	steps, err := a.UintOr("steps", 32, 12)
	if err != nil {
		return nil, err
	}
	steps = max(steps, 1)
	if err := d.simple(ctx, l, 'T', touchArgs(v[0], v[1], touchDown)); err != nil {
		return nil, err
	}
	lerp := func(p, q, i uint64) uint64 { return uint64(int64(p) + (int64(q)-int64(p))*int64(i)/int64(steps)) }
	for i := uint64(1); i <= steps; i++ {
		if err := sleep(ctx, ms/steps); err != nil {
			break
		}
		if err := d.simple(ctx, l, 'T', touchArgs(lerp(v[0], v[2], i), lerp(v[1], v[3], i), touchMove)); err != nil {
			break
		}
	}
	return nil, d.simple(context.WithoutCancel(ctx), l, 'T', touchArgs(v[2], v[3], touchUp))
}

// ---- screenshot

func (d *Driver) screenshot(ctx context.Context, l device.Link, a device.Args) (any, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		w, h, px, got, err := d.shotOnce(ctx, l)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if got < w*h {
			lastErr = fmt.Errorf("gpd: screenshot incomplete (%d of %d pixels)", got, w*h)
			continue // a gap: ask again
		}
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for i := 0; i < w*h; i++ { // the stream is little-endian XRGB: B G R X
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = px[i*4+2], px[i*4+1], px[i*4], 255
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		res := map[string]any{"width": w, "height": h, "size": buf.Len(), "attempts": attempt}
		if sha, err := l.PutBlob("screenshot", "screenshot.png", map[string]any{"w": w, "h": h}, buf.Bytes()); err == nil {
			res["blob"] = sha
		}
		if out, _ := a["out"].(string); out != "" {
			if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
				return nil, err
			}
			res["out"] = out
		}
		return res, nil
	}
	return nil, lastErr
}

func (d *Driver) shotOnce(ctx context.Context, l device.Link) (w, h int, px []byte, got int, err error) {
	frames, stop := l.Frames() // before the command: the stream starts at once
	defer stop()
	id := d.nextID()
	if err = l.Write(ctx, cmdPacket(d.token, id, 'S', nil)); err != nil {
		return
	}
	idle := time.NewTimer(d.timeout)
	defer idle.Stop()
	var seen []bool
	have := false
	for {
		select {
		case fr := <-frames:
			if !fr.SumOK {
				continue
			}
			if fr.Type == frameReply && !have {
				r, ok := parseReply(fr.Payload, id)
				if !ok {
					continue
				}
				if r.status != 0 {
					return 0, 0, nil, 0, fmt.Errorf("gpd: screenshot refused, status %d", r.status)
				}
				if len(r.data) < 4 {
					return 0, 0, nil, 0, fmt.Errorf("gpd: short screenshot reply")
				}
				w, h = int(binary.LittleEndian.Uint16(r.data)), int(binary.LittleEndian.Uint16(r.data[2:]))
				if w == 0 || h == 0 || w*h > 1<<24 {
					return 0, 0, nil, 0, fmt.Errorf("gpd: bad screen size %dx%d", w, h)
				}
				px, seen, have = make([]byte, w*h*4), make([]bool, w*h), true
			} else if fr.Type == frameScreen && have {
				p := fr.Payload
				if binary.LittleEndian.Uint32(p[4:]) != id {
					continue
				}
				first, n := int(binary.LittleEndian.Uint32(p[12:])), int(binary.LittleEndian.Uint16(p[16:]))
				if n == 0 {
					return // the end marker
				}
				if len(p) < 18+n*4 || first+n > w*h {
					continue
				}
				copy(px[first*4:], p[18:18+n*4])
				for i := first; i < first+n; i++ {
					if !seen[i] {
						seen[i] = true
						got++
					}
				}
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(d.timeout)
		case <-idle.C:
			if !have {
				return 0, 0, nil, 0, fmt.Errorf("gpd: no reply to the screenshot command")
			}
			return // the end marker was lost: judge by the pixel count
		case <-ctx.Done():
			return 0, 0, nil, 0, ctx.Err()
		}
	}
}

// ---- the HTTP image slot

var errSlotDir = fmt.Errorf("gpd: no slot directory (set opts.slot_dir or run under phonelab with a data dir)")

func (d *Driver) slotPath(name string) string { return filepath.Join(d.slotDir, name) }

func validSlotName(n string) bool {
	for _, s := range SlotFiles {
		if s == n {
			return true
		}
	}
	return false
}

func (d *Driver) push(ctx context.Context, l device.Link, a device.Args) (any, error) {
	if d.slotDir == "" {
		return nil, errSlotDir
	}
	name := "phorepoc.elf"
	if v, ok := a["name"].(string); ok && v != "" {
		name = v
	}
	if !validSlotName(name) {
		return nil, fmt.Errorf("name must be one of %v", SlotFiles)
	}
	var data []byte
	var err error
	switch {
	case a.Has("blob"):
		sha, _ := a.String("blob")
		var rc io.ReadCloser
		if rc, _, err = l.Blob(sha); err == nil {
			data, err = io.ReadAll(rc)
			rc.Close()
		}
	case a.Has("file"):
		p, _ := a.String("file")
		data, err = os.ReadFile(p)
	default:
		err = fmt.Errorf("push needs blob or file")
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > slotMax {
		return nil, fmt.Errorf("image size %d not in 1..%d", len(data), slotMax)
	}
	if len(data) > 0x8006 && string(data[0x8001:0x8006]) == "CD001" {
		return nil, fmt.Errorf("that is an ISO: push its boot/phorepoc.elf (extract it first)")
	}
	if name == "phorepoc.elf" && !bytes.HasPrefix(data, []byte("\x7fELF")) {
		return nil, fmt.Errorf("phorepoc.elf must be an ELF image")
	}
	if err := os.MkdirAll(d.slotDir, 0o755); err != nil {
		return nil, err
	}
	if err := d.writeSlot(name, data); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	res := map[string]any{"name": name, "bytes": len(data), "sha256": sha}
	if name == "grubx64.efi" && d.lab != "" && !bytes.Contains(data, []byte(d.lab)) {
		// GRUB's embedded config may be compressed, so this is a hint, not proof
		res["warning"] = fmt.Sprintf("the lab address %s was not found in grubx64.efi; build it with GRUB_LAB=%s", d.lab, d.lab)
	}
	l.Emit(device.Event{Kind: "slot.push", Data: map[string]any{"name": name, "sha256": sha, "bytes": len(data)}})
	if name == "phorepoc.elf" {
		d.mu.Lock()
		d.pendingSha, d.pendingAt = sha, time.Now()
		d.mu.Unlock()
	}
	res["rebooted"] = false
	if rb, ok := a["reboot"].(bool); !ok || rb {
		rc, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if r, err := d.reboot(rc, l); err != nil {
			res["reboot_note"] = "link down: the rig takes the image at its next netboot (" + err.Error() + ")"
		} else {
			res["rebooted"] = true
			res["reboot"] = r
		}
	}
	return res, nil
}

func (d *Driver) writeSlot(name string, data []byte) error {
	tmp := d.slotPath(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	var err error
	for i := 0; i < 10; i++ { // Windows refuses to replace a file the HTTP server still has open
		if err = os.Rename(tmp, d.slotPath(name)); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	os.Remove(tmp)
	return err
}

// SlotHandler serves /gpd/<file> from the slot directory (public, like the web UI: iPXE sends no token). An empty slot is a 404 on
// purpose: the rig falls back to its own OS after about 30 s.
func (d *Driver) SlotHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/gpd/")
		if d.slotDir == "" || !validSlotName(name) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(d.slotPath(name))
		if err != nil {
			log.Printf("[gpd] %s GET /gpd/%s: not in the slot (404)", r.RemoteAddr, name)
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		d.mu.Lock()
		d.fetches[name]++
		d.lastFetch = time.Now()
		d.mu.Unlock()
		if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
			log.Printf("[gpd] %s GET /gpd/%s (%d bytes)", r.RemoteAddr, name, st.Size())
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
}

func (d *Driver) slotSha(name string) (string, int64, bool) {
	st, err := os.Stat(d.slotPath(name))
	if err != nil || st.IsDir() {
		return "", 0, false
	}
	d.mu.Lock()
	e, ok := d.shaCache[name]
	d.mu.Unlock()
	if ok && e.size == st.Size() && e.mtime.Equal(st.ModTime()) {
		return e.sha, e.size, true
	}
	b, err := os.ReadFile(d.slotPath(name))
	if err != nil {
		return "", 0, false
	}
	sum := sha256.Sum256(b)
	e = shaEntry{st.Size(), st.ModTime(), hex.EncodeToString(sum[:])}
	d.mu.Lock()
	d.shaCache[name] = e
	d.mu.Unlock()
	return e.sha, e.size, true
}

func (d *Driver) info(ctx context.Context, l device.Link) (any, error) {
	res := map[string]any{"rig_ip": d.op.RigIP()}
	if ll := d.op.LastLog(); !ll.IsZero() {
		res["last_log_age_s"] = time.Since(ll).Seconds()
	}
	slot := map[string]any{}
	files := map[string]any{}
	if d.slotDir != "" {
		slot["dir"] = d.slotDir
		for _, n := range SlotFiles {
			if sha, size, ok := d.slotSha(n); ok {
				files[n] = map[string]any{"sha256": sha, "bytes": size}
			}
		}
	}
	slot["files"] = files
	d.mu.Lock()
	f := map[string]int{}
	for k, v := range d.fetches {
		f[k] = v
	}
	if !d.lastFetch.IsZero() {
		slot["last_fetch_age_s"] = time.Since(d.lastFetch).Seconds()
	}
	d.mu.Unlock()
	slot["fetches"] = f
	res["slot"] = slot
	// a ping, best effort: with the link down there is nobody to answer
	pc, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if r, err := d.call(pc, l, 'P', nil, 1); err == nil && len(r.data) >= 10 {
		mac := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", r.data[4], r.data[5], r.data[6], r.data[7], r.data[8], r.data[9])
		p := map[string]any{"boot": fmt.Sprintf("%08x", binary.LittleEndian.Uint32(r.data)), "mac": mac}
		if d.mac != "" {
			p["mac_ok"] = d.mac == mac
		}
		res["ping"] = p
	} else {
		res["ping"] = "no answer"
	}
	return res, nil
}
