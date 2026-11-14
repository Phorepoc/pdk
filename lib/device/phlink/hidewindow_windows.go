// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

package phlink

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console program started from the windowless service from opening a console window.
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
