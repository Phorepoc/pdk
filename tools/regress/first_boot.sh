#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# first_boot.sh -- IN-CONTAINER regression test: the home screen on a freshly formatted C:, first and second boot.
#   bash tools/regress/first_boot.sh            (after tools/make_phorepoc_rom.sh and tools/make_phorepoc_kernel.sh)
# Formats a NEW blank disk (never a reused one: the first boots of a disk install the home-screen configuration and
# create C:'s system data), boots the image twice from copies of it, and fails unless both boots show the home screen
# with its widgets (first_boot.py). Out: $OUT = $PHOREPOC_BUILD/x86/regress/first-boot: bootN .log .ppm .png.
set -eu
cd "$(dirname "$0")/../.."
B="${PHOREPOC_BUILD:-/build}"
OUT="${OUT:-$B/x86/regress/first-boot}"
ISO="${ISO:-$B/x86/ekern/phorepoc-ekern.iso}"
[ -f "$ISO" ] || { echo "no $ISO -- build the ROM and the kernel first" >&2; exit 2; }
rm -rf "$OUT"; mkdir -p "$OUT"
OSV=$(python3 tools/emitter/paths.py)
IMG="$OUT/blank.img" KOUT="$OUT" FRESH=1 bash "$OSV/tools/phorepoc-x86/make_disk.sh" "${DISK_MB:-256}" > "$OUT/make_disk.log"
exec python3 tools/regress/first_boot.py --iso "$ISO" --disk "$OUT/blank.img" --out "$OUT" \
     --smp "${SMP:-2}" --mem "${MEM:-1024}" --settle "${SETTLE:-25}" --boots "${BOOTS:-2}"
