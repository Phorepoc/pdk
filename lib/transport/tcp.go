// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package transport

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// TCPOpener links to a device over TCP. Dial connects out to an agent listening on the phone ("192.168.1.20:7654");
// Listen waits for the phone to connect in ("0.0.0.0:9999"), which is how phone-side scripts behind NAT/Wi-Fi
// usually work. Exactly one of the two is set.
type TCPOpener struct {
	Dial   string
	Listen string

	mu sync.Mutex
	ln net.Listener
}

func (o *TCPOpener) Describe() string {
	if o.Listen != "" {
		return "tcp listen " + o.Listen
	}
	return "tcp " + o.Dial
}

func (o *TCPOpener) Open(ctx context.Context) (Port, error) {
	if o.Listen == "" {
		d := net.Dialer{Timeout: 3 * time.Second}
		c, err := d.DialContext(ctx, "tcp", o.Dial)
		if err != nil {
			return nil, ErrNotFound // agent not running (yet): retry quietly
		}
		return &tcpPort{c: c}, nil
	}
	o.mu.Lock()
	if o.ln == nil {
		ln, err := net.Listen("tcp", o.Listen)
		if err != nil {
			o.mu.Unlock()
			return nil, err
		}
		o.ln = ln
	}
	ln := o.ln
	o.mu.Unlock()
	// Accept with a deadline so a cancelled context and the Session's retry loop both stay responsive.
	if tl, ok := ln.(*net.TCPListener); ok {
		_ = tl.SetDeadline(time.Now().Add(2 * time.Second))
	}
	c, err := ln.Accept()
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &tcpPort{c: c}, nil
}

type tcpPort struct{ c net.Conn }

func (p *tcpPort) Name() string { return p.c.RemoteAddr().String() }

// Read maps a read timeout to (0, nil) like the serial port, so the Session's loop can poll.
func (p *tcpPort) Read(b []byte) (int, error) {
	_ = p.c.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	n, err := p.c.Read(b)
	var ne net.Error
	if err != nil && errors.As(err, &ne) && ne.Timeout() {
		return n, nil
	}
	return n, err
}
func (p *tcpPort) Write(b []byte) (int, error) { return p.c.Write(b) }
func (p *tcpPort) Close() error                { return p.c.Close() }
func (p *tcpPort) Alive() bool                 { return true } // a dead socket ends Read with an error
