#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# make_arm_shim.sh -- build the AArch64 entry shim of the PinePhone image (adaptation-a64 os/src/phorepoc-arm/a53shim/shim.S).
# Run it on the HOST (it needs docker and the toolchain image that has an AArch64 GCC, default phorepoc-arm; the Linux
# build container has none), before "ph image pinephone". Output: <build>/arm/a53/shim-ram-noprobe.bin, which
# make_arm_image.sh links into the recipe tree.
#   bash tools/make_arm_shim.sh <adaptation-a64 checkout> <build dir or docker volume>
# Environment: ARM_IMAGE (docker image with aarch64-linux-gnu-gcc, default phorepoc-arm).
set -eu
A64="${1:?adaptation-a64 checkout}"; BUILD="${2:?build dir or docker volume}"
case "$BUILD" in /*|?:*|.*) BV="$(cd "$BUILD" && (pwd -W 2>/dev/null || pwd))" ;; *) BV="$BUILD" ;; esac
MSYS_NO_PATHCONV=1 docker run --rm -v "$BV:/build" -v "$(cd "$A64" && (pwd -W 2>/dev/null || pwd))/os:/os:ro" "${ARM_IMAGE:-phorepoc-arm}" bash -c '
  set -e; mkdir -p /build/arm/a53
  aarch64-linux-gnu-gcc -c -DTARGET=0x40300000 /os/src/phorepoc-arm/a53shim/shim.S -o /tmp/shim.o
  aarch64-linux-gnu-ld -Ttext=0 /tmp/shim.o -o /tmp/shim.elf
  aarch64-linux-gnu-objcopy -O binary /tmp/shim.elf /build/arm/a53/shim-ram-noprobe.bin
  ls -l /build/arm/a53/shim-ram-noprobe.bin'
