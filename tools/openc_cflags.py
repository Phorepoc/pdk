# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""openc_cflags -- print the C compile line for Open C / P.I.P.S. client code built outside the registry (ports of
C libraries that bring their own build: Mesa's, in the mesa-symbian repository).

Runs inside the toolchain container (like emitter/build_mmp_x32.py, which it imports read-only).
It derives the flags exactly the way build_mmp_x32.build() derives them for a `cdriver` component
of the Open C family (the registry's "libc" entry's `defs=["__SOFTFP="]`: on ARM the compiler prefix
supplies it; libc's own -fno-builtin is libc-internal and is NOT taken), from a minimal Open C client .mmp:

    #include <platform_paths.hrh>
    OS_LAYER_SYSTEMINCLUDE / OS_LAYER_LIBC_SYSTEMINCLUDE / OS_LAYER_SSL_SYSTEMINCLUDE

so the SYSTEMINCLUDE set (epoc32/include, stdapis, stdapis/sys, stdapis/openssl...) is the one a real
Open C program's mmp gets. The C-driver transform (drop the C++-only switches, -std=..., C has no
built-in wchar_t -> -Dwchar_t=unsigned short) is build_mmp_x32's own.

    python3 openc_cflags.py [--std=gnu11] [--exe] [--arm]    -> one flag per line on stdout

--arm (or OPENC_ARCH=arm): the ARM line instead (emitter/build_mmp.py, for arm-none-eabi-gcc): toolchain/symcc's ARM
flags/defines/includes + its prefix header, build_mmp.py's per-component
switches (variant .hrh, __NO_CLASS_CONSTS__, the stdarg guards for a stdapis client, `-D__SOFTFP=`) and its
`cdriver` transform. -nostdinc as on x32: every libc header comes from stdapis, none from newlib.
"""
from __future__ import annotations

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))

import build_mmp_x32 as B          # noqa: E402  (read-only use: its constants + helpers)
import mmp as MMP                  # noqa: E402  (build_mmp_x32 has already pointed MMP.CPP at the container's cpp)
from paths import OS_ROOT, SYMBIAN  # noqa: E402

TEMPLATE = """#include <platform_paths.hrh>
TARGET          openc_client.exe
TARGETTYPE      exe
UID             0x20004C45 0x0
OS_LAYER_SYSTEMINCLUDE
OS_LAYER_LIBC_SYSTEMINCLUDE
OS_LAYER_SSL_SYSTEMINCLUDE
OS_LAYER_LIBXML2_SYSTEMINCLUDE
OS_LAYER_ZLIB_SYSTEMINCLUDE
"""


def main() -> int:
    std = "gnu11"
    exe = False
    arm = os.environ.get("OPENC_ARCH") == "arm"
    for a in sys.argv[1:]:
        if a == "--arm":
            arm = True
        if a.startswith("--std="):
            std = a.split("=", 1)[1]
        elif a == "--exe":
            exe = True
    d = "/tmp/openc-template"
    os.makedirs(d, exist_ok=True)
    path = os.path.join(d, "openc_client.mmp")
    with open(path, "w", newline="\n") as f:
        f.write(TEMPLATE)
    m = MMP.parse(path, B.MMP_DEFS)
    if arm:
        eka = os.path.join(SYMBIAN, "kernelhwsrv", "kernel", "eka")
        flags = (["-x", "c", f"-std={std}", "-O2", "-march=armv5te", "-marm", "-mthumb-interwork", "-mfloat-abi=soft",
                  "-fshort-wchar", "-fno-short-enums", "-fno-strict-aliasing", "-fno-delete-null-pointer-checks",
                  "-ffunction-sections", "-fdata-sections", "-nostdinc",
                  "-include", os.path.join(HERE, "toolchain", "symbian_gcc_prefix.h"),
                  "-D__SYMBIAN32__", "-D__GCCE__", "-D__EABI__", "-D__MARM__", "-D__MARM_ARMV5__", "-D__EPOC32__",
                  "-D_UNICODE", "-D__NO_CLASS_CONSTS__", "-D__EXE__" if exe else "-D__DLL__", "-D__SOFTFP="]
                 + [f"-I{p}" for p in (os.path.join(OS_ROOT, "toolchain", "overlay"),
                                       os.path.join(OS_ROOT, "toolchain", "generated"),
                                       os.path.join(eka, "include"), os.path.join(eka, "euser"),
                                       os.path.join(eka, "euser", "cbase"), os.path.join(eka, "euser", "epoc", "arm"),
                                       os.path.join(eka, "euser", "maths"), os.path.join(eka, "euser", "unicode"),
                                       os.path.join(eka, "common"))]
                 + [f"-I{p}" for p in m.sysinc]
                 + ["-include", MMP.variant_for(m.path),
                    # build_mmp.py: a stdapis client keeps the prefix header's builtin va_list (EABI varargs)
                    "-D_STDARG_H", "-D_STDARG_E_H", "-D__e32_va_list=va_list",
                    # build_mmp.py's `cdriver` transform
                    "-DSYMCC_C_DRIVER", "-Dwchar_t=unsigned short", "-D__wchar_t_defined"])
        print("\n".join(flags))
        return 0
    libc = B.C.COMPONENTS["libc"]
    kind = "-D__EXE__" if exe else "-D__DLL__"
    flags = (B.BASE_FLAGS + B.BASE_DEFS + [kind] + [f"-D{x}" for x in libc.get("defs", [])]
             + B.BASE_INCS + [f"-I{p}" for p in m.sysinc]
             + ["-include", MMP.variant_for(m.path)] + B.PRE)
    # build_mmp_x32's `cdriver` transform: C, not C++
    cflags = [(f"-std={std}" if f == "-std=gnu++03" else f) for f in flags
              if f not in ("-frtti", "-fno-rtti", "-fno-access-control", "-fno-threadsafe-statics",
                           "-fpermissive", "-fno-exceptions")]
    cflags = ["-x", "c"] + cflags + ["-Dwchar_t=unsigned short", "-D__wchar_t_defined"]
    print("\n".join(cflags))
    return 0


if __name__ == "__main__":
    sys.exit(main())
