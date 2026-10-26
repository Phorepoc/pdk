# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""theme_gen -- compile the complete 2010 SVG themes, Haze (2000B12D) and Jing (2000B12E), as skin_gen.py does the
default skin.

The default skin's artwork (~2400 SVGs under /epoc32/S60/Icons/) was never in the open release, so it is built from the
Carbide theme-model templates. Haze and Jing are self-contained (descriptor + ~1700 SVGs + bitmaps in their srcdata
dirs): the skin descriptor compiler over <name>.txt, then mifconv over the generated <name>_MIFList.txt -- what
haze.mk / jing.mk do. Descriptor paths are Windows paths; every BITMAPPATH gets forward slashes for the Linux compiler.

    python3 tools/theme_gen.py [haze] [jing]
Output: BUILD/skin-gen/<name>/data/<name>.{skn,mif,mbm}; x32_rom_args.py stages them to
Z:/private/10207114/import/<UID>/<name>.skn and Z:/resource/skins/<UID>/<name>.{mif,mbm}.
"""
import os
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD, SYMBIAN  # noqa: E402
import mif_gen  # noqa: E402
from skin_gen import COMPILER  # noqa: E402

SRCDATA = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.mw.uiresources", "skins", "AknSkinContent", "srcdata")
THEMES = {"haze": "2000B12D", "jing": "2000B12E"}


def gen(name: str) -> int:
    base = os.path.join(BUILD, "skin-gen", name)
    out = os.path.join(base, "data")
    shutil.rmtree(base, ignore_errors=True)
    os.makedirs(out)
    # the compiler and the 32-bit mifconv both work in a local temp copy (mifconv cannot open files on a bind mount
    # with 64-bit inodes)
    tmp = tempfile.mkdtemp()
    stage = os.path.join(tmp, "src")
    shutil.copytree(os.path.join(SRCDATA, name), stage, ignore=shutil.ignore_patterns("*.zip"))
    desc = os.path.join(stage, name + ".txt")
    with open(desc, "rb") as fh:
        raw = fh.read()
    enc = "utf-16" if raw[:2] in (b"\xff\xfe", b"\xfe\xff") else "latin-1"
    text = raw.decode(enc)
    text = re.sub(r"(?im)^(\s*BITMAPPATH\s+)(\S+)", lambda m: m.group(1) + m.group(2).replace("\\", "/"), text)
    with open(desc, "wb") as fh:
        fh.write(text.encode(enc))
    r = subprocess.run([COMPILER, "-m" + out, "-t" + out, name + ".txt", name], cwd=stage,
                       capture_output=True, text=True, errors="replace")
    print(f"{name}: skin compiler dropped {r.stdout.count('not add beacuse')} item graphics")
    if r.returncode or not os.path.isfile(os.path.join(out, name + ".skn")):
        print((r.stdout + r.stderr)[-600:])
        return 1
    shutil.copy(mif_gen.PALETTE, os.path.join(tmp, "palette.pal"))
    with open(os.path.join(out, name + "_MIFList.txt"), encoding="latin-1") as fh:
        lst = re.sub(r"(?m)(^|\s)/(?=\w)", lambda m: m.group(1) + "-", fh.read()).replace("\\", "/")
    with open(os.path.join(tmp, "list.txt"), "w", encoding="latin-1", newline="\n") as fh:
        fh.write(lst)
    r = subprocess.run([mif_gen.MIFCONV, os.path.join(tmp, name + ".mif"), "-T" + tmp,
                        "-P" + os.path.join(tmp, "palette.pal"), "-S" + os.path.dirname(mif_gen.SVGTENC),
                        "-F" + os.path.join(tmp, "list.txt")],
                       cwd=stage, capture_output=True, text=True, errors="replace")
    if r.returncode or not os.path.isfile(os.path.join(tmp, name + ".mif")):
        lines = [ln for ln in (r.stdout + r.stderr).splitlines() if not ln.startswith(("Loading", "Error deleting"))]
        print(f"{name}: mifconv failed: " + "\n".join(lines[-8:]))
        return 1
    for ext in (".mif", ".mbm"):
        if os.path.isfile(os.path.join(tmp, name + ext)):
            shutil.copy(os.path.join(tmp, name + ext), out)
    shutil.rmtree(tmp, ignore_errors=True)
    for f in sorted(os.listdir(out)):
        if f.startswith(name + "."):
            print(f"  {f}: {os.path.getsize(os.path.join(out, f))} B")
    return 0


def main() -> int:
    names = sys.argv[1:] or sorted(THEMES)
    return max((gen(n) for n in names), default=0)


if __name__ == "__main__":
    sys.exit(main())
