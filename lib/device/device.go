// Package device is the multi-phone seam. A Driver knows one family of devices: the operations it supports
// (described as data, so the REST API, the MCP server and the web UI are all generated from the same list),
// how to run them over a Link, and how to turn its log lines into structured events. Adding a phone = adding
// a driver package that calls Register; nothing in core/api/mcp changes.
package device

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"sync"
)

// Param describes one argument of an operation. Type is "string", "uint32", "int", "bool", "blob" (the
// sha256 of a stored blob, e.g. an uploaded image) or "file" (a path on the server's disk).
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// OpSpec describes an operation. Capability groups ops for the UI ("slots", "memory", "input", ...).
type OpSpec struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Capability  string  `json:"capability"`
	Params      []Param `json:"params"`
	// Reboots: the op ends with the device resetting (the Session expects the disconnect).
	Reboots bool `json:"reboots"`
}

// Event is a structured fact parsed from the log or produced by an op.
type Event struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data,omitempty"`
}

// Mark is a position in the device's line stream (a line sequence number).
type Mark uint64

// Link is what a driver gets to talk to its device while running an op. The Session holds the port and
// the op lock; the driver never sees the raw transport.
type Link interface {
	Write(ctx context.Context, b []byte) error
	// Mark returns the position after the newest line received so far.
	Mark() Mark
	// WaitLine waits for a line after m matching re and returns it with its submatches.
	WaitLine(ctx context.Context, m Mark, re *regexp.Regexp) ([]string, error)
	// LinesSince returns up to max lines received after m.
	LinesSince(m Mark, max int) []string
	// Blob opens a stored blob by sha256.
	Blob(sha string) (io.ReadCloser, int64, error)
	// Progress reports op progress to subscribers (done/total in the op's own units).
	Progress(stage string, done, total int64)
	// Emit records an event (stored + broadcast).
	Emit(ev Event)
	// Frames subscribes to binary frames from the device (see Frame); call the returned func to stop.
	// Subscribe BEFORE sending the command that produces them.
	Frames() (<-chan Frame, func())
	// PutBlob stores data (a screenshot, a dump) in the history and returns its sha256.
	PutBlob(kind, name string, meta any, data []byte) (string, error)
}

// Frame is a binary record cut out of the device's byte stream. The core understands one generic framing,
// "phonelab frame v1": a 16-byte header {A5 5A 'P' 'F', type u8, flags u8, seq LE16, len LE32, sum LE32 (byte sum
// of the payload)} then len payload bytes. Text logs never contain the magic, so frames can sit between log chunks.
// Type numbers are the driver's business.
type Frame struct {
	Type    byte
	Seq     uint16
	Payload []byte
	SumOK   bool
}

// Driver is one device family.
type Driver interface {
	Name() string
	Ops() []OpSpec
	Exec(ctx context.Context, l Link, op string, args Args) (any, error)
	// BootStart reports whether this log line is the first line of a new boot.
	BootStart(line string) bool
	// Classify turns a log line into zero or more events.
	Classify(line string) []Event
}

// Factory builds a driver from its config section.
type Factory func(cfg map[string]any) (Driver, error)

var (
	regMu sync.Mutex
	reg   = map[string]Factory{}
)

func Register(name string, f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[name] = f
}

func New(name string, cfg map[string]any) (Driver, error) {
	regMu.Lock()
	f := reg[name]
	regMu.Unlock()
	if f == nil {
		return nil, fmt.Errorf("unknown driver %q (have %v)", name, Drivers())
	}
	return f(cfg)
}

func Drivers() []string {
	regMu.Lock()
	defer regMu.Unlock()
	var n []string
	for k := range reg {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// FindOp returns the spec of op, or nil.
func FindOp(d Driver, op string) *OpSpec {
	for _, s := range d.Ops() {
		if s.Name == op {
			s := s
			return &s
		}
	}
	return nil
}
