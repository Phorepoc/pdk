#!/bin/bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# prepare_cache.sh -- what the prepare recipes (recipes/prepare.sh of the board's repositories) leave behind that a later
# run can reuse. A repository lists it in recipes/prepare.cache:
#
#   image <ref>      a toolchain image the recipe runs in (its content id is part of the key)
#   key   <file>     a file of the repository that decides what the recipe makes (its content is part of the key)
#   path  <dir>      a folder to keep: relative to the repository, or $BUILD/<dir> for the build folder
#
# Prints, for the workflow's cache step (GITHUB_OUTPUT): key and paths. The key is the content of the listed files and
# images and nothing else, so a hit is what the recipe would make again; what the cached files go on to be compiled
# against (the exported headers) is checked by content when they are used (tools/emitter/objcache.py), never assumed.
set -eu
: "${PHOREPOC_BUILD:?}" "${PHOREPOC_ADAPTATIONS:=}" "${PHOREPOC_ARCH:?}"
sum="arch $PHOREPOC_ARCH"; paths=""
for r in $(echo "$PHOREPOC_ADAPTATIONS" | tr : ' '); do
  f="$r/recipes/prepare.cache"; [ -f "$f" ] || continue
  sum="$sum
repo $(basename "$r")"
  while read -r kind arg rest; do
    case "$kind" in
      image) docker pull -q "$arg" </dev/null >/dev/null; sum="$sum
image $arg $(docker image inspect --format '{{.Id}}' "$arg")" ;;
      key) sum="$sum
key $arg $(sha1sum < "$r/$arg" | cut -d' ' -f1)" ;;
      path) case "$arg" in
              '$BUILD/'*) paths="$paths$PHOREPOC_BUILD/${arg#\$BUILD/}
" ;;
              *) paths="$paths$r/$arg
" ;;
            esac ;;
      ''|'#'*) ;;
      *) echo "prepare_cache: $f: unknown line: $kind $arg" >&2; exit 1 ;;
    esac
  done < "$f"
done
[ -n "$paths" ] || exit 0
echo "$sum" >&2
{
  echo "key=prepare-$PHOREPOC_ARCH-$(printf '%s' "$sum" | sha1sum | cut -c1-40)"
  echo "paths<<PATHS_END"
  printf '%s' "$paths"
  echo "PATHS_END"
} >> "${GITHUB_OUTPUT:-/dev/stdout}"
