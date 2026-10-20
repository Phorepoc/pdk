# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_efsrv_x32 -- efsrv.dll (the F32 CLIENT library) as an x32 E32Image.

The first x32 DLL that IMPORTS: euser.dll had nothing below it,
efsrv sits on top of it and binds 135 of its ordinals. That is what makes it the image that
proves the builder is a DLL builder and not a euser builder -- and it is a prerequisite for
EFILE, which imports 39 efsrv ordinals.

Pure client-side C++ (RFs/RFile/RDir/TParse...): no asm, no arch branch, nothing needing an
amd64 sibling. The whole build shape lives in x32_client_dll.py; this is its configuration.

    python3 tools/emitter/build_efsrv_x32.py [out.dll]   (default BUILD/x32core/efsrv_x32.dll; SKIP_CENSUS=1 reuses objects)
"""
from __future__ import annotations

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import x32_client_dll as CD                     # noqa: E402
from paths import resolve                       # noqa: E402

FS = os.path.join(resolve("baseline/kernelhwsrv"), "userlibandfileserver", "fileserver")

CFG = CD.Cfg(name="efsrv",
             def_path=os.path.join(FS, "eabi", "efsrvu.def"),
             uid2=0x1000008D, uid3=0x100039E4,      # efsrv.mmh: uid 0x1000008d 0x100039e4
             census_marker="=== efsrv census")


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(CD.EU.CORE_OUT, "efsrv_x32.dll")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    raise SystemExit(CD.build(CFG, out))
