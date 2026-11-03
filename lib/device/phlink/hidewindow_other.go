// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package phlink

import "os/exec"

func hideWindow(*exec.Cmd) {}
