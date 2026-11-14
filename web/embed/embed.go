// Copyright The Phorepoc Authors
// SPDX-License-Identifier: AGPL-3.0-only

// Package webui carries the built web UI (cd web && npm run build writes dist/ here) into the phonelab binary.
package webui

import "embed"

//go:embed all:dist
var Dist embed.FS
