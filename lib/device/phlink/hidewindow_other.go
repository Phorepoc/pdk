// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package phlink

import "os/exec"

func hideWindow(*exec.Cmd) {}
