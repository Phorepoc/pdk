#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# make_arm_image.sh -- the PinePhone image from the public repositories ("ph image pinephone"). Runs on Linux inside the
# toolchain container, after the ARM components are built (ph component build -A -arch arm -b pinephone).
#
#   1. the kernel include shims (as make_phorepoc_kernel.sh) -> <build>/arm/kinc (the device builds include them)
#   2. the core images the kernel boots first (euser, efsrv, hal, efile: the device builds) -> <build>/arm/device
#   3. the recipe's tree (<build>/arm/kview): the adaptation's image scripts (adaptation-a64 os/tools/kernel,
#      adaptation-pinephone os/tools/phorepoc-arm) were written for one tree holding the OS sources, the build tools
#      and the Symbian packages; that tree is assembled here from links -- src/ toolchain/ tools/ (the merged OS tree +
#      the pdk), emitter/ (the pdk's emitter + the core images under the names the recipe uses), baseline/ (the
#      source), a `py` on the PATH
#   4. build_ekern_rom.sh BOARD=a64 PHOREPOC=1 BOOT=ssm: kernel link, bootstrap, the ROM from the shared recipe
#      (make_phorepoc_rom.sh, ROM_ARCH=arm), then make_pinephone_image.sh (symbian.img + boot.scr)
#   5. RELEASE=1: make_pinephone_sd.sh RELEASE=1, the SD-card image for balenaEtcher (needs docker: run it on the host)
#
# SSM_TEST (default wlanjoin: the SSM test list joins the WLAN from c:wlanjoin.txt), ROM_TAIL (the big GL libraries last in the ROM).
# Output: <build>/pinephone/{symbian.img,boot.scr,symbian.img.map}. Settings: PRUNE (default 1: drop images whose
# imports the ROM lacks), NGA (default 1), RAM_MB (make_pinephone_image.sh).
set -eu
PDK="$(cd "$(dirname "$0")/.." && pwd)"
# PHOREPOC_QT48=1 = the Qt 4.8 configuration (registry/components_s4.py: the Qt libraries, libstdcpp/osncore/alf with their x32 flags; needs the Qt source tree in <build>/qt48, fetched separately);
# the image must be made with the same setting as the components
export PHOREPOC_QT48="${PHOREPOC_QT48:-0}"
export PHOREPOC_CORE="${PHOREPOC_CORE:-$PDK/../core}"
export PHOREPOC_OS_ROOT="${PHOREPOC_OS_ROOT:-$PHOREPOC_CORE/os}"
export PHOREPOC_SYMBIAN="${PHOREPOC_SYMBIAN:-$PHOREPOC_CORE/symbian}"
export PHOREPOC_BUILD="${PHOREPOC_BUILD:-$PHOREPOC_CORE/build}"
OS_ROOT="$PHOREPOC_OS_ROOT"
[ -z "${PHOREPOC_ADAPTATIONS:-}" ] || OS_ROOT="$(python3 "$PDK/tools/emitter/paths.py")"
SYM="$PHOREPOC_SYMBIAN"; SYM10="$SYM/symbian-2010"; KHS="$SYM10/oss.FCL.sf.os.kernelhwsrv"
A="$PHOREPOC_BUILD/arm"; DEV="$A/device"; V="$A/kview"; OUT="$PHOREPOC_BUILD/pinephone"
[ -f "$OS_ROOT/tools/kernel/build_ekern_rom.sh" ] || { echo "no ARM image recipe in $OS_ROOT/tools/kernel (board pinephone: adaptation-a64)" >&2; exit 1; }
[ -f "$OS_ROOT/tools/phorepoc-arm/make_pinephone_image.sh" ] || { echo "no PinePhone packager (adaptation-pinephone)" >&2; exit 1; }
[ -d "$A/img" ] || { echo "no ARM images in $A/img (ph component build -A -arch arm -b pinephone)" >&2; exit 1; }

echo "[1/5] kernel include shims"
KINC="$A/kinc"; rm -rf "$KINC"; mkdir -p "$KINC/kernel"
echo "#include \"$KHS/kernel/eka/include/heap_hybrid.h\"" > "$KINC/kernel/heap_hybrid.h"
echo "#include \"$KHS/halservices/hal/inc/hal_int.h\"" > "$KINC/kernel/hal_int.h"
SHIM="$(mktemp -d)"; trap 'rm -rf "$SHIM"' EXIT
printf '#!/bin/sh\nargs=""\nfor a in "$@"; do case "$a" in -I-|-lang-c++) ;; *) args="$args $a";; esac; done\nexec g++ -E -x c++ $args\n' > "$SHIM/cpp"
chmod +x "$SHIM/cpp"
( cd "$KHS/kernel/eka/kernel" && PATH="$SHIM:$PATH" perl genexec.pl -i execs.txt \
    -e "$KINC/kernel/exec_enum.h" -u "$KINC/kernel/exec_user.h" -k "$KINC/kernel/exec_kernel.h" )

echo "[2/5] core images (device builds)"
mkdir -p "$DEV"
export PHOREPOC_KINC="$KINC"
for t in euser_device efsrv_device hal_device efile_exe; do
  python3 "$PDK/tools/emitter/build_$t.py" > "$DEV/$t.log" 2>&1 || { echo "build_$t failed:"; tail -5 "$DEV/$t.log"; exit 1; }
done

echo "[3/5] the recipe's tree ($V)"
# link every entry of the given folders into one folder (later folders do not replace earlier entries)
merge() { local dst=$1; shift; mkdir -p "$dst"; for d in "$@"; do [ -d "$d" ] || continue
  for e in "$d"/* "$d"/.[!.]*; do [ -e "$e" ] || continue; [ -e "$dst/${e##*/}" ] || ln -s "$e" "$dst/${e##*/}"; done; done; }
rm -rf "$V/src" "$V/tools" "$V/toolchain" "$V/emitter" "$V/baseline" "$V/bin"
mkdir -p "$V"
ln -s "$OS_ROOT/src" "$V/src"
rm -rf "$V/third_party" "$V/.epoc32"; ln -s "$OS_ROOT/third_party" "$V/third_party"
# the exported public headers (tools/epoc32_export.py) the kernel-side NGA sources include (<sgresource/...>)
[ -d "$PHOREPOC_BUILD/epoc32" ] && ln -s "$PHOREPOC_BUILD/epoc32" "$V/.epoc32"
merge "$V/tools" "$OS_ROOT/tools" "$PDK/tools"
merge "$V/toolchain" "$OS_ROOT/toolchain" "$PDK/tools/toolchain"
rm -f "$V/toolchain/kinc"; ln -s "$KINC" "$V/toolchain/kinc"
# emitter/ = the pdk's emitter scripts + the core images under the recipe's names
merge "$V/emitter" "$PDK/tools/emitter"
ln -sf "$DEV/euser.dll" "$V/emitter/euser_dev.dll"; ln -sf "$DEV/efsrv.dll" "$V/emitter/efsrv_dev.dll"
ln -sf "$DEV/hal.dll" "$V/emitter/hal_dev.dll"; ln -sf "$DEV/efile.exe" "$V/emitter/efile.exe"
for n in e32strt.exe estart.exe; do [ -f "$A/img/$n" ] && { ln -sf "$A/img/$n" "$V/emitter/e32strt.exe"; break; }; done
[ -e "$V/emitter/e32strt.exe" ] || { echo "no e32strt (the real ESTART) in $A/img: build the ARM components first" >&2; exit 1; }
mkdir -p "$V/baseline"
ln -s "$SYM10" "$V/baseline/symbian-2010"
[ -d "$SYM/symbian-rcl3" ] && ln -s "$SYM/symbian-rcl3" "$V/baseline/symbian-rcl3"
ln -s "$KHS" "$V/baseline/kernelhwsrv"
mkdir -p "$V/bin"; printf '#!/bin/sh\nexec python3 "$@"\n' > "$V/bin/py"; chmod +x "$V/bin/py"
export PATH="$V/bin:$PATH" KVIEW="$V"
# the AArch64 entry shim (needs an AArch64 GCC, which this container lacks: tools/make_arm_shim.sh builds it on the host)
[ -f "$A/a53/shim-ram-noprobe.bin" ] || [ -n "$(command -v aarch64-linux-gnu-gcc)" ] \
  || { echo "no entry shim in $A/a53: run tools/make_arm_shim.sh <adaptation-a64> <build> on the host first" >&2; exit 1; }
mkdir -p "$A/a53"; rm -rf "$V/.a53-build"; ln -s "$A/a53" "$V/.a53-build"
echo "[4/5] kernel + ROM (build_ekern_rom.sh BOARD=a64 PHOREPOC=1 BOOT=ssm)"
mkdir -p "$OUT"
( cd "$V" && BOARD=a64 SMP=1 MMODEL=flexible SECONDARY=efile PHOREPOC=1 BOOT=ssm ROM_ARCH=arm NGA="${NGA:-1}" \
    PRUNE_UNRESOLVED="${PRUNE:-1}" ROM_TAIL="${ROM_TAIL:-phgl,libegl,libglesv2,libglesv1_cm}" SSM_TEST="${SSM_TEST:-wlanjoin}" \
    bash tools/kernel/build_ekern_rom.sh ) > "$OUT/rom.log" 2>&1 \
  || { echo "IMAGE FAILED:"; tail -20 "$OUT/rom.log"; exit 1; }

echo "[5/5] the PinePhone image"
for f in symbian.img symbian.img.map boot.scr; do [ -f "$V/.pinephone-build/$f" ] && cp -f "$V/.pinephone-build/$f" "$OUT/"; done
[ -f "$OUT/symbian.img" ] || { echo "no symbian.img (see $OUT/rom.log)" >&2; exit 1; }
echo "pinephone image: $(ls -l "$OUT/symbian.img" | awk '{print $5}') bytes -> $OUT (SD card: O=$OUT RELEASE=1 make_pinephone_sd.sh on the host)"
