#!/bin/sh
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# frontend_id.sh -- names the compile front end of this checkout: the trees of tools/emitter and tools/toolchain (the
# build scripts and the compiler wrapper) and the toolchain image. A published build tree records it
# (build/.compile-frontend); a build that starts from such a tree compiles in one job when its own matches, since then
# little is left to compile, and in shards when it does not (a changed flag or compiler recompiles most of the tree).
# Only a speed decision: whether an object is reused is decided by its content (tools/emitter/objcache.py).
set -eu
pdk="${PHOREPOC_PDK:-pdk}"
printf '%s %s %s\n' "$(git -C "$pdk" rev-parse HEAD:tools/emitter)" "$(git -C "$pdk" rev-parse HEAD:tools/toolchain)" \
  "$(docker image inspect --format '{{.Id}}' "${PHOREPOC_IMAGE:-ghcr.io/phorepoc/base:latest}")"
