"""stale_stubs -- images linked BEFORE a provider of theirs existed.

    python3 tools/stale_stubs.py        # lists them; relink with build_mmp_x32.py <key> ...

build_mmp_x32.py turns the imports of a LIBRARY with no built provider into link stubs (__unimpl_export -> panic
EUSRNIMP when called) and records those libraries in <image>.stubs. When the provider is registered and built
LATER, the earlier image still calls the stub -- Control Panel died that way (the GS Call plug-in, linked before
psui existed). This lists every image whose .stubs names a library that now has a built image.
Images built before .stubs existed are not covered (their record is missing: relink them once, or run --all).
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD  # noqa: E402
import components as C  # noqa: E402
import mmp as MMP  # noqa: E402

IMG = os.path.join(BUILD, "x32img")
# PHOREPOC_S4=1: the Symbian^4 keys' images live in their own tree; a provider may be in either
IMG_S4 = os.path.join(BUILD, "qt48" if os.environ.get("PHOREPOC_QT48") == "1" else "s4", "x32img")
S4_KEYS = set(__import__("components_s4").COMPONENTS) if os.environ.get("PHOREPOC_S4") == "1" else set()


def built_rom(stem: str):
    """(image dir, rom name) of a built component, else None."""
    if stem not in C.COMPONENTS:
        return None
    comp = C.COMPONENTS[stem]
    try:
        m = MMP.parse(os.path.join(MMP.BASE, comp.get("x32_mmp", comp["mmp"])))
    except Exception:
        return None
    rom = C.rom_name(stem, m.targettype)
    d = IMG_S4 if stem in S4_KEYS else IMG
    return (d, rom) if os.path.isfile(os.path.join(d, rom)) else None


def main() -> int:
    stale, missing = [], 0
    for key in C.COMPONENTS:
        built = built_rom(key)
        if not built:
            continue
        rec = os.path.join(built[0], built[1] + ".stubs")
        if not os.path.isfile(rec):
            missing += 1
            continue
        libs = [l for l in open(rec).read().split() if l]
        now = [l for l in libs if built_rom(l)]
        if now:
            stale.append((key, now))
    for key, libs in stale:
        print(f"{key}: relink -- {', '.join(libs)} now built")
    print(f"{len(stale)} stale image(s); {missing} built image(s) have no .stubs record yet")
    return 1 if stale else 0


if __name__ == "__main__":
    sys.exit(main())
