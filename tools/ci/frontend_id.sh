#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

# frontend_id.sh -- names the compile front end of this checkout: what decides HOW a source file is compiled, and nothing
# else: the toolchain image (the compiler), the compiler wrapper and its flag files (tools/toolchain, tools/openc_cflags.py)
# and the scripts that build the command line and the object key (tools/emitter: build_mmp*.py, mmp.py, paths.py,
# objcache.py, case_shim.py, devlink.py). A published build tree records it (build/.compile-frontend); a build that starts
# from such a tree compiles in one job when its own matches, since then little is left to compile, and in shards when it
# does not (a changed flag or compiler recompiles most of the tree).
# The pdk commit is NOT part of it: a change to the docs, the workflows, the linker side (e32image.py, elf.py, the
# image recipes) or phe itself leaves it alone. Only a speed decision: whether an object is reused is decided by its
# content (tools/emitter/objcache.py), never by this id.
set -eu
pdk="${PHOREPOC_PDK:-pdk}"
files="tools/openc_cflags.py tools/emitter/build_mmp.py tools/emitter/build_mmp_x32.py tools/emitter/mmp.py
tools/emitter/paths.py tools/emitter/objcache.py tools/emitter/case_shim.py tools/emitter/devlink.py"
sum=$(for f in $files; do printf '%s %s\n' "$f" "$(git -C "$pdk" rev-parse "HEAD:$f" 2>/dev/null || echo none)"; done | sha1sum | cut -c1-12)
printf '%s %s %s\n' "$sum" "$(git -C "$pdk" rev-parse HEAD:tools/toolchain)" \
  "$(docker image inspect --format '{{.Id}}' "${PHOREPOC_IMAGE:-ghcr.io/phorepoc/base:latest}")"
