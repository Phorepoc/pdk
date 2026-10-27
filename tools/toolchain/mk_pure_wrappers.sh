#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# mk_pure_wrappers.sh -- inside the toolchain container: writes $W/bin/pp-gcc and pp-g++, the "pure Open C" compile line
# for ported C/C++ libraries that bring their own build (FFmpeg and OpenH264 in codecpack, WebKit/ICU, OpenSSL):
# tools/openc_cflags.py --exe minus the Symbian kernel/euser include dirs and minus its force-includes (variant .hrh,
# port_prelude.h, e32def.h); a stub e32def.h (openc_posix/pure) satisfies Open C's own #include <e32def.h>, and
# openc_posix/ adds the POSIX headers Open C lacks (poll.h, netinet/tcp.h, sys/statvfs.h, uchar.h ...).
#
#   ARCH=arm|x32 W=<out dir> bash tools/toolchain/mk_pure_wrappers.sh
#
# env: PHOREPOC_CORE / PHOREPOC_SYMBIAN / PHOREPOC_BUILD as for every pdk tool (the include tree must be exported);
# W defaults to $PHOREPOC_BUILD/pure-openc-$ARCH.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
PDK="$(cd "$HERE/../.." && pwd)"
ARCH=${ARCH:-x32}
OVR=$HERE/openc_posix
BUILD=${PHOREPOC_BUILD:-$(python3 -c "import sys; sys.path.insert(0, '$PDK/tools/emitter'); import paths; print(paths.BUILD)")}
W=${W:-$BUILD/pure-openc-$ARCH}
if [ "$ARCH" = arm ]; then
  # ARMv7 Thumb-2 + VFP, soft-float ABI (links with the rest of an ARM image)
  CROSS=arm-none-eabi-; CFL=--arm
  MFL="-march=armv7-a -mthumb -mfloat-abi=softfp -mfpu=vfpv3-d16 -mword-relocations -include $OVR/pure/ph_arm_prefix.h -D__IEEE_LITTLE_ENDIAN"
  GINC=$(arm-none-eabi-gcc -print-file-name=include)
  POST="-fno-pic"   # after the caller's flags: libraries asking for -fPIC get GOT relocations, which E32 images cannot carry
else
  CROSS=; CFL=; MFL=-mx32; GINC=$(gcc -print-file-name=include); POST=""
fi
mapfile -t FL < <(python3 "$PDK/tools/openc_cflags.py" --exe $CFL | grep -v -x -E -e '-x|c|-march=armv5te|-marm|-mthumb-interwork|-mfloat-abi=soft|-DSYMCC_C_DRIVER|-std=gnu11|-fno-exceptions|-fno-rtti|-fshort-wchar|-w|-ffreestanding|-fno-builtin|-masm=intel|-Dwchar_t=unsigned short|-D__SOFTFP=')
[ "${#FL[@]}" -gt 10 ] || { echo "mk_pure_wrappers: openc_cflags.py failed (is the include tree exported, the build dir writable?)"; exit 1; }
OUTA=(); LATE=(); skip=0; prev=""
for f in "${FL[@]}"; do
  if [ $skip = 1 ]; then skip=0; continue; fi
  case "$f" in
    -include) skip=1; continue;;
    -I|-idirafter) prev=$f; continue;;
  esac
  if [ "$prev" = -idirafter ]; then
    prev=""
    # the x32 line's case-folded mirror of the whole tree (casefix/): searched before stdapis it hands out the old estlib
    # <signal.h> / <sys/types.h> (no sigset_t, no ssize_t) -- kept only as the very last fallback, after GCC's own
    case "$f" in */casefix) LATE+=("-idirafter" "$f");; *) OUTA+=("-idirafter" "$f");; esac
    continue
  fi
  if [ "$prev" = -I ]; then prev=""; p="$f"; else p="${f#-I}"; [ "$p" = "$f" ] && { OUTA+=("$f"); continue; }; fi
  case "$p" in
    */toolchain/*|*/kernel/eka/*|*/halservices/*|*/tools/phorepoc-x86/port) continue;;
    # Open C's stdapis LAST (-idirafter), and epoc32/include (zlib.h too): they bundle OpenSSL 0.9.8, an old sqlite3.h
    # and zlib, and a ported library's own come in as -isystem dirs, which GCC searches after every -I (a -I naming an
    # -isystem dir is ignored). Open C still answers everything nobody else provides (libstdc++'s #include_next too).
    */stdapis|*/stdapis/*|*/epoc32/include) OUTA+=("-idirafter" "$p"); continue;;
  esac
  OUTA+=("-I$p")
done
mkdir -p "$W/bin"
for t in gcc g++; do
  # ARM: the bare-metal driver rejects -pthread (OpenSSL, CMake's Threads): dropped -- the threads are Open C's pthreads
  { echo "#!/bin/sh"
    [ "$ARCH" = arm ] && echo 'for a; do shift; [ "$a" = -pthread ] || set -- "$@" "$a"; done'
    echo -n "exec $CROSS$t $MFL -include $OVR/ph_cxx_compat.h "'"$@"'" $POST -I$OVR/pure "
    printf "'%s' " "${OUTA[@]}" "-D__SOFTFP=" "-I$OVR" -include "$OVR/ph_cxx_compat.h" "-idirafter" "$GINC" ${LATE[@]+"${LATE[@]}"}
    echo; } > "$W/bin/pp-$t"
  chmod +x "$W/bin/pp-$t"
done
echo "mk_pure_wrappers: $W/bin/pp-gcc, pp-g++ ($ARCH)"
