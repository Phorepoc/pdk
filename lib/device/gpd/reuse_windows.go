// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package gpd

import "syscall"

// setReuse lets rigctl.py (or a second PhoneLab) share the log port.
func setReuse(network, address string, c syscall.RawConn) error {
	return c.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
}
