#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# board_lock.sh <board repository> -- write <board repository>/board.lock: the commit of every repository the board's
# release is built from (the pdk, core and its board.conf REPOS), taken from the checkouts next to this pdk. The release
# workflow (.github/workflows/board-image.yml) builds exactly these commits. Run it before tagging a release; every
# commit must be pushed (the workflow fetches them from GitHub).
set -eu
dir="$(cd "${1:?usage: board_lock.sh <board repository>}" && pwd)"
[ -f "$dir/board.conf" ] || { echo "no board.conf in $dir" >&2; exit 1; }
NAME= REPOS=; . "$dir/board.conf"
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$dir/board.lock"
{
	echo "# repository commit -- what the $NAME release is built from (pdk tools/board_lock.sh)"
	for r in pdk core $REPOS; do
		[ -d "$root/$r/.git" ] || { echo "no checkout $root/$r" >&2; exit 1; }
		if [ -n "$(git -C "$root/$r" status --porcelain --untracked-files=no)" ]; then
			echo "warning: $r has uncommitted changes (the lock pins its last commit)" >&2
		fi
		echo "$r $(git -C "$root/$r" rev-parse HEAD)"
	done
} > "$out"
cat "$out"
