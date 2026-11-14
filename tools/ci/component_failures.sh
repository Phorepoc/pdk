#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# component_failures.sh LOG -- the verdict on a bulk `phe component build -A` (board-finish's link step), which goes on past a
# component that fails. Every component LOG names as FAIL is built once more on its own (the relink passes may have given it
# the provider it lacked); one that still fails ends the build with its error text, unless tools/ci/component-fail.allow
# lists it for this architecture. Before this check a component could fail silently and the image kept the core layer's copy
# (a board release once shipped the 2.6 KB stub phgl.dll: no OpenGL ES in any app).
set -u
log=${1:?usage: component_failures.sh component-build.log [bulk exit status]}
status=${2:-0}
arch=${PHOREPOC_ARCH:?}
here=$(cd "$(dirname "$0")" && pwd)
allow="$here/component-fail.allow"
failed=$(awk '/^=== /{c=$2} /FAIL/{print c}' "$log" | tr -d : | sort -u)
if [ -z "$failed" ]; then
	# A clean bulk build exits 0 (a target with no sources is skipped with exit 0, a missing archive is a LINK FAIL). A
	# non-zero exit that names no component (the build front end, a registry error, an unexpected `! ` return) fails.
	[ "$status" = 0 ] && { echo "components failed: 0"; exit 0; }
	echo "::group::the bulk build exited $status and names no failed component -- its ! notes and last lines"
	grep -a -B1 "^  ! " "$log" | grep -v "no provider registered" | grep -v "^--" | tail -n 40
	tail -n 40 "$log"
	echo "::endgroup::"
	echo "::error::the bulk component build exited $status without naming a failed component ($arch)"
	exit 1
fi
echo "components failed in the bulk build: $(echo $failed)"
bad=
for c in $failed; do
	if phe component build -arch "$arch" "$c" > "component-retry-$c.log" 2>&1; then
		echo "  $c: builds on its own now (a provider came later)"
		continue
	fi
	if [ -f "$allow" ] && sed 's/#.*//' "$allow" | awk -v a="$arch" -v c="$c" '($1 == a || $1 == "*") && $2 == c { f = 1 } END { exit !f }'; then
		echo "  $c: fails, allowed for $arch (component-fail.allow)"
		continue
	fi
	echo "::group::$c fails -- its build output"
	tail -n 60 "component-retry-$c.log"
	echo "::endgroup::"
	echo "::error::component $c does not build ($arch)"
	bad="$bad $c"
done
[ -z "$bad" ] || { echo "components that fail:$bad"; exit 1; }
