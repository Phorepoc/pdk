#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# layering.sh [<repository>] -- fail if the repository names a layer above it (docs/layering.md). The repository's
# .layering file lists what it must not name: one extended regular expression per line (case-insensitive), # comments.
# Lines of tracked files matching one are reported; a line may carry "layering: ok" to accept a mention on purpose.
set -eu
repo="${1:-.}"
deny="$repo/.layering"
[ -f "$deny" ] || { echo "layering: no $deny, nothing to check"; exit 0; }
pat=$(grep -v '^[[:space:]]*\(#\|$\)' "$deny" | paste -sd'|' -)
[ -n "$pat" ] || exit 0
hits=$(git -C "$repo" grep -nIiE "$pat" -- . ':!.layering' | grep -v 'layering: ok' || true)
if [ -n "$hits" ]; then
	echo "$hits" | head -100
	echo "layering: $(echo "$hits" | wc -l) line(s) name a layer above this repository (see the pdk's docs/layering.md)" >&2
	exit 1
fi
echo "layering: clean"
