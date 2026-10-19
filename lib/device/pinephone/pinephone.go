// Package pinephone drives the PinePhone running Phorepoc over phlink: the kernel-side CDC ACM function in
// phlink.cpp in adaptation-a64 (usbcc/) (A/B slots: a64_glue.cpp, boot.cmd). Wire protocol, host -> phone:
//
//	'R'                       reboot (watchdog)
//	'B'                       re-arm slot B (one-shot) + reboot
//	'U' len:LE32 sum:LE32 ... upload len bytes into slot B; sum = word sum, zero-padded
//	'P' addr:LE32 count:LE32  log count (<=64) words: "phlink: P <addr8> = <val8>"
//	'W' addr:LE32 value:LE32  write a word: "phlink: W <addr8> = <readback8>"
//
// Phone -> host is the kernel log as text lines; replies are lines in it.
package pinephone

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

func init() {
	device.Register("pinephone", func(cfg map[string]any) (device.Driver, error) { return &Driver{}, nil })
}

// SlotBMax mirrors KSlotBMax in a64_glue.cpp.
const SlotBMax = 1024 << 20

type Driver struct{ live liveState }

func (*Driver) Name() string { return "pinephone" }

func (*Driver) Ops() []device.OpSpec { return append(append(baseOps(), remoteOps()...), fsOps()...) }

func baseOps() []device.OpSpec {
	return []device.OpSpec{
		{Name: "reboot", Capability: "power", Reboots: true,
			Description: "Reboot the phone (watchdog reset). Boots slot B if it is armed, else the golden slot A."},
		{Name: "rearm", Capability: "slots", Reboots: true,
			Description: "Re-arm slot B for one more one-shot trial boot of the image already in it, and reboot."},
		{Name: "push", Capability: "slots", Reboots: true,
			Description: "Upload an image into slot B, wait for the phone's checksum verdict, then (by default) reboot into it.",
			Params: []device.Param{
				{Name: "blob", Type: "blob", Description: "sha256 of an uploaded image blob"},
				{Name: "file", Type: "file", Description: "path of an image on the server's disk (e.g. .pinephone-build/symbian.img)"},
				{Name: "reboot", Type: "bool", Description: "reboot into slot B after a good upload (default true)"},
				{Name: "mode", Type: "string", Description: "auto (default: delta if the running image supports it, else full) | full | delta -- delta sends only the 64 KB blocks that differ from slot B"},
			}},
		{Name: "peek", Capability: "memory",
			Description: "Read 32-bit words from a physical address (registers or RAM).",
			Params: []device.Param{
				{Name: "addr", Type: "uint32", Required: true, Description: "physical address (hex ok: 0x01ca0000)"},
				{Name: "count", Type: "int", Description: "words to read, 1..64 (default 1)"},
			}},
		{Name: "poke", Capability: "memory",
			Description: "Write one 32-bit word to a physical address; returns the readback.",
			Params: []device.Param{
				{Name: "addr", Type: "uint32", Required: true, Description: "physical address"},
				{Name: "value", Type: "uint32", Required: true, Description: "value to write"},
			}},
		{Name: "dump", Capability: "memory",
			Description: "Read several register ranges (\"0x01ca0000:64,0x01c20000:16\"; count in words, any size).",
			Params: []device.Param{
				{Name: "ranges", Type: "string", Required: true, Description: "comma-separated addr:count"},
			}},
	}
}

var (
	reBoot     = regexp.MustCompile(`_E32Startup reached HwInit0`)
	reSlot     = regexp.MustCompile(`\[EKERN\]\[slot\] running slot ([AB])\b(.*)`)
	reUpload   = regexp.MustCompile(`phlink: UPLOAD (OK|FAILED) len=([0-9a-fA-F]+) sum=([0-9a-fA-F]+)`)
	reUpProg   = regexp.MustCompile(`phlink: upload ([0-9a-fA-F]{8})$`)
	reExc      = regexp.MustCompile(`\*\*\* ExcFault|\[EKERN\]\[exc\] iR15 faulting PC=(0x[0-9a-fA-F]+)`)
	reCat      = regexp.MustCompile(`\[EKERN\]\s+category='([^']+)'`) // a panic has a category; '' = a normal exit
	reCpu      = regexp.MustCompile(`\[EKERN\]\[mon\] busy% cpu0\.\.3 = (\d+) (\d+) (\d+) (\d+)`)
	reRAM      = regexp.MustCompile(`\[EKERN\]\[mon\] free RAM KB=0x([0-9a-fA-F]+)`)
	reSdIO     = regexp.MustCompile(`\[EKERN\]\[mon\] sd io: calls=0x([0-9a-fA-F]+)(?: sectors=0x([0-9a-fA-F]+))?(?: ms=0x([0-9a-fA-F]+))?`)
	reKey      = regexp.MustCompile(`\[EKERN\]\[key\] power (down|up)`)
	reRebootQ  = regexp.MustCompile(`phlink: reboot requested`)
	reUserMode = regexp.MustCompile(`\[EKERN\]\[slot\].*(healthy|GOOD)`)

	rePeek = regexp.MustCompile(`phlink: P ([0-9a-fA-F]{8}) = ([0-9a-fA-F]{8})`)
	rePoke = regexp.MustCompile(`phlink: W ([0-9a-fA-F]{8}) = ([0-9a-fA-F]{8})`)
)

func (*Driver) BootStart(line string) bool { return reBoot.MatchString(line) }

func hexU(s string) uint64 { v, _ := strconv.ParseUint(s, 16, 64); return v }

func (*Driver) Classify(line string) []device.Event {
	var ev []device.Event
	add := func(kind string, data map[string]any) { ev = append(ev, device.Event{Kind: kind, Data: data}) }
	switch {
	case reBoot.MatchString(line):
		add("boot.start", nil)
	case reSlot.MatchString(line):
		m := reSlot.FindStringSubmatch(line)
		add("boot.slot", map[string]any{"slot": m[1], "detail": strings.TrimSpace(m[2])})
	case reUpload.MatchString(line):
		m := reUpload.FindStringSubmatch(line)
		add("upload.verdict", map[string]any{"ok": m[1] == "OK", "len": hexU(m[2]), "sum": m[3]})
	case reRebootQ.MatchString(line):
		add("reboot.requested", nil)
	case reKey.MatchString(line):
		add("key.power", map[string]any{"state": reKey.FindStringSubmatch(line)[1]})
	case reUserMode.MatchString(line):
		add("boot.healthy", map[string]any{"text": line})
	}
	// the monitor glues lines together sometimes, so these are not exclusive
	if m := reExc.FindStringSubmatch(line); m != nil {
		d := map[string]any{"text": line}
		if m[1] != "" {
			d["pc"] = m[1]
		}
		add("fault", d)
	}
	if m := reCat.FindStringSubmatch(line); m != nil {
		add("panic", map[string]any{"category": m[1]})
	}
	if m := reCpu.FindStringSubmatch(line); m != nil {
		var c []int
		for _, s := range m[1:] {
			n, _ := strconv.Atoi(s)
			c = append(c, n)
		}
		add("mon.cpu", map[string]any{"busy": c})
	}
	if m := reRAM.FindStringSubmatch(line); m != nil {
		add("mon.ram", map[string]any{"free_kb": hexU(m[1])})
	}
	if m := reSdIO.FindStringSubmatch(line); m != nil {
		add("mon.sdio", map[string]any{"calls": hexU(m[1]), "sectors": hexU(m[2]), "ms": hexU(m[3])})
	}
	return ev
}

func cmd9(c byte, a, b uint32) []byte {
	h := make([]byte, 9)
	h[0] = c
	binary.LittleEndian.PutUint32(h[1:], a)
	binary.LittleEndian.PutUint32(h[5:], b)
	return h
}

// WordSum is the phone's upload checksum (A64SlotUploadEnd): the sum of the little-endian words, zero-padded.
func WordSum(b []byte) uint32 {
	var s uint32
	n := len(b) &^ 3
	for i := 0; i < n; i += 4 {
		s += binary.LittleEndian.Uint32(b[i:])
	}
	if n < len(b) {
		var t [4]byte
		copy(t[:], b[n:])
		s += binary.LittleEndian.Uint32(t[:])
	}
	return s
}

type Word struct {
	Addr  string `json:"addr"`
	Value string `json:"value"`
}

func (d *Driver) Exec(ctx context.Context, l device.Link, op string, a device.Args) (any, error) {
	if res, ok, err := d.execRemote(ctx, l, op, a); ok {
		return res, err
	}
	switch op {
	case "reboot":
		return nil, l.Write(ctx, []byte{'R'})
	case "rearm":
		m := l.Mark()
		if err := l.Write(ctx, []byte{'B'}); err != nil {
			return nil, err
		}
		// the phone only answers when it cannot re-arm (no valid slot B); otherwise it resets at once
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if r, err := l.WaitLine(c, m, regexp.MustCompile(`phlink: slot B re-armed r=([0-9a-fA-F]+)`)); err == nil && hexU(r[1]) != 0 {
			return nil, fmt.Errorf("phone refused to re-arm slot B (r=%s): no valid image in slot B", r[1])
		}
		return nil, nil
	case "peek":
		addr, err := a.Uint32("addr")
		if err != nil {
			return nil, err
		}
		n, err := a.UintOr("count", 16, 1)
		if err != nil {
			return nil, err
		}
		return d.peek(ctx, l, addr, int(n))
	case "poke":
		addr, err := a.Uint32("addr")
		if err != nil {
			return nil, err
		}
		val, err := a.Uint32("value")
		if err != nil {
			return nil, err
		}
		m := l.Mark()
		if err := l.Write(ctx, cmd9('W', addr, val)); err != nil {
			return nil, err
		}
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		r, err := l.WaitLine(c, m, rePoke)
		if err != nil {
			return nil, fmt.Errorf("no poke reply: %w", err)
		}
		return Word{Addr: "0x" + r[1], Value: "0x" + r[2]}, nil
	case "dump":
		s, err := a.String("ranges")
		if err != nil {
			return nil, err
		}
		var out []Word
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			as, cs, _ := strings.Cut(part, ":")
			addr, err := strconv.ParseUint(as, 0, 32)
			if err != nil {
				return nil, fmt.Errorf("bad range %q", part)
			}
			cnt := uint64(1)
			if cs != "" {
				if cnt, err = strconv.ParseUint(cs, 0, 16); err != nil || cnt == 0 {
					return nil, fmt.Errorf("bad count in %q", part)
				}
			}
			for off := uint64(0); off < cnt; off += 64 {
				w, err := d.peek(ctx, l, uint32(addr+off*4), int(min(64, cnt-off)))
				if err != nil {
					return out, err
				}
				out = append(out, w...)
			}
		}
		return out, nil
	case "push":
		return d.push(ctx, l, a)
	}
	return nil, fmt.Errorf("pinephone: unknown op %q", op)
}

func (d *Driver) peek(ctx context.Context, l device.Link, addr uint32, n int) ([]Word, error) {
	if n < 1 || n > 64 {
		return nil, fmt.Errorf("count must be 1..64")
	}
	addr &^= 3
	m := l.Mark()
	if err := l.Write(ctx, cmd9('P', addr, uint32(n))); err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	last := fmt.Sprintf("%08x", addr+uint32(n-1)*4)
	re := regexp.MustCompile(`phlink: P (?i:` + last + `) = `)
	if _, err := l.WaitLine(c, m, re); err != nil {
		return nil, fmt.Errorf("peek reply incomplete: %w", err)
	}
	var out []Word
	seen := map[string]bool{}
	for _, s := range l.LinesSince(m, 4096) {
		for _, w := range rePeek.FindAllStringSubmatch(s, -1) {
			a := strings.ToLower(w[1])
			v := hexU(a)
			if v >= uint64(addr) && v < uint64(addr)+uint64(n)*4 && !seen[a] {
				seen[a] = true
				out = append(out, Word{Addr: "0x" + a, Value: "0x" + strings.ToLower(w[2])})
			}
		}
	}
	return out, nil
}

type PushResult struct {
	Bytes    int     `json:"bytes"`
	Sum      string  `json:"sum"`
	PhoneSum string  `json:"phone_sum"`
	Seconds  float64 `json:"seconds"`
	KBps     float64 `json:"kbps"`
	Rebooted bool    `json:"rebooted"`
}

func (d *Driver) push(ctx context.Context, l device.Link, a device.Args) (any, error) {
	var data []byte
	switch {
	case a.Has("blob"):
		sha, err := a.String("blob")
		if err != nil {
			return nil, err
		}
		rc, _, err := l.Blob(sha)
		if err != nil {
			return nil, err
		}
		data, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
	case a.Has("file"):
		p, err := a.String("file")
		if err != nil {
			return nil, err
		}
		if data, err = os.ReadFile(p); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("push needs blob or file")
	}
	if len(data) == 0 || len(data) > SlotBMax {
		return nil, fmt.Errorf("image size %d not in 1..%d", len(data), SlotBMax)
	}
	reboot := true
	if v, ok := a["reboot"].(bool); ok {
		reboot = v
	}
	mode := "auto"
	if v, ok := a["mode"].(string); ok && v != "" {
		mode = v
	}
	switch mode {
	case "auto", "delta":
		res, err := d.pushDelta(ctx, l, data, reboot)
		if err != errNoDelta || mode == "delta" {
			return res, err
		}
		// an image without the delta commands: the whole image, as before
	case "full":
	default:
		return nil, fmt.Errorf("mode must be auto, full or delta")
	}
	sum := WordSum(data)
	m := l.Mark()
	t0 := time.Now()
	if err := l.Write(ctx, cmd9('U', uint32(len(data)), sum)); err != nil {
		return nil, err
	}
	const chunk = 16384
	for off := 0; off < len(data); off += chunk {
		end := min(off+chunk, len(data))
		if err := l.Write(ctx, data[off:end]); err != nil {
			return nil, fmt.Errorf("upload write at %d: %w", off, err)
		}
		l.Progress("send", int64(end), int64(len(data)))
	}
	secs := max(time.Since(t0).Seconds(), 0.001) // KBps must stay finite for JSON
	l.Progress("verify", 0, 1)
	c, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	r, err := l.WaitLine(c, m, reUpload)
	if err != nil {
		return nil, fmt.Errorf("no verdict from the phone: %w", err)
	}
	res := PushResult{Bytes: len(data), Sum: fmt.Sprintf("%08x", sum), PhoneSum: r[3], Seconds: secs, KBps: float64(len(data)) / 1024 / secs}
	if r[1] != "OK" {
		return res, fmt.Errorf("phone says UPLOAD FAILED (len=%s sum=%s)", r[2], r[3])
	}
	if reboot {
		if err := l.Write(ctx, []byte{'R'}); err != nil {
			return res, err
		}
		res.Rebooted = true
	}
	return res, nil
}
