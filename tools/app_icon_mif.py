# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

"""app_icon_mif -- one application icon (an SVG-Tiny file) into its .mif, with the release's mifconv + svgtbinencode.

    python3 tools/app_icon_mif.py <icon.svg> <out.mif>

The launcher draws an application icon at the grid's size: a .mif (vector, NVG-encoded) is rendered sharp at any size,
where a 64x64 .mbm was stretched and came out blurred on the larger grids. The source must be SVG-Tiny (no filter,
clipPath or group opacity; core's os/tools/icongen svg_tiny writes it). Same mifconv set-up as tools/mif_gen.py; inside
the toolchain container (mifconv on PATH).
"""
import os
import shutil
import subprocess
import sys
import tempfile

import mif_gen as MG


def main() -> int:
    if len(sys.argv) != 3:
        print(__doc__.strip(), file=sys.stderr)
        return 2
    src, out = os.path.abspath(sys.argv[1]), os.path.abspath(sys.argv[2])
    tmp = tempfile.mkdtemp(prefix="appmif-")   # local temp: the 32-bit mifconv cannot open files on 64-bit-inode mounts
    try:
        stage = os.path.join(tmp, "src")
        os.makedirs(stage)
        stem = os.path.splitext(os.path.basename(out))[0]
        shutil.copy(src, os.path.join(stage, stem + ".svg"))
        pfile = os.path.join(stage, "_params.txt")
        with open(pfile, "w", newline="\n") as fh:
            fh.write(f"{MG.switch('/c32,8')} {stem}.svg\n")
        mif = os.path.join(tmp, stem + ".mif")
        cmd = [MG.MIFCONV, mif, "-E", "-I.", "-T.", "-S" + os.path.dirname(MG.SVGTENC), "-F" + pfile]
        r = subprocess.run(cmd, cwd=stage, capture_output=True, text=True, errors="replace")
        if r.returncode != 0 or not os.path.isfile(mif):
            print(f"app_icon_mif: mifconv failed: {(r.stdout + r.stderr).strip()[-400:]}", file=sys.stderr)
            return 1
        shutil.copy(mif, out)
        print(f"app_icon_mif: {out} ({os.path.getsize(out)} B)")
        return 0
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
