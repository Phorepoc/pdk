"""mif_all -- generate the icon files (.mif) of EVERY registered component whose bld.inf declares them (mifconv), so the
ROM never ships a component without its icons (without wsficons.mif the WLAN app died, without
gsaccplugin/gssenplugin.mif Control Panel > Device crashed in CArrayPtr<CGulIcon>::ResetAndDestroy).

    python3 tools/mif_all.py          # only the .mif targets not yet in BUILD/icon-gen/ (tools/mif_gen.py does the work)
    python3 tools/mif_all.py --all    # regenerate all of them

Run by tools/make_phorepoc_rom.sh before the ROM recipe (MIF_ALL=0 = off).
"""
from __future__ import annotations

import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import SYMBIAN, BUILD, EPOC32_ROOT, resolve  # noqa: E402
import components as C  # noqa: E402

ICON_GEN = os.path.join(BUILD, "icon-gen")

BLOCK = re.compile(r"START\s+EXTENSION\s+\S*mifconv\b(.*?)^\s*END\b", re.I | re.S | re.M)
TARGET = re.compile(r"OPTION\s+TARGETFILE\s+(\S+)", re.I)


def group_dirs() -> dict[str, list[str]]:
    """bld.inf dir -> the .mif targets it declares, for every registered component"""
    out: dict[str, list[str]] = {}
    for comp in C.COMPONENTS.values():
        mmp = comp.get("mmp") if isinstance(comp, dict) else None
        if not mmp:
            continue
        # mif_dirs: bld.inf dirs OUTSIDE the component's own group (Photos: photosgallery/icons holds glx_aif.mif, the app icon)
        for xd in (comp.get("mif_dirs") or []):
            xdir = os.path.normpath(os.path.join(SYMBIAN, "symbian-2010", xd))
            xinf = os.path.join(xdir, "bld.inf")
            if xdir not in out and os.path.isfile(xinf):
                xt = open(xinf, encoding="utf-8", errors="replace").read()
                xts = [t.lower() for blk in BLOCK.findall(xt) for t in TARGET.findall(blk) if t.lower().endswith(".mif")]
                if xts:
                    out[xdir] = xts
        path = mmp if os.path.isabs(mmp) else resolve(mmp) if mmp.startswith("baseline/") else os.path.normpath(os.path.join(SYMBIAN, "symbian-2010", mmp))
        inf = os.path.join(os.path.dirname(path), "bld.inf")
        d = os.path.dirname(path)
        if d in out or not os.path.isfile(inf):
            continue
        text = open(inf, encoding="utf-8", errors="replace").read()
        targets = [t.lower() for blk in BLOCK.findall(text) for t in TARGET.findall(blk) if t.lower().endswith(".mif")]
        if targets:
            out[d] = targets
    return out


def listed_dirs() -> list[str]:
    """mif_gen's default dirs (AvKon, VariatedBitmaps), then core's registry/mif_dirs.txt, in their order: the
    icon targets of the ROM that no registered component's own group declares (Phonebook2's #included bld.infs
    must come after it)"""
    sys.path.insert(0, HERE)
    import mif_gen
    out = [os.path.normpath(d) for d in mif_gen.DEFAULT_DIRS]
    lst = os.path.join(os.path.dirname(C.__file__), "mif_dirs.txt")
    if os.path.isfile(lst):
        for line in open(lst, encoding="utf-8"):
            line = line.split("#", 1)[0].strip()
            if line:
                out.append(os.path.normpath(resolve(line)))
    return out


def main() -> None:
    # bld.infs with #ifdef'd icon blocks are read through cpp + the variant headers `ph source export` writes; without
    # them those targets silently vanish (the platform macros are all undefined)
    if not os.path.isfile(os.path.join(EPOC32_ROOT, "include", "bldvariant.hrh")):
        sys.exit(f"mif_all: {EPOC32_ROOT}/include has no bldvariant.hrh -- run `ph source export` first")
    have = {f.lower() for f in os.listdir(ICON_GEN)} if os.path.isdir(ICON_GEN) else set()
    dirs = group_dirs()
    listed = []
    for d in listed_dirs():
        if d.lower().endswith(".mmp"):          # an .mmp line: its START BITMAP .mbm files (always rebuilt, they are quick)
            if os.path.isfile(d):
                listed.append(d)
                dirs.setdefault(d, [])
            continue
        inf = next((f for f in (os.listdir(d) if os.path.isdir(d) else []) if f.lower() == "bld.inf"), None)
        if not inf:
            continue
        listed.append(d)
        text = open(os.path.join(d, inf), encoding="utf-8", errors="replace").read()
        dirs.setdefault(d, [t.lower() for blk in BLOCK.findall(text) for t in TARGET.findall(blk)
                            if t.lower().endswith(".mif")])
    want = lambda d: "--all" in sys.argv or not dirs[d] or any(t not in have for t in dirs[d])   # noqa: E731
    todo = [d for d in listed if want(d)]
    todo += sorted(d for d in dirs if d not in listed and want(d))
    print(f"mif_all: {len(dirs)} component groups declare icons, {len(todo)} to generate")
    if todo:
        subprocess.run([sys.executable, os.path.join(HERE, "mif_gen.py")] + todo, check=False)


if __name__ == "__main__":
    main()
