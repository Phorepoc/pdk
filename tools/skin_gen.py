"""skin_gen -- compile the S60 default skin package (series60skin, PID 101f84b9).

The skin server (aknskinsrv) only initialises its shared item-def chunk when it merges a skin package; with no default
skin on Z: every client faults reading an uninitialised chunk header. The release's build
(skins/AknSkinContent/group/scalableseries60skin.mk) runs the skin descriptor compiler over the skin descriptor:

    akn_skin_desc_compiler -m<gen> -t<gen> scalableseries60skin.txt series60skin

The toolchain image carries the compiler, built on Linux from its 2010 source (the release ships it for Windows only).
Then the .mk's RESOURCE step: mifconv over the generated series60skin_MIFList.txt -> series60skin.mif + .mbm.

Artwork: the descriptor points ~2400 SVG references at /epoc32/S60/Icons/ -- the platform icon delivery, NOT part of
the 2010 open release (the compiler drops such an item: "not add beacuse file not exist"). We compile a COPY of the
descriptor whose /epoc32/... BITMAPPATHs point at the staged icons/, holding whatever the release does carry
(mif_gen.art_index). The descriptor writes Windows paths; every BITMAPPATH is turned into forward slashes, which the
Linux compiler needs.

    python3 tools/skin_gen.py
Output: BUILD/skin-gen/data/series60skin.skn (Z:/private/10207114/import/101f84b9/) + series60skin.mif/.mbm
(Z:/resource/skins/101f84b9/); x32_rom_args.py / gui_rom_args.py stage them into the ROM.
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

UIRES = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.mw.uiresources")
SRC = os.path.join(UIRES, "skins", "AknSkinContent", "srcdata", "scalableseries60skin")
GEN = os.path.join(BUILD, "skin-gen", "data")
STAGE = os.path.join(BUILD, "skin-gen", "src")
COMPILER = shutil.which("akn_skin_desc_compiler") or "akn_skin_desc_compiler"


def stage_sources() -> None:
    """STAGE = descriptor copy (BITMAPPATH /epoc32/... -> icons/, all slashes forward) + gfx dirs + found artwork."""
    shutil.rmtree(STAGE, ignore_errors=True)
    os.makedirs(os.path.join(STAGE, "icons"))
    for d in ("gfx", "gfx-ah"):
        shutil.copytree(os.path.join(SRC, d), os.path.join(STAGE, d))
    with open(os.path.join(SRC, "scalableseries60skin.txt"), "rb") as fh:
        text = fh.read().decode("utf-16")
    text = re.sub(r"(?im)^(\s*BITMAPPATH\s+)[\\/]epoc32[\\/]\S*", r"\g<1>icons/", text)
    text = re.sub(r"(?im)^(\s*BITMAPPATH\s+)(\S+)", lambda m: m.group(1) + m.group(2).replace("\\", "/"), text)
    idx = mif_gen.art_index([os.path.dirname(SRC)])
    want = {m.lower() for m in re.findall(r"([\w\-]+)\.svg", text, re.I)}
    found = set()
    stripped = 0
    for stem in sorted(want):
        if stem in idx:
            # frame slices lose the templates' 1-px guide strokes (mif_gen.copy_art)
            stripped += mif_gen.copy_art(idx[stem], os.path.join(STAGE, "icons", stem + ".svg"), stem)
            found.add(stem)
    # A one-line BITMAP item whose SVG is not in the open release: the compiler drops the graphic but KEEPS the item,
    # bound to a wrong bitmap -- and an app asking the skin first (AknsUtils::CreateIconL with its own MIF as the
    # fallback) then gets that bitmap instead of its own icon. Leave such items out: the app's MIF answers.
    left_out = []

    def keep(m):
        if m.group(2).lower() in found:
            return m.group(1)
        left_out.append(m.group(2))
        return ""
    text = re.sub(r"(?im)^([ \t]*BITMAP[ \t]+IID=\S+[^\n]*?([\w\-]+)\.svg[^\n]*\n)", keep, text)
    with open(os.path.join(STAGE, "scalableseries60skin.txt"), "wb") as fh:
        fh.write(text.encode("utf-16"))
    print(f"skin artwork: template guide strokes removed from {stripped} frame slices")
    print(f"skin artwork: {len(found)} of {len(want)} referenced SVGs found in the open release")
    print(f"skin items: {len(left_out)} one-line BITMAP items without artwork left out (their apps' own MIFs answer)")


def main() -> int:
    os.makedirs(GEN, exist_ok=True)
    stage_sources()
    r = subprocess.run([COMPILER, "-m" + GEN, "-t" + GEN, "scalableseries60skin.txt", "series60skin"],
                       cwd=STAGE, capture_output=True, text=True, errors="replace")
    print(f"skin compiler: {r.stdout.count('not add beacuse')} item graphics dropped (artwork not in the open release)")
    skn = os.path.join(GEN, "series60skin.skn")
    if r.returncode or not os.path.isfile(skn):
        print((r.stdout + r.stderr)[-600:])
        return 1
    print(f"series60skin.skn: {os.path.getsize(skn)} B")
    # the .mk's RESOURCE step: the skin's own bitmaps (gfx/, gfx-ah/). Without them every skinned-icon lookup is a
    # failed OpenBitmapFile IPC.
    # mifconv runs in a local temp copy of the staged sources: the 32-bit mifconv cannot open files on a bind mount
    # with 64-bit inodes. The compiler writes Windows switches (/E, /c16,8), which mifconv on Linux takes only as -.
    tmp = tempfile.mkdtemp()
    work = os.path.join(tmp, "src")
    shutil.copytree(STAGE, work)
    shutil.copy(mif_gen.PALETTE, os.path.join(tmp, "palette.pal"))
    with open(os.path.join(GEN, "series60skin_MIFList.txt"), encoding="latin-1") as fh:
        text = re.sub(r"(?m)(^|\s)/(?=\w)", lambda m: m.group(1) + "-", fh.read())
    with open(os.path.join(tmp, "list.txt"), "w", encoding="latin-1", newline="\n") as fh:
        fh.write(text)
    r = subprocess.run([mif_gen.MIFCONV, os.path.join(tmp, "series60skin.mif"), "-T" + tmp,
                        "-P" + os.path.join(tmp, "palette.pal"), "-S" + os.path.dirname(mif_gen.SVGTENC),
                        "-F" + os.path.join(tmp, "list.txt")],
                       cwd=work, capture_output=True, text=True, errors="replace")
    mif = os.path.join(tmp, "series60skin.mif")
    if r.returncode or not os.path.isfile(mif):
        out = [ln for ln in (r.stdout + r.stderr).splitlines() if not ln.startswith(("Loading", "Error deleting"))]
        print("mifconv failed: " + "\n".join(out[-8:]))
        return 1
    for ext in (".mif", ".mbm"):
        if os.path.isfile(os.path.join(tmp, "series60skin" + ext)):
            shutil.copy(os.path.join(tmp, "series60skin" + ext), GEN)
    shutil.rmtree(tmp, ignore_errors=True)
    print(f"series60skin.mif: {os.path.getsize(os.path.join(GEN, 'series60skin.mif'))} B")
    return 0


if __name__ == "__main__":
    sys.exit(main())
