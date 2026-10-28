// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package phlink

// The profile op: phlink 'Q' streams a snapshot of every thread's CPU time, every process and every chunk
// (the device side of phlink, text lines in blob frames 6/7). Two snapshots make CPU %; the driver keeps
// the previous one, so polling the op every N seconds costs one snapshot per poll.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/device"
)

const (
	frameBlobData = 6
	frameBlobEnd  = 7
	blobProfile   = 1
)

type snapshot struct {
	At       time.Time
	Now, Hz  uint64
	Cores    int
	Idle     map[int]uint64
	Procs    map[uint64]string
	Threads  map[uint64]snapThread
	Chunks   []snapChunk
	FreeKB   uint64
	KHeapKB  uint64
	RawBytes int
}

type snapThread struct {
	PID  uint64
	CPU  uint64
	Name string
}

type snapChunk struct {
	PID  uint64
	Size uint64
	Name string
}

func parseSnapshot(b []byte) (*snapshot, error) {
	s := &snapshot{Idle: map[int]uint64{}, Procs: map[uint64]string{}, Threads: map[uint64]snapThread{}, RawBytes: len(b)}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 4096), 1<<20)
	hex := func(f string) uint64 { v, _ := strconv.ParseUint(f, 16, 64); return v }
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), " ", 5)
		switch {
		case f[0] == "N" && len(f) >= 4:
			s.Now, s.Hz, s.Cores = hex(f[1]), hex(f[2]), int(hex(f[3]))
		case f[0] == "I" && len(f) >= 3:
			s.Idle[int(hex(f[1]))] = hex(f[2])
		case f[0] == "P" && len(f) >= 3:
			s.Procs[hex(f[1])] = strings.TrimSpace(strings.Join(f[2:], " "))
		case f[0] == "T" && len(f) >= 5:
			s.Threads[hex(f[1])] = snapThread{PID: hex(f[2]), CPU: hex(f[3]), Name: strings.TrimSpace(f[4])}
		case f[0] == "C" && len(f) >= 4:
			s.Chunks = append(s.Chunks, snapChunk{PID: hex(f[1]), Size: hex(f[2]), Name: strings.TrimSpace(strings.Join(f[3:], " "))})
		case f[0] == "M" && len(f) >= 3:
			s.FreeKB, s.KHeapKB = hex(f[1]), hex(f[2])
		}
	}
	if s.Hz == 0 {
		return nil, fmt.Errorf("profile snapshot without a header (%d bytes)", len(b))
	}
	return s, nil
}

// shortName: "Telephone[100058b3]0001" -> "Telephone"
func shortName(n string) string {
	if i := strings.IndexByte(n, '['); i > 0 {
		return n[:i]
	}
	return n
}

var (
	reQErrRe = regexp.MustCompile(`phlink: (Q busy|rx 'Q')`)
)

func (d *Driver) snapshot(ctx context.Context, l device.Link) (*snapshot, error) {
	frames, stop := l.Frames()
	defer stop()
	m := l.Mark()
	if err := l.Write(ctx, cmd9('Q', 0, 0)); err != nil {
		return nil, err
	}
	errc := make(chan string, 1)
	ec, ecancel := context.WithCancel(ctx)
	defer ecancel()
	go func() {
		if r, err := l.WaitLine(ec, m, reQErrRe); err == nil {
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
			if e == "rx 'Q'" {
				return nil, fmt.Errorf("the running image has no profiler (no 'profile' in its caps)")
			}
			return nil, fmt.Errorf("phone: %s", e)
		case <-idle.C:
			return nil, fmt.Errorf("profile snapshot stalled")
		case f := <-frames:
			idle.Reset(10 * time.Second)
			if !f.SumOK {
				return nil, fmt.Errorf("profile frame %d: checksum mismatch", f.Seq)
			}
			switch f.Type {
			case frameBlobData:
				buf.Write(f.Payload)
			case frameBlobEnd:
				if len(f.Payload) < 8 || binary.LittleEndian.Uint32(f.Payload) != blobProfile {
					continue
				}
				if n := int(binary.LittleEndian.Uint32(f.Payload[4:])); n != buf.Len() {
					return nil, fmt.Errorf("profile: got %d of %d bytes", buf.Len(), n)
				}
				s, err := parseSnapshot(buf.Bytes())
				if s != nil {
					s.At = time.Now()
				}
				return s, err
			}
		}
	}
}

type ProfThread struct {
	ID      uint64  `json:"id"`
	Process string  `json:"process"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu"` // % of one core over the window
}

type ProfProcess struct {
	ID      uint64  `json:"id"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu"` // % of one core (sum of its threads)
	RAM     uint64  `json:"ram"` // bytes committed in the chunks it owns
	Threads int     `json:"threads"`
	Chunks  int     `json:"chunks"`
}

type Profile struct {
	WindowS   float64       `json:"window_s"`
	Cores     int           `json:"cores"`
	CoreBusy  []float64     `json:"core_busy"` // % per core
	CPUTotal  float64       `json:"cpu_total"` // % of all cores
	FreeRAM   uint64        `json:"free_ram"`
	KHeap     uint64        `json:"kernel_heap"`
	GlobalRAM uint64        `json:"global_ram"` // chunks with no owning process (shared, kernel)
	Processes []ProfProcess `json:"processes"`
	Threads   []ProfThread  `json:"threads"`
	Bytes     int           `json:"snapshot_bytes"`
}

type profState struct {
	mu   sync.Mutex
	last *snapshot
}

var profs sync.Map // device link identity is not exposed; one phone per driver instance is the norm

func (d *Driver) profile(ctx context.Context, l device.Link, top int, window time.Duration) (Profile, error) {
	st, _ := profs.LoadOrStore(d, &profState{})
	ps := st.(*profState)
	ps.mu.Lock()
	prev := ps.last
	ps.mu.Unlock()
	if prev == nil || time.Since(prev.At) > 2*time.Minute {
		var err error
		if prev, err = d.snapshot(ctx, l); err != nil {
			return Profile{}, err
		}
		if err := sleep(ctx, window); err != nil {
			return Profile{}, err
		}
	}
	cur, err := d.snapshot(ctx, l)
	if err != nil {
		return Profile{}, err
	}
	ps.mu.Lock()
	ps.last = cur
	ps.mu.Unlock()
	return diffProfile(prev, cur, top), nil
}

func diffProfile(a, b *snapshot, top int) Profile {
	p := Profile{Cores: b.Cores, FreeRAM: b.FreeKB << 10, KHeap: b.KHeapKB << 10, Bytes: b.RawBytes}
	dt := float64(b.Now - a.Now)
	if b.Now <= a.Now || dt == 0 {
		dt = 1
	}
	p.WindowS = dt / float64(b.Hz)
	pct := func(d uint64) float64 { return float64(int64(d*10000/uint64(dt))) / 100 }
	sumBusy := 0.0
	for c := 0; c < b.Cores; c++ {
		di := b.Idle[c] - a.Idle[c]
		busy := 100 - pct(di)
		if busy < 0 {
			busy = 0
		}
		p.CoreBusy = append(p.CoreBusy, busy)
		sumBusy += busy
	}
	if b.Cores > 0 {
		p.CPUTotal = float64(int(sumBusy/float64(b.Cores)*100)) / 100
	}
	procs := map[uint64]*ProfProcess{}
	get := func(id uint64) *ProfProcess {
		if pp := procs[id]; pp != nil {
			return pp
		}
		pp := &ProfProcess{ID: id, Name: shortName(b.Procs[id])}
		procs[id] = pp
		return pp
	}
	for id, t := range b.Threads {
		var d uint64
		if o, ok := a.Threads[id]; ok && t.CPU >= o.CPU {
			d = t.CPU - o.CPU
		}
		// a thread can't use more than the window: the phone adds a running thread's current stretch from
		// iLastStartTime, which is stale while a core is mid-switch -> deltas of 50x the window (seen 2026-10-08)
		if d > uint64(dt) {
			d = uint64(dt)
		}
		if t.Name == "" {
			continue
		}
		c := pct(d)
		pp := get(t.PID)
		pp.Threads++
		if strings.HasPrefix(t.Name, "Null") && strings.HasPrefix(pp.Name, "ekern") {
			continue // the idle threads: they are core_busy's complement, not CPU use
		}
		pp.CPU += c
		p.Threads = append(p.Threads, ProfThread{ID: id, Process: pp.Name, Name: t.Name, CPU: c})
	}
	for _, c := range b.Chunks {
		if c.PID == 0 {
			p.GlobalRAM += c.Size
			continue
		}
		pp := get(c.PID)
		pp.RAM += c.Size
		pp.Chunks++
	}
	for _, pp := range procs {
		pp.CPU = float64(int(pp.CPU*100)) / 100
		p.Processes = append(p.Processes, *pp)
	}
	sort.Slice(p.Threads, func(i, j int) bool { return p.Threads[i].CPU > p.Threads[j].CPU })
	sort.Slice(p.Processes, func(i, j int) bool {
		if p.Processes[i].CPU != p.Processes[j].CPU {
			return p.Processes[i].CPU > p.Processes[j].CPU
		}
		return p.Processes[i].RAM > p.Processes[j].RAM
	})
	if top > 0 && len(p.Threads) > top {
		p.Threads = p.Threads[:top]
	}
	return p
}
