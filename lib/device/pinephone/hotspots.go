package pinephone

// The hotspots op: phlink 'H' 1 starts the kernel's PC sampler on every busy core (a64_glue.cpp A64HotStart, every
// 2 ms), 'H' 0 stops it and streams the (thread, user pc) histogram as blob kind 2. Addresses are named with a
// symbol file exported from the ROM map (the ROM map's symbol export).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const blobHotspots = 2

type hotRaw struct {
	Samples, Idle, Lost, PeriodMs uint64
	Names                         map[uint64]string // NThread address -> "process / thread"
	Hits                          []hotHit
}

type hotHit struct {
	Thr, PC, N, LR uint64
}

func parseHot(b []byte) (*hotRaw, error) {
	h := &hotRaw{Names: map[uint64]string{}}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 4096), 1<<20)
	hex := func(f string) uint64 { v, _ := strconv.ParseUint(f, 16, 64); return v }
	seen := false
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), " ", 3)
		switch {
		case f[0] == "S" && len(f) >= 2:
			g := strings.Fields(sc.Text())
			if len(g) >= 5 {
				h.Samples, h.Idle, h.Lost, h.PeriodMs = hex(g[1]), hex(g[2]), hex(g[3]), hex(g[4])
				seen = true
			}
		case f[0] == "W" && len(f) >= 3:
			h.Names[hex(f[1])] = strings.TrimSpace(f[2])
		case f[0] == "X" && len(f) >= 3:
			g := strings.Fields(sc.Text())
			if len(g) >= 4 {
				x := hotHit{Thr: hex(g[1]), PC: hex(g[2]), N: hex(g[3])}
				if len(g) >= 5 {
					x.LR = hex(g[4])
				}
				h.Hits = append(h.Hits, x)
			}
		}
	}
	if !seen {
		return nil, fmt.Errorf("hotspots blob without a header (%d bytes)", len(b))
	}
	return h, nil
}

// symbol tables, cached per file + mtime
type symImage struct {
	Addr, Size uint64
	Name       string
	Fn         []uint64
	FnName     []string
}

type symTab struct {
	mod  time.Time
	imgs []symImage
}

var symCache sync.Map // path -> *symTab

func loadSyms(path string) (*symTab, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if v, ok := symCache.Load(path); ok && v.(*symTab).mod.Equal(st.ModTime()) {
		return v.(*symTab), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t := &symTab{mod: st.ModTime()}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		g := strings.SplitN(sc.Text(), " ", 4)
		switch {
		case g[0] == "I" && len(g) == 4:
			a, _ := strconv.ParseUint(g[1], 16, 64)
			s, _ := strconv.ParseUint(g[2], 16, 64)
			t.imgs = append(t.imgs, symImage{Addr: a, Size: s, Name: g[3]})
		case g[0] == "F" && len(g) >= 3 && len(t.imgs) > 0:
			a, _ := strconv.ParseUint(g[1], 16, 64)
			im := &t.imgs[len(t.imgs)-1]
			im.Fn = append(im.Fn, a)
			im.FnName = append(im.FnName, strings.Join(g[2:], " "))
		}
	}
	for i := range t.imgs {
		im := &t.imgs[i]
		idx := make([]int, len(im.Fn))
		for k := range idx {
			idx[k] = k
		}
		sort.SliceStable(idx, func(x, y int) bool { return im.Fn[idx[x]] < im.Fn[idx[y]] })
		fn, nm := make([]uint64, len(idx)), make([]string, len(idx))
		for k, j := range idx {
			fn[k], nm[k] = im.Fn[j], im.FnName[j]
		}
		im.Fn, im.FnName = fn, nm
	}
	symCache.Store(path, t)
	return t, nil
}

// lookup returns (image, function) for a run address; "" when unknown.
func (t *symTab) lookup(pc uint64) (string, string) {
	if t == nil {
		return "", ""
	}
	for i := range t.imgs {
		im := &t.imgs[i]
		if pc < im.Addr || pc >= im.Addr+im.Size {
			continue
		}
		k := sort.Search(len(im.Fn), func(j int) bool { return im.Fn[j] > pc }) - 1
		if k < 0 {
			return im.Name, ""
		}
		return im.Name, im.FnName[k]
	}
	return "", ""
}

type HotFunc struct {
	Function string  `json:"function"` // symbol, or the pc in hex when unknown
	Image    string  `json:"image"`    // XIP image ("" = RAM-loaded code or not user code)
	Thread   string  `json:"thread"`   // "process / thread" (the busiest one for this function)
	Hits     uint64  `json:"hits"`
	Pct      float64 `json:"pct"` // % of busy samples
	// the user lr at the sample, by function: the CALLER for a leaf (an Exec stub, memcpy); for a non-leaf it can be
	// stale (whatever the function called last), so read it as a hint
	Callers []HotThread `json:"callers,omitempty"`
}

type HotThread struct {
	Thread string  `json:"thread"`
	Hits   uint64  `json:"hits"`
	Pct    float64 `json:"pct"`
}

type Hotspots struct {
	Seconds   float64     `json:"seconds"`
	Samples   uint64      `json:"samples"` // busy-core samples
	Idle      uint64      `json:"idle_samples"`
	Lost      uint64      `json:"lost"`
	Symbols   string      `json:"symbols"` // the symbol file used, or why none
	Functions []HotFunc   `json:"functions"`
	Images    []HotThread `json:"images"` // per image (Thread = image name)
	Threads   []HotThread `json:"threads"`
}

func (d *Driver) hotRequest(ctx context.Context, l device.Link, start bool) ([]byte, error) {
	frames, stop := l.Frames()
	defer stop()
	m := l.Mark()
	a := uint32(0)
	if start {
		a = 1
	}
	if err := l.Write(ctx, cmd9('H', a, 0)); err != nil {
		return nil, err
	}
	errc := make(chan string, 1)
	ec, ecancel := context.WithCancel(ctx)
	defer ecancel()
	go func() {
		if r, err := l.WaitLine(ec, m, reHotRe); err == nil {
			errc <- r[1]
		}
	}()
	var buf bytes.Buffer
	idle := time.NewTimer(10 * time.Second)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case e := <-errc:
			switch e {
			case "H started":
				if start {
					return nil, nil
				}
			case "rx 'H'":
				return nil, fmt.Errorf("the running image has no hotspots sampler (no 'hotspots' in its caps)")
			default:
				return nil, fmt.Errorf("phone: %s", e)
			}
		case <-idle.C:
			return nil, fmt.Errorf("hotspots: no reply")
		case f := <-frames:
			idle.Reset(10 * time.Second)
			if !f.SumOK {
				return nil, fmt.Errorf("hotspots frame %d: checksum mismatch", f.Seq)
			}
			switch f.Type {
			case frameBlobData:
				buf.Write(f.Payload)
			case frameBlobEnd:
				if len(f.Payload) < 8 || binary.LittleEndian.Uint32(f.Payload) != blobHotspots {
					buf.Reset()
					continue
				}
				if n := int(binary.LittleEndian.Uint32(f.Payload[4:])); n != buf.Len() {
					return nil, fmt.Errorf("hotspots: got %d of %d bytes", buf.Len(), n)
				}
				return buf.Bytes(), nil
			}
		}
	}
}

var reHotRe = regexp.MustCompile(`phlink: (H started|Q busy|rx 'H')`)

var hotStarted sync.Map // *Driver -> time.Time

// hotspots: action "start" | "stop" | "run" (start, wait seconds, stop).
func (d *Driver) hotspots(ctx context.Context, l device.Link, action string, secs time.Duration, top int, syms string) (any, error) {
	if action == "start" || action == "run" {
		if _, err := d.hotRequest(ctx, l, true); err != nil {
			return nil, err
		}
		hotStarted.Store(d, time.Now())
		if action == "start" {
			return map[string]any{"started": true}, nil
		}
		if err := sleep(ctx, secs); err != nil {
			return nil, err
		}
	}
	b, err := d.hotRequest(ctx, l, false)
	if err != nil {
		return nil, err
	}
	raw, err := parseHot(b)
	if err != nil {
		return nil, err
	}
	var since float64
	if v, ok := hotStarted.Load(d); ok {
		since = time.Since(v.(time.Time)).Seconds()
	}
	var tab *symTab
	note := "none (pass symbols = a symbol file exported from the ROM map)"
	if syms != "" {
		if tab, err = loadSyms(syms); err != nil {
			note = "unreadable: " + err.Error()
			tab = nil
		} else {
			note = syms
		}
	}
	return summarizeHot(raw, tab, top, since, note), nil
}

func summarizeHot(raw *hotRaw, tab *symTab, top int, secs float64, note string) Hotspots {
	h := Hotspots{Seconds: secs, Samples: raw.Samples, Idle: raw.Idle, Lost: raw.Lost, Symbols: note}
	type fk struct{ img, fn string }
	fnHits := map[fk]uint64{}
	fnThr := map[fk]map[string]uint64{}
	fnCall := map[fk]map[string]uint64{}
	imgHits := map[string]uint64{}
	thrHits := map[string]uint64{}
	for _, x := range raw.Hits {
		thr := raw.Names[x.Thr]
		if thr == "" {
			thr = fmt.Sprintf("(thread %x, gone)", x.Thr)
		}
		img, fn := tab.lookup(x.PC)
		if strings.HasPrefix(thr, "ekern") {
			img, fn = "(kernel thread)", "" // no user frame: its pc is noise
		}
		if fn == "" {
			fn = fmt.Sprintf("0x%08x", x.PC)
			if img == "(kernel thread)" {
				fn = "(kernel)"
			}
		}
		k := fk{img, fn}
		fnHits[k] += x.N
		if fnThr[k] == nil {
			fnThr[k] = map[string]uint64{}
			fnCall[k] = map[string]uint64{}
		}
		fnThr[k][thr] += x.N
		if x.LR != 0 && img != "(kernel thread)" {
			ci, cf := tab.lookup(x.LR)
			if cf == "" {
				cf = fmt.Sprintf("0x%08x", x.LR)
			}
			if ci != "" && ci != img {
				cf += " [" + ci + "]"
			}
			fnCall[k][cf] += x.N
		}
		label := img
		if label == "" {
			label = "(not in ROM)"
		}
		imgHits[label] += x.N
		thrHits[thr] += x.N
	}
	pct := func(n uint64) float64 {
		if raw.Samples == 0 {
			return 0
		}
		return float64(n*10000/raw.Samples) / 100
	}
	for k, n := range fnHits {
		best, bn := "", uint64(0)
		for t, c := range fnThr[k] {
			if c > bn || (c == bn && t < best) {
				best, bn = t, c
			}
		}
		h.Functions = append(h.Functions, HotFunc{Function: k.fn, Image: k.img, Thread: best, Hits: n, Pct: pct(n)})
	}
	sort.Slice(h.Functions, func(i, j int) bool {
		if h.Functions[i].Hits != h.Functions[j].Hits {
			return h.Functions[i].Hits > h.Functions[j].Hits
		}
		return h.Functions[i].Function < h.Functions[j].Function
	})
	if top > 0 && len(h.Functions) > top {
		h.Functions = h.Functions[:top]
	}
	list := func(m map[string]uint64) []HotThread {
		var out []HotThread
		for k, n := range m {
			out = append(out, HotThread{Thread: k, Hits: n, Pct: pct(n)})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Hits != out[j].Hits {
				return out[i].Hits > out[j].Hits
			}
			return out[i].Thread < out[j].Thread
		})
		return out
	}
	h.Images, h.Threads = list(imgHits), list(thrHits)
	for i := range h.Functions {
		f := &h.Functions[i]
		if c := list(fnCall[fk{f.Image, f.Function}]); len(c) > 0 {
			f.Callers = c[:min(3, len(c))]
		}
	}
	return h
}

// Symbolize maps a run address to (function, image) with a symbol file (core.Symbolizer: fault addresses in reports).
func (d *Driver) Symbolize(symbols string, pc uint64) (string, string, error) {
	t, err := loadSyms(symbols)
	if err != nil {
		return "", "", err
	}
	img, fn := t.lookup(pc)
	return fn, img, nil
}
