// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package gpd

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Phorepoc/pdk/lib/transport"
)

// The rig's debug link (adaptation-x32 ekern/xk_netlog_proto.h; reference tool adaptation-pc tools/gpd-rig/rigctl.py):
//
//	device -> lab   UDP broadcast to :6666   "PHL1" | boot id u32 | seq u32 | log bytes
//	lab -> device   UDP to :6667             "PHC1" | token u32 | cmd id u32 | op u8 | args
//	device -> lab   reply to the sender      "PHR1" | cmd id u32 | op u8 | status i32 | data
//	                screenshot stream        "PHF1" | cmd id u32 | w u16 | h u16 | first px u32 | count u16 | XRGB px (count 0 = end)
//
// The Opener turns that into what the core Session understands: a byte stream. Log bytes pass through as text; a new
// boot id, a sequence gap and an unexpected reset become marker lines (classified by the driver); replies and screen
// packets are wrapped as "phonelab frame v1" frames (types frameReply / frameScreen) for the driver's Frames().

const (
	logMagic    = "PHL1"
	cmdMagic    = "PHC1"
	replyMagic  = "PHR1"
	screenMagic = "PHF1"

	frameReply  byte = 1
	frameScreen byte = 2
)

var frameHeadMagic = []byte{0xA5, 0x5A, 'P', 'F'}

// wrapFrame builds a phonelab frame v1 around payload.
func wrapFrame(typ byte, seq uint16, payload []byte) []byte {
	b := make([]byte, 16+len(payload))
	copy(b, frameHeadMagic)
	b[4] = typ
	binary.LittleEndian.PutUint16(b[6:], seq)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(payload)))
	var sum uint32
	for _, c := range payload {
		sum += uint32(c)
	}
	binary.LittleEndian.PutUint32(b[12:], sum)
	copy(b[16:], payload)
	return b
}

// cmdPacket builds one PHC1 command.
func cmdPacket(token, id uint32, op byte, args []byte) []byte {
	b := make([]byte, 0, 13+len(args))
	b = append(b, cmdMagic...)
	b = binary.LittleEndian.AppendUint32(b, token)
	b = binary.LittleEndian.AppendUint32(b, id)
	b = append(b, op)
	return append(b, args...)
}

// Opener owns the UDP socket for the life of the service (a link flap must not lose the boot/sequence state).
type Opener struct {
	Listen     string        // log + reply socket, default 0.0.0.0:6666
	CmdPort    int           // default 6667
	To         string        // where commands go (default the limited broadcast, as rigctl does)
	IP         string        // accept packets only from this source address (empty = any)
	LinkWindow time.Duration // the link is up while a packet arrived within this (default 20 s)
	WDMin      time.Duration // an unexpected reset with at least this much silence is a watchdog reset (default 15 s)
	WDMax      time.Duration // ... and at most this (default 5 min)

	mu          sync.Mutex
	conn        *net.UDPConn
	queue       [][]byte
	boot        uint32
	haveBoot    bool
	seq         uint32
	lastRx      time.Time // any packet
	lastLog     time.Time // a log packet
	rigIP       string
	expectReset bool
	frameSeq    uint16
}

func (o *Opener) Describe() string { return "udp listen " + o.listen() }

func (o *Opener) listen() string {
	if o.Listen == "" {
		return "0.0.0.0:6666"
	}
	return o.Listen
}

// LocalAddr is the bound log socket (tests listen on port 0).
func (o *Opener) LocalAddr() net.Addr {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.conn == nil {
		return nil
	}
	return o.conn.LocalAddr()
}

// RigIP is the source address of the newest log packet.
func (o *Opener) RigIP() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.rigIP
}

// LastLog is when the newest log packet arrived (zero = none yet).
func (o *Opener) LastLog() time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastLog
}

func (o *Opener) window() time.Duration {
	if o.LinkWindow > 0 {
		return o.LinkWindow
	}
	return 20 * time.Second
}

func (o *Opener) bind() error {
	if o.conn != nil {
		return nil
	}
	lc := net.ListenConfig{Control: setReuse}
	pc, err := lc.ListenPacket(context.Background(), "udp4", o.listen())
	if err != nil {
		return fmt.Errorf("gpd: listen %s: %w", o.listen(), err)
	}
	c := pc.(*net.UDPConn)
	_ = c.SetReadBuffer(8 << 20)
	o.conn = c
	return nil
}

// Open succeeds only while the rig is talking: it polls the socket itself so a first packet after a long silence is seen.
func (o *Opener) Open(ctx context.Context) (transport.Port, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.bind(); err != nil {
		return nil, err
	}
	if len(o.queue) == 0 {
		o.pollLocked(250 * time.Millisecond)
	}
	if o.lastRx.IsZero() || time.Since(o.lastRx) > o.window() {
		return nil, transport.ErrNotFound // the rig is not on the link (Manjaro, hung, off): retry quietly
	}
	return &port{o: o}, nil
}

// Close releases the socket (tests).
func (o *Opener) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.conn != nil {
		o.conn.Close()
		o.conn = nil
	}
}

// pollLocked reads at most one datagram within d and queues what the Session should see. The lock is released while
// blocked, so commands can go out meanwhile.
func (o *Opener) pollLocked(d time.Duration) {
	c := o.conn
	if c == nil {
		return
	}
	buf := make([]byte, 4096)
	_ = c.SetReadDeadline(time.Now().Add(d))
	o.mu.Unlock()
	n, from, err := c.ReadFromUDP(buf)
	o.mu.Lock()
	if err != nil || n == 0 {
		return
	}
	if o.IP != "" && from.IP.String() != o.IP {
		return
	}
	o.handleLocked(buf[:n], from, time.Now())
}

func (o *Opener) handleLocked(d []byte, from *net.UDPAddr, now time.Time) {
	switch {
	case len(d) >= 12 && string(d[:4]) == logMagic:
		boot := binary.LittleEndian.Uint32(d[4:])
		seq := binary.LittleEndian.Uint32(d[8:])
		var out bytes.Buffer
		if !o.haveBoot || boot != o.boot {
			if o.haveBoot {
				gap := now.Sub(o.lastLog).Round(time.Second)
				switch {
				case o.expectReset:
					fmt.Fprintf(&out, "\n===== rig: commanded reset =====\n")
				case gap >= o.wdMin() && gap <= o.wdMax():
					fmt.Fprintf(&out, "\n===== rig: watchdog reset after %ds of silence =====\n", int(gap.Seconds()))
				default:
					fmt.Fprintf(&out, "\n===== rig: unexpected reset after %ds of silence =====\n", int(gap.Seconds()))
				}
			}
			fmt.Fprintf(&out, "\n===== rig: boot %08x from %s =====\n", boot, from.IP)
			o.boot, o.haveBoot, o.seq, o.expectReset = boot, true, seq-1, false
		}
		if lost := seq - o.seq - 1; lost != 0 {
			if lost >= 1<<31 {
				return // a duplicate or a stale packet
			}
			fmt.Fprintf(&out, "\n===== rig: %d packets lost =====\n", lost)
		}
		o.seq = seq
		o.lastRx, o.lastLog, o.rigIP = now, now, from.IP.String()
		out.Write(d[12:])
		o.queue = append(o.queue, out.Bytes())
	case len(d) >= 13 && string(d[:4]) == replyMagic:
		o.lastRx = now
		o.frameSeq++
		o.queue = append(o.queue, wrapFrame(frameReply, o.frameSeq, d))
	case len(d) >= 18 && string(d[:4]) == screenMagic:
		o.lastRx = now
		o.frameSeq++
		o.queue = append(o.queue, wrapFrame(frameScreen, o.frameSeq, d))
	}
}

func (o *Opener) wdMin() time.Duration {
	if o.WDMin > 0 {
		return o.WDMin
	}
	return 15 * time.Second
}

func (o *Opener) wdMax() time.Duration {
	if o.WDMax > 0 {
		return o.WDMax
	}
	return 5 * time.Minute
}

// send transmits one command datagram.
func (o *Opener) send(b []byte) error {
	o.mu.Lock()
	c := o.conn
	to := o.To
	if len(b) >= 13 && string(b[:4]) == cmdMagic && b[12] == 'R' {
		o.expectReset = true // a reset we asked for is not a fault
	}
	o.mu.Unlock()
	if c == nil {
		return errors.New("gpd: socket closed")
	}
	if to == "" {
		to = "255.255.255.255"
	}
	port := o.CmdPort
	if port == 0 {
		port = 6667
	}
	ip := net.ParseIP(to)
	if ip == nil {
		return fmt.Errorf("gpd: bad command address %q", to)
	}
	_, err := c.WriteToUDP(b, &net.UDPAddr{IP: ip, Port: port})
	return err
}

type port struct {
	o      *Opener
	closed bool
}

func (p *port) Name() string {
	if ip := p.o.RigIP(); ip != "" {
		return "udp " + p.o.listen() + " <- " + ip
	}
	return "udp " + p.o.listen()
}

func (p *port) Read(b []byte) (int, error) {
	o := p.o
	o.mu.Lock()
	defer o.mu.Unlock()
	if p.closed || o.conn == nil {
		return 0, errors.New("gpd: port closed")
	}
	if len(o.queue) == 0 {
		o.pollLocked(250 * time.Millisecond)
	}
	if len(o.queue) == 0 {
		return 0, nil
	}
	c := o.queue[0]
	n := copy(b, c)
	if n < len(c) {
		o.queue[0] = c[n:]
	} else {
		o.queue = o.queue[1:]
	}
	return n, nil
}

func (p *port) Write(b []byte) (int, error) {
	if p.closed {
		return 0, errors.New("gpd: port closed")
	}
	if err := p.o.send(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// Close ends this Port only; the socket (and the boot/sequence state) stays with the Opener.
func (p *port) Close() error { p.closed = true; return nil }
func (p *port) Alive() bool  { return true }
