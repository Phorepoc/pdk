#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# build_a53_shim.sh [out] -- the AArch64 -> AArch32 entry shim of the PinePhone image (adaptation-a64
# os/src/phorepoc-arm/a53shim/shim.S, TARGET 0x40300000 = the in-RAM ROM), built in the toolchain's arm image
# (it carries the AArch64 compiler; the build image make_arm_image.sh runs in does not). Run it on the host (needs docker):
#   bash tools/build_a53_shim.sh <build>/arm/shim-ram-noprobe.bin
# make_arm_image.sh picks the file up from there (PHOREPOC_SHIM overrides the path).
set -eu
PDK="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${PHOREPOC_A53SHIM:-$PDK/../adaptation-a64/os/src/phorepoc-arm/a53shim}"
OUT="${1:-${PHOREPOC_BUILD:-$PDK/../core/build}/arm/shim-ram-noprobe.bin}"
mkdir -p "$(dirname "$OUT")"; OUTD="$(cd "$(dirname "$OUT")" && pwd)"
MSYS_NO_PATHCONV=1 docker run --rm -v "$(cd "$SRC" && pwd -W 2>/dev/null || cd "$SRC" && pwd):/s:ro" \
  -v "$(cd "$OUTD" && pwd -W 2>/dev/null || cd "$OUTD" && pwd):/o" "${PHOREPOC_ARM_IMAGE:-ghcr.io/phorepoc/arm:latest}" bash -c \
  "set -e; aarch64-linux-gnu-gcc -c -DTARGET=0x40300000 /s/shim.S -o /tmp/shim.o && aarch64-linux-gnu-ld -Ttext=0 /tmp/shim.o -o /tmp/shim.elf && aarch64-linux-gnu-objcopy -O binary /tmp/shim.elf /o/$(basename "$OUT")"
echo "build_a53_shim: $OUT ($(wc -c < "$OUT") bytes)"
