#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# component_failures.sh LOG -- the verdict on a bulk `phe component build -A` (board-finish's link step), which goes on past a
# component that fails. Every component LOG names as FAIL is built once more on its own (the relink passes may have given it
# the provider it lacked); one that still fails ends the build with its error text, unless tools/ci/component-fail.allow
# lists it for this architecture. Before this check a component could fail silently and the image kept the core layer's copy
# (adaptation-pc v0.5.1..v0.5.8 shipped the 2.6 KB stub phgl.dll: no OpenGL ES in any app).
set -u
log=${1:?usage: component_failures.sh component-build.log}
arch=${PHOREPOC_ARCH:?}
here=$(cd "$(dirname "$0")" && pwd)
allow="$here/component-fail.allow"
failed=$(awk '/^=== /{c=$2} /FAIL/{print c}' "$log" | tr -d : | sort -u)
[ -n "$failed" ] || { echo "components failed: 0"; exit 0; }
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
