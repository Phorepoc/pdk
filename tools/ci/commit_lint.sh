#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# commit_lint.sh <from> <to> [<repository>] -- every commit in from..to has a Conventional Commit subject
# ("type(scope)!: summary"), which next_version.sh turns into the release version. Merge commits are skipped.
# <from> may be empty or all zeros (a new branch): then only <to> itself is checked.
set -eu
from=$1 to=$2
cd "${3:-.}"
types='feat|fix|perf|refactor|docs|test|build|ci|chore|style|revert'
if [ -z "$from" ] || [ "$from" = 0000000000000000000000000000000000000000 ]; then range="-1 $to"; else range="$from..$to"; fi
bad=0
for h in $(git rev-list --no-merges $range); do
	subj=$(git log -1 --format=%s "$h")
	if ! echo "$subj" | grep -q -E "^($types)(\([a-z0-9._/-]+\))?!?: [^ ]"; then
		echo "::error::$(echo "$h" | cut -c1-8) \"$subj\": not a Conventional Commit (type(scope): summary; types: $types)"
		bad=1
	fi
done
exit $bad
