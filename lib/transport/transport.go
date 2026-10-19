// Package transport opens the byte pipe to a device. A Device driver never opens ports itself: the core
// Session owns exactly one Port per device, so nothing else can grab it (the reason PhoneLab exists).
package transport

import (
	"context"
	"errors"
	"io"
)

// Port is an open link to a device.
type Port interface {
	io.ReadWriteCloser
	// Name is a human label ("COM5", "/dev/ttyACM0", "mock").
	Name() string
	// Alive reports whether the underlying device still exists. Some drivers (Windows usbser) keep a read
	// blocked forever after the phone resets; the Session polls this and closes the port to unblock it.
	Alive() bool
}

// Opener finds and opens the device. It is called again after every disconnect.
type Opener interface {
	Open(ctx context.Context) (Port, error)
	// Describe is shown in health output ("serial vid=0E22 pid=0666").
	Describe() string
}

// ErrNotFound means the device is not attached (yet); the Session retries quietly.
var ErrNotFound = errors.New("device not found")
