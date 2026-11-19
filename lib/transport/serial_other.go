// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package transport

import "go.bug.st/serial"

// cancelIO: on Linux/macOS closing the descriptor wakes a blocked write (EBADF/EIO); a purge drops the queued bytes first.
func cancelIO(sp serial.Port) {
	_ = sp.ResetOutputBuffer()
}
