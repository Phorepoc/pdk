"""mif_gen -- build S60 icon files (.mif) with the REAL mifconv.

What the `START EXTENSION s60/mifconv` build step does (the same bld.inf blocks
tools/mbg.py reads for the .mbg headers): run mifconv over the icon list ->
Z:/resource/apps/<target>.mif. mifconv is the release's own tool (the toolchain image carries it with
svgtbinencode on PATH); SVGs are encoded by the shipped svgtbinencode.

Artwork: the platform icon directories (/epoc32/s60/icons) were a separate delivery that is
NOT part of the 2010 open release. What the release does carry is searched in order:
    the component's own dirs, Carbide.ui's Symbian3 + S60 5.0 theme models, the haze/jing
    skin sources
and every icon still missing gets OUR placeholder -- a valid, fully transparent SVG-Tiny
document -- so that every icon id in the generated .mbg resolves to an entry (a missing
.mif makes AknIconUtils::CreateIconL leave KErrNotFound and takes the UI server down).
The per-target report says how many icons are real vs placeholder.

Vector icons need a renderer at run time: the shipped svgtbinencode emits NVG, which
akniconsrv hands to nvgdecoder (-> OpenVG); raw SVG (-X) needs the 110-TU SVGEngine. Both
paths run now, so VECTOR (the real SVG artwork above) is the DEFAULT.
`--bitmap` = the old bitmap-only mode, for a ROM without the VG stack: EVERY icon is OUR
placeholder .bmp (transparent mask; the text cursors are a visible bar), which mifconv packs into
<target>.mbm + a .mif index that akniconsrv serves through plain FBS bitmaps. (Those placeholders
show as grey 8x8 squares wherever an icon is drawn without its mask.)
A bld.inf that #includes another one (Phonebook2/group -> its plug-ins): a SOURCEFILE is resolved
in the INCLUDING dir, so an included block with the same list name picks up the wrong list. Run
mif_gen on the included bld.inf's own dir afterwards (pbk2rclengine/callplugin/sendplugin).

    python3 tools/mif_gen.py                       # avkon2.mif (+ the other AvKon targets)
    python3 tools/mif_gen.py <dir containing bld.inf> ...
    python3 tools/mif_gen.py @$PHOREPOC_CORE/registry/mif_dirs.txt   # a list of dirs
Output: BUILD/icon-gen/<target>.mif  (x32_rom_args.py / gui_rom_args.py stage them into the ROM)
"""
from __future__ import annotations

import os
import re
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD, resolve  # noqa: E402
import mbg as MBG  # noqa: E402
from mmp import _isfile_nocase  # noqa: E402

BASE = MBG.BASE
OUT = os.path.join(BUILD, "icon-gen")
MIFCONV = shutil.which("mifconv") or "mifconv"
SVGTENC = shutil.which("svgtbinencode") or "svgtbinencode"
PALETTE = os.path.join(BASE, "oss.FCL.sf.mw.uitools", "uitools_pub", "third_party_bitmap_palette_api",
                       "inc", "ThirdPartyBitmap.pal")   # the S60 256-colour palette (-P)
DEFAULT_DIRS = [os.path.join(BASE, "../symbian-rcl3/oss.FCL.sf.mw.classicui", "uifw", "AvKon", "group"),
                # variatedbitmaps.mif: qgn_prop_link_embd_* (KAvkonVariatedBitmapsFile); psln's General theme view
                # leaves KErrNotFound in activation without it (the view never opened)
                os.path.join(BASE, "../symbian-rcl3/oss.FCL.sf.mw.uiresources", "uigraphics", "VariatedBitmaps", "group")]
ART_DIRS = [
    os.path.join(BASE, "oss.FCL.sftools.dev.ui", "carbideui", "Symbian3", "config", "model"),
    os.path.join(BASE, "oss.FCL.sftools.dev.ui", "carbideui", "com.nokia.tools.theme.s60.50",
                 "config", "model"),
    os.path.join(BASE, "../symbian-rcl3/oss.FCL.sf.mw.uiresources", "skins", "AknSkinContent", "srcdata", "haze"),
    os.path.join(BASE, "../symbian-rcl3/oss.FCL.sf.mw.uiresources", "skins", "AknSkinContent", "srcdata", "jing"),
]
PLACEHOLDER = ('<?xml version="1.0" encoding="UTF-8"?>\n'
               '<!-- OURS: placeholder; this icon\'s artwork is not part of the 2010 open release -->\n'
               '<svg xmlns="http://www.w3.org/2000/svg" version="1.1" baseProfile="tiny" '
               'width="24" height="24" viewBox="0 0 24 24">\n'
               '<rect x="0" y="0" width="24" height="24" fill="none"/>\n</svg>\n')


VECTOR = True


def bmp24(w: int, h: int, rgb: tuple[int, int, int]) -> bytes:
    """An uncompressed 24-bit BMP of one colour."""
    import struct
    row = bytes(rgb[::-1]) * w
    row += bytes(-len(row) % 4)
    data = row * h
    return (b"BM" + struct.pack("<IHHI", 54 + len(data), 0, 0, 54)
            + struct.pack("<IiiHHIIiiII", 40, w, h, 1, 24, 0, len(data), 2835, 2835, 0, 0) + data)


def art_index(extra: list[str]) -> dict[str, str]:
    idx: dict[str, str] = {}
    for d in extra + ART_DIRS:
        if not os.path.isdir(d):
            continue
        for root, _dirs, files in os.walk(d):
            for f in files:
                stem, ext = os.path.splitext(f.lower())
                if ext == ".svg":
                    idx.setdefault(stem, os.path.join(root, f))
    # artwork the release lacks under the name a bld.inf asks for, but carries under a sibling name
    for want, have in ART_ALIASES.items():
        if want not in idx and have in idx:
            idx[want] = idx[have]
    return idx


# the browser's app icon (browserng_aif.mif: "-c8,8 qgn_menu_browserng") -> Carbide's
# qgn_menu_browser (without it the "Web" grid item was an empty square)
ART_ALIASES = {"qgn_menu_browserng": "qgn_menu_browser"}


# The Carbide.ui theme-model templates draw frame slices with 1-px guide strokes (#006400 normal, #00386D
# pressed) so a designer sees how a 9-slice frame is cut; on screen they showed as "wireframe" boxes inside every
# softkey and control button. Staged without them, the slices are plain fills.
_GUIDE = re.compile(r'\s*<(?:rect|polygon)\b[^>]*fill="#(?:006400|00386D)"[^>]*/>', re.I)


def copy_art(src: str, dst: str, stem: str) -> bool:
    """Stage one artwork SVG; frame slices (qgn_fr_*) lose the template's guide strokes. -> stripped?"""
    shutil.copyfile(src, dst)
    if not stem.startswith("qgn_fr_"):
        return False
    with open(dst, encoding="utf-8", errors="surrogateescape") as fh:
        svg = fh.read()
    clean, n = _GUIDE.subn("", svg)
    if n:
        with open(dst, "w", encoding="utf-8", errors="surrogateescape", newline="") as fh:
            fh.write(clean)
    return bool(n)


def build(target: str, tokens: list[str], comp_dir: str) -> bool:
    stage = os.path.join(OUT, os.path.splitext(target)[0] + "-src")
    shutil.rmtree(stage, ignore_errors=True)
    os.makedirs(stage)
    idx = art_index([os.path.dirname(comp_dir)])
    params, real, fake, depth = [], 0, 0, "/c8,8"
    for t in tokens:
        if MBG.DEPTH.match(t):
            depth = t
            continue
        if t.startswith(("-", "/")) and not re.search(r"\.(svg|bmp)$", t, re.I):
            continue
        stem = re.split(r"[\\/]", t)[-1].rsplit(".", 1)[0].lower()
        if not VECTOR:
            # OUR bitmap placeholder: black mask = fully transparent; cursors = a visible bar
            cursor = "cursor" in stem
            with open(os.path.join(stage, stem + ".bmp"), "wb") as fh:
                fh.write(bmp24(2, 20, (255, 255, 255)) if cursor else bmp24(8, 8, (128, 128, 128)))
            if "," in depth:
                suffix = "_mask_soft" if depth.endswith(",8") else "_mask"
                with open(os.path.join(stage, stem + suffix + ".bmp"), "wb") as fh:
                    fh.write(bmp24(2, 20, (255, 255, 255)) if cursor else bmp24(8, 8, (0, 0, 0)))
            fake += 1
            params.append(f"{depth} {stem}.bmp")
            continue
        dst = os.path.join(stage, stem + ".svg")
        if stem in idx:
            copy_art(idx[stem], dst, stem)
            real += 1
        else:
            with open(dst, "w", encoding="utf-8", newline="\n") as fh:
                fh.write(PLACEHOLDER)
            fake += 1
        params.append(f"{depth} {stem}.svg")
    pfile = os.path.join(stage, "_params.txt")
    with open(pfile, "w", newline="\n") as fh:
        fh.write("\n".join(params) + "\n")
    mif = os.path.join(OUT, target)
    for stale in (mif, os.path.splitext(mif)[0] + ".mbm"):
        if os.path.isfile(stale):
            os.remove(stale)
    # /I /T relative (cwd = stage): with absolute ones, long icon names (peninputgenericvkb's 53-char
    # qgn_indi_fep_button_case_numeric_arabic_indic_sel) overflowed the SVG encoder's temp paths
    # ("Error deleting file (svg conversion)")
    r = subprocess.run([MIFCONV, mif, "/E", "/I.", "/T.", "/S" + os.path.dirname(SVGTENC),
                        "/P" + PALETTE, "/F" + pfile],
                       cwd=stage, capture_output=True, text=True, errors="replace")
    ok = r.returncode == 0 and os.path.isfile(mif)
    size = os.path.getsize(mif) if ok else 0
    print(f"  {target}: {real} real + {fake} placeholder icons -> {size} B" if ok else
          f"  ! {target}: mifconv failed: {(r.stdout + r.stderr).strip()[-400:]}")
    return ok


def run(dirs: list[str]) -> int:
    if not shutil.which(MIFCONV):
        print("mifconv not found on PATH (the toolchain image carries it)")
        return 1
    os.makedirs(OUT, exist_ok=True)
    bad = 0
    for d in dirs:
        inf = [f for f in os.listdir(d) if f.lower() == "bld.inf"]
        if not inf:
            print(f"  ! no bld.inf in {d}")
            bad += 1
            continue
        text = MBG.inf_text(os.path.join(d, inf[0]))
        seen = set()
        # icon MAKEFILES in the same dir (the older S60 form, pushmtm/Group/PushMtmIcons.mk)
        for mk in sorted(f for f in os.listdir(d) if f.lower().endswith(".mk")):
            for mif, _hdr, toks in MBG.mk_mifconv(os.path.join(d, mk)):
                if mif.lower() not in seen:
                    seen.add(mif.lower())
                    bad += 0 if build(mif.lower(), toks, d) else 1
        for blk in MBG.BLOCK.findall(text):
            opts = MBG.block_options(blk)
            target = opts.get("TARGETFILE", "").strip().lower()
            if not target or target in seen:
                continue
            seen.add(target)
            if "SOURCEFILE" in opts:
                # relative to the bld.inf's dir, any case (mmsui: ../data/MmsUi_IconList.txt)
                src = _isfile_nocase(os.path.join(d, opts["SOURCEFILE"].strip().replace("\\", "/")))
                if not src:
                    print(f"  ! {target}: list {opts['SOURCEFILE']} missing")
                    bad += 1
                    continue
                toks = open(src, errors="replace").read().split()
            else:
                toks = opts.get("SOURCES", "").split()
            bad += 0 if build(target, toks, d) else 1
    return 1 if bad else 0


def mmp_bitmaps(mmp: str) -> int:
    """An mmp's `START BITMAP <x>.mbm ... END` blocks (bmconv, not mifconv): SOURCE <depth> <bmp>...
    lines under SOURCEPATHs relative to the mmp. mifconv packs .bmp sources into an .mbm with the
    same one-entry-per-file order bmconv uses (the mask is its own entry: tools/mbg.py's header)."""
    text = open(mmp, errors="replace").read()
    bad = 0
    for mbm, blk in MBG.BITMAP.findall(text):
        if not re.match(r"^[\w.-]+$", mbm):
            continue
        spath, args = os.path.dirname(mmp), []
        for line in blk.splitlines():
            m = re.match(r"\s*SOURCEPATH\s+(\S+)", line, re.I)
            if m:
                spath = os.path.normpath(os.path.join(os.path.dirname(mmp), m.group(1).replace("\\", "/")))
                continue
            m = re.match(r"\s*SOURCE\s+(\S+)\s+(.*)", line, re.I)
            if m:
                for f in m.group(2).split():
                    src = _isfile_nocase(os.path.join(spath, f.replace("\\", "/")))
                    if not src:
                        print(f"  ! {mbm}: {f} missing")
                        bad += 1
                        continue
                    args += ["-" + m.group(1).lower(), os.path.relpath(src, OUT)]
        if not args:
            continue
        r = subprocess.run([MIFCONV, mbm.lower(), "-P" + os.path.relpath(PALETTE, OUT), *args],
                           cwd=OUT, capture_output=True, text=True)
        mif = os.path.join(OUT, os.path.splitext(mbm.lower())[0] + ".mif")
        if os.path.exists(mif) and os.path.getsize(mif) < 64:   # mifconv's empty index beside it
            os.remove(mif)
        ok = r.returncode == 0
        bad += 0 if ok else 1
        print(f"  {mbm.lower()}: {len(args) // 2} bitmaps" + ("" if ok else " FAILED " + r.stdout[-200:]))
    return bad


if __name__ == "__main__":
    args = sys.argv[1:]
    VECTOR = "--bitmap" not in args
    args = [a for a in args if a not in ("--vector", "--bitmap")]
    # @list: dirs one per line (registry-style paths, see paths.resolve; # comments), e.g. @$PHOREPOC_CORE/registry/mif_dirs.txt
    expanded = []
    for a in args:
        if a.startswith("@"):
            lst = a[1:] if os.path.isabs(a[1:]) else os.path.join(os.path.dirname(HERE), a[1:])
            for line in open(lst, encoding="utf-8"):
                line = line.split("#", 1)[0].strip()
                if line:
                    expanded.append(resolve(line))
        else:
            expanded.append(a)
    # an .mmp argument (or @list line) = its START BITMAP targets (mmp_bitmaps), a dir = its bld.inf
    mmps = [a for a in expanded if a.lower().endswith(".mmp")]
    args = [a for a in expanded if a not in mmps]
    os.makedirs(OUT, exist_ok=True)
    bad = sum(mmp_bitmaps(m) for m in mmps)
    rc = run(args or DEFAULT_DIRS) if args or not mmps else 0
    sys.exit(1 if bad or rc else 0)
