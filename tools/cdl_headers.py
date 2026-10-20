# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""cdl_headers -- generate the S60 CDL client headers that no bld.inf exports.

S60's layout, skin and icon customisation interfaces are declared in .cdl files. The original build ran CdlCompiler
in `client` mode to write <x>.cdl.h, .cdl.common.h and .cdl.common.hrh; most of them are exported pre-generated
(aknlayout2's generated_inc), but MifHeader.cdl (needed by AknIcon) and a few others are not.

CdlCompiler and its toolkit are in the 2010 source (sf.mw.uitools, EPL) with a Linux path already in it; this script
builds them for the host once (BUILD/host/cdlcompiler) and runs them. Existing headers are kept.

    python3 tools/cdl_headers.py

Run by `epoc32_export.py --all`.
"""
from __future__ import annotations

import glob
import os
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import SYMBIAN, BUILD, EPOC32_ROOT  # noqa: E402

UITOOLS = os.path.join(SYMBIAN, "symbian-2010", "oss.FCL.sf.mw.uitools")
UIRES = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.mw.uiresources")
AKNLAYOUT2 = os.path.join(UIRES, "layouts", "aknlayout2", "cdl")
CDLC = os.path.join(BUILD, "host", "cdlcompiler")
OUT = os.path.join(EPOC32_ROOT, "include", "platform", "mw")
CDLS = [
    os.path.join(UIRES, "uiresources_plat", "cdl_api", "inc", "UserPckg.cdl"),
    os.path.join(UIRES, "uiresources_plat", "layout_data_api", "inc", "LayoutPack.cdl"),
    os.path.join(UIRES, "uiresources_plat", "mif_header_cdl_api", "inc", "MifHeader.cdl"),
    os.path.join(AKNLAYOUT2, "CdlFont.cdl"),
    os.path.join(AKNLAYOUT2, "LayoutMetaData.cdl"),
    os.path.join(AKNLAYOUT2, "displaylayoutmetrics.cdl"),
    os.path.join(AKNLAYOUT2, "SkinLayout.cdl"),
]


def build_compiler() -> None:
    """CdlCompiler.cpp + the toolkit, unmodified apart from the tolower/toupper overload (modern <locale>
    overloads them, so std::transform cannot pick one)"""
    if os.path.isfile(CDLC):
        return
    os.makedirs(os.path.dirname(CDLC), exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        inc = os.path.join(tmp, "inc")
        shutil.copytree(os.path.join(UITOOLS, "uitools_plat", "cdl_api", "inc", "CdlCompilerToolkit"),
                        os.path.join(inc, "CdlCompilerToolkit"))
        srcs = []
        for src in [os.path.join(UITOOLS, "cdlcompiler", "src", "CdlCompiler.cpp")] + \
                sorted(glob.glob(os.path.join(UITOOLS, "cdlcompilertoolkit", "src", "*.cpp"))):
            text = open(src, encoding="latin-1").read()
            text = text.replace("r.begin(), tolower)", "r.begin(), ::tolower)")
            text = text.replace("r.begin(), toupper)", "r.begin(), ::toupper)")
            dst = os.path.join(tmp, os.path.basename(src))
            open(dst, "w", encoding="latin-1", newline="\n").write(text)
            srcs.append(dst)
        forced = [a for h in ("fstream", "iostream", "sstream", "cstring", "cstdlib", "algorithm", "cctype")
                  for a in ("-include", h)]
        subprocess.run(["g++", "-std=gnu++98", "-fpermissive", "-w", "-O1"] + forced +
                       ["-I" + inc, "-I" + os.path.join(UITOOLS, "cdlcompilertoolkit", "inc"),
                        "-I" + os.path.join(UIRES, "layouts", "cdl", "inc"), "-o", CDLC] + srcs, check=True)
    print(f"cdl_headers: built {CDLC}")


def main() -> None:
    build_compiler()
    os.makedirs(OUT, exist_ok=True)
    have = {f.lower() for f in os.listdir(OUT)}
    env = dict(os.environ, EPOCROOT="")           # the toolkit's Linux path asserts it is set
    for cdl in CDLS:
        with tempfile.TemporaryDirectory() as tmp:  # also the cwd: the compiler makes its temporary files there
            r = subprocess.run([CDLC, "client", "-p" + tmp + "/", cdl], capture_output=True, text=True,
                               errors="replace", cwd=tmp, env=env)
            got = [f for f in os.listdir(tmp) if f.lower().endswith((".h", ".hrh"))]
            new = [f for f in got if f.lower() not in have]
            for f in new:
                shutil.copyfile(os.path.join(tmp, f), os.path.join(OUT, f.lower()))
            print(f"cdl_headers: {os.path.basename(cdl)}: {len(got)} files, {len(new)} new"
                  + ("" if got else "  !! " + (r.stdout + r.stderr).strip()[:200]))


if __name__ == "__main__":
    main()
