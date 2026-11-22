# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

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
from paths import BUILD, SYMBIAN, layer_dir  # noqa: E402
import mif_gen  # noqa: E402
from skin_gen import COMPILER  # noqa: E402

SRCDATA = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.mw.uiresources", "skins", "AknSkinContent", "srcdata")
THEMES = {"haze": "2000B12D", "jing": "2000B12E"}


_SHAPE = re.compile(r"<(rect|path|polygon|polyline|circle|ellipse)\b([^>]*?)(/?)>")


def fill_unfilled(stage: str, prefix: str) -> int:
    """Give the shapes of one frame's SVGs (prefix = the frame's file-name stem) that have no fill the fill the frame's
    own pieces use. In the staged copy only: the source tree stays as released.

    Why: Haze's calculator paper (qsn_fr_calc_paper_center/corner_*/side_*) draws each piece as a <rect> with no fill
    attribute plus a 1-px highlight with fill="#ffffff" fill-opacity="0.6". SVG's default fill is black, so the paper
    rendered as a black box. The fill is taken from the frame's own explicit (non-gradient) fills, so it matches its
    frame; no colour is invented. Returns the number of shapes changed."""
    files = sorted(f for f in os.listdir(stage) if f.startswith(prefix) and f.endswith(".svg"))
    fill = None
    for f in files:
        with open(os.path.join(stage, f), encoding="utf-8", errors="replace") as fh:
            for m in _SHAPE.finditer(fh.read()):
                c = re.search(r'\sfill="(#[0-9a-fA-F]{3,6})"', m.group(2))
                if c:
                    o = re.search(r'\sfill-opacity="([0-9.]+)"', m.group(2))
                    fill = (c.group(1), o.group(1) if o else None)
                    break
        if fill:
            break
    if not fill:
        return 0
    attrs = ' fill="%s"' % fill[0] + (' fill-opacity="%s"' % fill[1] if fill[1] else "")
    changed = 0
    for f in files:
        p = os.path.join(stage, f)
        with open(p, encoding="utf-8", errors="replace") as fh:
            svg = fh.read()

        def add(m):
            nonlocal changed
            if re.search(r"\sfill=|fill\s*:", m.group(2)):
                return m.group(0)
            changed += 1
            return "<%s%s%s%s>" % (m.group(1), m.group(2), attrs, m.group(3))
        new = _SHAPE.sub(add, svg)
        if new != svg:
            with open(p, "w", encoding="utf-8", newline="") as fh:
                fh.write(new)
    print("theme_gen: %s*: %d unfilled shapes -> fill %s" % (prefix, changed, attrs.strip()))
    return changed


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
    # Nokia built on Windows: the descriptor names lay_ah_*.svg, the files are LAY_AH_*.svg (167 Haze items dropped on
    # a case-sensitive file system). Every file and every file reference lower case.
    for dp, _, fns in os.walk(stage):
        for fn in fns:
            if fn != fn.lower():
                os.rename(os.path.join(dp, fn), os.path.join(dp, fn.lower()))
    text = re.sub(r"[\w\-]+\.(?:svg|bmp)\b", lambda m: m.group(0).lower(), text, flags=re.I)
    fill_unfilled(stage, "qsn_fr_calc_paper_")
    desc = os.path.join(stage, name + ".txt")
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


def gen_etesian() -> int:
    """The Etesian themes from the etesian-theme layer's own tools/build_skin.py (the same compiler and mifconv): "etesian" (the Hybrid colour
    mode, UID E7016B33) and "etesian-dark" (E7016B31), each into BUILD/skin-gen/<name>/data/<name>.{skn,mif,mbm}. A build without that layer has none."""
    layer = layer_dir("etesian-theme")
    if not layer:
        print("etesian: no etesian-theme layer in this build, skipped")
        return 0
    env = dict(os.environ, PHE_PALETTE=mif_gen.PALETTE, PHE_SVGTENC=mif_gen.SVGTENC)
    rc = 0
    for name in ("etesian", "etesian-dark"):
        if not os.path.isfile(os.path.join(layer, "skin", name + ".txt")):
            continue
        out = os.path.join(BUILD, "skin-gen", name, "data")
        shutil.rmtree(os.path.dirname(out), ignore_errors=True)
        r = subprocess.run([sys.executable, os.path.join(layer, "tools", "build_skin.py"), name, out], env=env,
                           capture_output=True, text=True, errors="replace")
        ok = r.returncode == 0 and all(os.path.isfile(os.path.join(out, name + e)) for e in (".skn", ".mif"))
        print(f"{name}: {'built' if ok else 'FAILED'} (etesian-theme layer {layer})")
        if not ok:
            print((r.stdout + r.stderr)[-800:])
            rc = 1
    return rc


def main() -> int:
    names = sys.argv[1:] or sorted(THEMES) + ["etesian"]
    return max((gen_etesian() if n == "etesian" else gen(n) for n in names), default=0)


if __name__ == "__main__":
    sys.exit(main())
