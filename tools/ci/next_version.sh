#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# next_version.sh [<repository> [<path>...]] -- the next release version from the Conventional Commits since the last v* tag
# (https://www.conventionalcommits.org): a breaking change ("type!:" or a "BREAKING CHANGE:" footer) bumps the major
# version (the minor one while below 1.0), "feat" the minor one, "fix" and "perf" the patch one; anything else
# releases nothing. Prints the version (v1.2.3), or nothing when there is nothing to release. No tag yet = v0.1.0.
# With paths, only the commits that change them count (a repository that releases one package of a larger tree: core's
# browser frontend).
set -eu
cd "${1:-.}"
[ $# -gt 0 ] && shift
last=$(git describe --tags --abbrev=0 --match 'v[0-9]*.[0-9]*.[0-9]*' 2>/dev/null || true)
if [ -z "$last" ]; then
	[ -n "$(git log -1 --format=%H 2>/dev/null)" ] && echo v0.1.0
	exit 0
fi
range="$last..HEAD"
bump=0                                    # 0 none, 1 patch, 2 minor, 3 major
for h in $(git rev-list --no-merges "$range" -- "$@"); do
	subj=$(git log -1 --format=%s "$h")
	b=0
	case "$subj" in
		feat*) b=2 ;;
		fix*|perf*) b=1 ;;
	esac
	echo "$subj" | grep -q -E '^[a-z]+(\([^)]*\))?!:' && b=3
	git log -1 --format=%b "$h" | grep -q -E '^BREAKING[ -]CHANGE:' && b=3
	# only a well-formed Conventional Commit subject counts for feat/fix/perf
	[ "$b" -lt 3 ] && ! echo "$subj" | grep -q -E '^(feat|fix|perf)(\([^)]*\))?: ' && b=0
	[ "$b" -gt "$bump" ] && bump=$b
done
[ "$bump" -eq 0 ] && exit 0
v=${last#v}
maj=${v%%.*}; rest=${v#*.}; min=${rest%%.*}; pat=${rest#*.}
case $bump in
	3) if [ "$maj" -eq 0 ]; then min=$((min + 1)); pat=0; else maj=$((maj + 1)); min=0; pat=0; fi ;;
	2) min=$((min + 1)); pat=0 ;;
	1) pat=$((pat + 1)) ;;
esac
echo "v$maj.$min.$pat"
