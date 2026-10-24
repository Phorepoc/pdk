// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Package webui carries the built web UI (cd web && npm run build writes dist/ here) into the phonelab binary.
package webui

import "embed"

//go:embed all:dist
var Dist embed.FS
