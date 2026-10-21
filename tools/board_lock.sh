#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# board_lock.sh <board> [<layer>...] -- write <device repo>/board.lock: the commit of every repository the board's image
# is built from (the pdk, core, the board's adaptations and the layers; default layer: tls), taken from the checkouts
# next to this pdk. The release workflow (.github/workflows/board-image.yml) builds exactly these commits. Run it before
# tagging a release; every commit must be pushed (the workflow fetches them from GitHub).
set -eu
board="${1:?usage: board_lock.sh <board> [<layer>...]}"; shift
layers="${*:-tls}"
root="$(cd "$(dirname "$0")/../.." && pwd)"
case "$board" in
	pc)        adapt="adaptation-x86-64 adaptation-pc"; dev=adaptation-pc ;;
	pinephone) adapt="adaptation-a64 adaptation-pinephone"; dev=adaptation-pinephone ;;
	*) echo "unknown board $board (pc, pinephone)" >&2; exit 1 ;;
esac
out="$root/$dev/board.lock"
{
	echo "# repository commit -- what the $board release is built from (pdk tools/board_lock.sh)"
	for r in pdk core $adapt $layers; do
		[ -d "$root/$r/.git" ] || { echo "no checkout $root/$r" >&2; exit 1; }
		if [ -n "$(git -C "$root/$r" status --porcelain --untracked-files=no)" ]; then
			echo "warning: $r has uncommitted changes (the lock pins its last commit)" >&2
		fi
		echo "$r $(git -C "$root/$r" rev-parse HEAD)"
	done
} > "$out"
cat "$out"
