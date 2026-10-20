// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package mock is a fake PinePhone speaking phlink (phlink.cpp in adaptation-a64 (usbcc/)), for unit tests and
// for running the service + web UI without hardware. It replays a recorded boot transcript (testdata/) on every
// "boot", answers U/R/B/P/W the way the phone does, and drops off the bus on reboot like the real CDC device.
package mock

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/transport"
)

type Phone struct {
	mu        sync.Mutex
	boot      []string // transcript lines replayed on each boot
	LineDelay time.Duration
	RebootGap time.Duration // how long the port is gone during a reboot

	Mem     map[uint32]uint32
	SlotB   []byte
	Armed   bool
	Boots   int
	FailSum bool // make the next upload fail its check

	Proto1  bool     // behave like an image without protocol v2 (no V/S/K/T)
	Screen  []uint32 // XRGB8888 framebuffer served by 'S'
	ScreenW int
	ScreenH int
	Input   []string // injected K/T commands, "K a6 0"
	NoLive  bool     // behave like a protocol 2 image: no live view (L is echoed)
	Live    LiveRef  // the live view reference (the picture last sent) and counters
	profN   int      // profile snapshots taken

	cur     *port
	offline bool
	slot    string
}

// New makes a phone that replays lines on boot.
func New(boot []string) *Phone {
	ph := &Phone{boot: boot, Mem: map[uint32]uint32{}, LineDelay: 0, RebootGap: 50 * time.Millisecond, slot: "A",
		ScreenW: 360, ScreenH: 640}
	ph.Screen = TestPattern(ph.ScreenW, ph.ScreenH)
	return ph
}

// Composite fakes phlink's which=2: a dark top bar (40 rows), the 360x640 OS picture, the soft key bar (40 rows)
// with green | menu | red blocks where a64_display.cpp draws them.
func Composite(os []uint32) []uint32 {
	fb := make([]uint32, 360*720)
	for y := 0; y < 720; y++ {
		for x := 0; x < 360; x++ {
			var c uint32 = 0xff101418
			switch {
			case y >= 40 && y < 680:
				c = os[(y-40)*360+x]
			case y >= 680:
				c = 0xff000000
				if y >= 690 && y < 710 {
					switch {
					case x >= 22 && x < 98:
						c = 0xff1fa83a
					case x >= 165 && x < 195:
						c = 0xffffffff
					case x >= 262 && x < 338:
						c = 0xffd62b2b
					}
				}
			}
			fb[y*360+x] = c
		}
	}
	return fb
}

// TestPattern: flat bands (long runs), a gradient strip (literals) and a checker (worst case).
func TestPattern(w, h int) []uint32 {
	fb := make([]uint32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var c uint32
			switch {
			case y < h/3:
				c = []uint32{0xff0000, 0x00ff00, 0x0000ff, 0xffffff}[x*4/w]
			case y < 2*h/3:
				c = uint32(x*255/w)<<16 | uint32(y&0xff)<<8 | 0x40
			default:
				if (x+y)&1 == 0 {
					c = 0x123456
				} else {
					c = 0xfedcba
				}
			}
			fb[y*w+x] = 0xff000000 | c
		}
	}
	return fb
}

// LoadTranscript reads a recorded log (one line per line; capped at max lines, 0 = all).
func LoadTranscript(path string, max int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, strings.TrimRight(sc.Text(), "\r"))
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out, sc.Err()
}

// Opener returns a transport.Opener for this phone.
func (ph *Phone) Opener() transport.Opener { return opener{ph} }

type opener struct{ ph *Phone }

func (o opener) Describe() string { return "mock" }

func (o opener) Open(ctx context.Context) (transport.Port, error) {
	ph := o.ph
	ph.mu.Lock()
	defer ph.mu.Unlock()
	if ph.offline {
		return nil, transport.ErrNotFound
	}
	if ph.cur != nil && !ph.cur.closed {
		return nil, fmt.Errorf("mock: port already open (access denied)")
	}
	p := &port{ph: ph, data: make(chan struct{}, 1)}
	ph.cur = p
	if ph.Boots == 0 {
		ph.Boots = 1
		go ph.replay(p, "A")
	}
	return p, nil
}

// Unplug simulates pulling the cable (the port disappears until Plug).
func (ph *Phone) Unplug() {
	ph.mu.Lock()
	ph.offline = true
	p := ph.cur
	ph.mu.Unlock()
	if p != nil {
		p.vanish()
	}
}

func (ph *Phone) Plug() {
	ph.mu.Lock()
	ph.offline = false
	ph.mu.Unlock()
}

func (ph *Phone) Slot() string {
	ph.mu.Lock()
	defer ph.mu.Unlock()
	return ph.slot
}

// reboot: the port vanishes, comes back, the boot transcript plays.
func (ph *Phone) reboot() {
	ph.mu.Lock()
	slot := "A"
	if ph.Armed && ph.SlotB != nil {
		slot = "B"
		ph.Armed = false // one-shot
	}
	ph.slot = slot
	ph.offline = true
	p := ph.cur
	ph.Boots++
	gap := ph.RebootGap
	ph.mu.Unlock()
	if p != nil {
		p.vanish()
	}
	go func() {
		time.Sleep(gap)
		ph.mu.Lock()
		ph.offline = false
		ph.mu.Unlock()
		// the phone streams whether or not the host has reopened yet; the real one buffers 256 KB
		for i := 0; i < 400; i++ {
			ph.mu.Lock()
			q := ph.cur
			ph.mu.Unlock()
			if q != nil && q != p && !q.closed {
				ph.replay(q, slot)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func (ph *Phone) replay(p *port, slot string) {
	slotLine := "[EKERN][slot] running slot " + slot + " (mock)"
	sawSlot := false
	defer func() {
		if !sawSlot { // a short transcript still reports its slot
			p.emit(slotLine + "\n")
		}
	}()
	for _, l := range ph.boot {
		if strings.Contains(l, "[EKERN][slot] running slot") {
			l, sawSlot = slotLine, true
		}
		if p.emit(l + "\n") {
			return
		}
		if ph.LineDelay > 0 {
			time.Sleep(ph.LineDelay)
		}
	}
}

// Say emits a line now (tests use it to inject log output).
func (ph *Phone) Say(line string) {
	ph.mu.Lock()
	p := ph.cur
	ph.mu.Unlock()
	if p != nil {
		p.emit(line + "\n")
	}
}

type port struct {
	ph     *Phone
	mu     sync.Mutex
	out    bytes.Buffer
	data   chan struct{}
	closed bool
	gone   bool

	// host -> phone command state
	in     []byte
	upLeft int
	upLen  uint32
	upSum  uint32
	upBuf  []byte
}

func (p *port) Name() string { return "mock" }

func (p *port) Alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.gone
}

// emit queues phone output; returns true if the port is dead.
func (p *port) emit(s string) bool {
	p.mu.Lock()
	if p.closed || p.gone {
		p.mu.Unlock()
		return true
	}
	p.out.WriteString(s)
	p.mu.Unlock()
	select {
	case p.data <- struct{}{}:
	default:
	}
	return false
}

func (p *port) vanish() {
	p.mu.Lock()
	p.gone = true
	p.mu.Unlock()
	select {
	case p.data <- struct{}{}:
	default:
	}
}

func (p *port) Read(b []byte) (int, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return 0, io.ErrClosedPipe
		}
		if p.out.Len() > 0 {
			n, _ := p.out.Read(b)
			p.mu.Unlock()
			return n, nil
		}
		if p.gone {
			p.mu.Unlock()
			return 0, io.EOF
		}
		p.mu.Unlock()
		select {
		case <-p.data:
		case <-time.After(50 * time.Millisecond):
			return 0, nil // read timeout, like the serial port
		}
	}
}

func (p *port) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	select {
	case p.data <- struct{}{}:
	default:
	}
	return nil
}

func (p *port) Write(b []byte) (int, error) {
	p.mu.Lock()
	if p.closed || p.gone {
		p.mu.Unlock()
		return 0, fmt.Errorf("mock: The port is closed")
	}
	p.mu.Unlock()
	for i := 0; i < len(b); {
		if p.upLeft > 0 {
			n := min(p.upLeft, len(b)-i)
			p.upBuf = append(p.upBuf, b[i:i+n]...)
			p.upLeft -= n
			i += n
			if p.upLeft == 0 {
				p.uploadDone()
			}
			continue
		}
		p.in = append(p.in, b[i])
		i++
		p.command()
	}
	return len(b), nil
}

func (p *port) command() {
	ph := p.ph
	c := p.in[0]
	switch c {
	case 'R':
		p.in = p.in[:0]
		p.emit("phlink: reboot requested\n")
		go ph.reboot()
	case 'B':
		p.in = p.in[:0]
		ph.mu.Lock()
		ok := ph.SlotB != nil
		if ok {
			ph.Armed = true
		}
		ph.mu.Unlock()
		if ok {
			p.emit("phlink: slot B re-armed r=00000000\n")
			go ph.reboot()
		} else {
			p.emit("phlink: slot B re-armed r=fffffffe\n")
		}
	case 'V':
		p.in = p.in[:0]
		ph.mu.Lock()
		v1 := ph.Proto1
		ph.mu.Unlock()
		if v1 {
			p.emit("phlink: rx 'V'\n")
		} else {
			ph.mu.Lock()
			nl := ph.NoLive
			ph.mu.Unlock()
			if nl {
				p.emit("phlink: V proto=00000002 caps=shot,key,touch,panel,composite,profile,hotspots\n")
			} else {
				p.emit("phlink: V proto=00000003 caps=shot,live,key,touch,panel,composite,profile,hotspots\n")
			}
		}
	case 'Q':
		if len(p.in) < 9 {
			return
		}
		p.in = p.in[:0]
		ph.mu.Lock()
		ph.profN++
		n := uint64(ph.profN)
		v1 := ph.Proto1
		ph.mu.Unlock()
		if v1 {
			p.emit("phlink: rx 'Q'\n")
			return
		}
		go p.sendBlob(1, []byte(ProfileText(n)))
	case 'H':
		if len(p.in) < 9 {
			return
		}
		start := binary.LittleEndian.Uint32(p.in[1:]) != 0
		p.in = p.in[:0]
		if start {
			p.emit("phlink: H started\n")
			return
		}
		go p.sendBlob(2, []byte(HotText))
	case 'S', 'K', 'T', 'L':
		if len(p.in) < 9 {
			return
		}
		a := binary.LittleEndian.Uint32(p.in[1:])
		v := binary.LittleEndian.Uint32(p.in[5:])
		p.in = p.in[:0]
		ph.mu.Lock()
		v1 := ph.Proto1 || (c == 'L' && ph.NoLive)
		if !v1 && c != 'L' {
			ph.Input = append(ph.Input, fmt.Sprintf("%c %x %d", c, a, v))
		}
		fb, w, h := ph.Screen, ph.ScreenW, ph.ScreenH
		if (c == 'S' || c == 'L') && a == 2 && w == 360 && h == 640 {
			fb, w, h = Composite(fb), 360, 720 // the whole screen at half panel size: bars + the OS picture
		}
		var lf [][]byte
		if c == 'L' && !v1 {
			lf = ph.Live.LiveFrames(append([]uint32(nil), fb...), w, h, a, 1, v&1 != 0)
		}
		ph.mu.Unlock()
		if v1 {
			p.emit(fmt.Sprintf("phlink: rx '%c'\n", c))
			return
		}
		switch c {
		case 'L':
			go func() {
				for _, f := range lf {
					if p.emit(string(f)) {
						return
					}
				}
			}()
		case 'S':
			p.emit(fmt.Sprintf("phlink: S id=00000001 size=%08x\n", uint32(w)<<16|uint32(h)))
			go p.sendShot(fb, w, h)
		case 'K':
			p.emit(fmt.Sprintf("phlink: K %08x %s\n", a, map[bool]string{false: "down", true: "up"}[v != 0]))
		case 'T':
			if v != 1 {
				p.emit(fmt.Sprintf("phlink: T %08x %s\n", a, map[bool]string{false: "down", true: "up"}[v != 0]))
			}
		}
	case 'U', 'P', 'W':
		if len(p.in) < 9 {
			return
		}
		a := binary.LittleEndian.Uint32(p.in[1:])
		v := binary.LittleEndian.Uint32(p.in[5:])
		p.in = p.in[:0]
		switch c {
		case 'U':
			p.upLen, p.upSum, p.upLeft, p.upBuf = a, v, int(a), nil
			p.emit(fmt.Sprintf("phlink: upload begin len=%08x r=00000000\n", a))
		case 'P':
			n := min(max(v, 1), 64)
			a &^= 3
			ph.mu.Lock()
			var sb strings.Builder
			for i := uint32(0); i < n; i++ {
				fmt.Fprintf(&sb, "phlink: P %08x = %08x\n", a+i*4, ph.Mem[a+i*4])
			}
			ph.mu.Unlock()
			p.emit(sb.String())
		case 'W':
			ph.mu.Lock()
			ph.Mem[a&^3] = v
			ph.mu.Unlock()
			p.emit(fmt.Sprintf("phlink: W %08x = %08x\n", a&^3, v))
		}
	default:
		p.in = p.in[:0]
		if c >= 0x20 && c < 0x7f {
			p.emit(fmt.Sprintf("phlink: rx '%c'\n", c))
		}
	}
}

func (p *port) uploadDone() {
	ph := p.ph
	// the phone sums the slot read back from SD, zero-padded to a sector
	buf := p.upBuf
	if r := len(buf) % 512; r != 0 {
		buf = append(buf, make([]byte, 512-r)...)
	}
	var sum uint32
	for i := 0; i+4 <= len(buf); i += 4 {
		sum += binary.LittleEndian.Uint32(buf[i:])
	}
	ph.mu.Lock()
	fail := ph.FailSum
	ph.FailSum = false
	ph.mu.Unlock()
	if fail || sum != p.upSum {
		p.emit(fmt.Sprintf("phlink: UPLOAD FAILED len=%08x sum=%08x\n", p.upLen, sum))
		return
	}
	ph.mu.Lock()
	ph.SlotB = p.upBuf
	ph.Armed = true
	ph.mu.Unlock()
	p.emit(fmt.Sprintf("phlink: UPLOAD OK len=%08x sum=%08x\n", p.upLen, sum))
}
