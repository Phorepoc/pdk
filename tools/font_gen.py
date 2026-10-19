"""font_gen -- the S60 system faces, built from an open family.

    python3 tools/font_gen.py [outdir]          (default BUILD/font-gen/)

S60's font provider asks FBSERV for exact typeface names (fontsupport/fontprovider/Src/FontProvider.cpp): the
Primary category = "Series 60 Sans" with the BOLD style, Secondary = "Series 60 Sans" regular, Title = "Series 60
Sans TitleSmBd"; its height tables are tuned to the S60 system font's vertical metrics. With faces under other names FBSERV's
nearest match picks by generic attributes -- the wrong weight -- and FreeType fits each face's own ascent+descent
into the requested pixel height, so a face with a shorter vertical extent renders bigger (Ubuntu 1.121 em vs the
S60 face 1.213 em: ~8%).

This writes the three S60 system faces (nosnr60 / nssb60 / nstsb60), made from Ubuntu (third_party/fonts/
ubuntu, Ubuntu Font Licence: renamed derivatives are allowed and must not use the reserved name "Ubuntu"):
  * the names FontProvider asks for (family / subfamily / weight / style bits as the S60 faces declare them)
  * each S60 face's vertical extent AS A FRACTION OF THE EM (hhea + OS/2 win + typo metrics), so a pixel-height
    request yields the same glyph size as the S60 face
  * S60's private-use symbols (puacodes.hrh, U+F800..): Ubuntu uses that range for its own superscript / inferior
    digits, so the profile widget's "ringing" symbol U+F811 drew a small "7" . The derived faces
    drop Ubuntu's U+F800-F8FF, alias the codes that have a Unicode twin, and carry OUR drawn glyphs for the profile
    symbols (sound / silent / + vibra / timed) -- the original glyphs are not ours to ship.
The copyright / licence name records are kept. Needs fontTools (pip install fonttools).
"""
import math
import os
import sys

from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import TTFont

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import OS_ROOT, BUILD  # noqa: E402

SRC = os.path.join(OS_ROOT, "third_party", "fonts", "ubuntu")

# (output, source, family, subfamily, usWeightClass, bold, (ascent, descent) in em as the matching S60 face:
#  nosnr60 1983/501, nssb60 2087/501, nstsb60 2079/393 of 2048 units)
FACES = [
    ("s60sans_r.ttf", "Ubuntu-Regular.ttf", "Series 60 Sans", "Regular", 400, False, (1983 / 2048, 501 / 2048)),
    ("s60sans_sb.ttf", "Ubuntu-Medium.ttf", "Series 60 Sans", "SemiBold", 700, True, (2087 / 2048, 501 / 2048)),
    ("s60sans_tsb.ttf", "Ubuntu-Medium.ttf", "Series 60 Sans TitleSmBd", "Regular", 400, False, (2079 / 2048, 393 / 2048)),
]
KEEP_NAME_IDS = {0, 7, 8, 9, 10, 11, 12, 13, 14}      # copyright, trademark, designer, licence ...

# S60 private-use codes (platform/mw/puacodes.hrh) with a Unicode twin in the face: code -> that character
PUA_ALIASES = {0xF802: 0x2013,     # KPuaCodeNarrowDash       -> en dash
               0xF803: 0x260E,     # KPuaCodePhoneSymbol      -> black telephone (if the face has it)
               0xF80A: 0x002D,     # KPuaCodeShortHyphen      -> hyphen
               0xF80B: 0x2009,     # KPuaCodeShortSpace       -> thin space
               0xF812: 0x3000,     # KPuaCodeFullWidthSpace   -> ideographic space (if any), else below
               0xF813: 0x2665}     # KPuaCodeHeartSymbol      -> black heart suit
# the profile symbols Active Idle's profile publisher puts before the profile name (caiprofileplugin.cpp)
PUA_DRAWN = {0xF811: ("s60_sound", False, False),        # KPuaCodeAprofSound
             0xF810: ("s60_soundvibra", False, True),    # KPuaCodeAprofSoundVibra
             0xF807: ("s60_silent", True, False),        # KPuaCodeSilentSymbol
             0xF80F: ("s60_silentvibra", True, True),    # KPuaCodeAprofSilentVibra
             0xF815: ("s60_timed", None, None)}          # KAiTimedProfilePUA (timed profile: a clock)


def _poly(pen, pts) -> None:
    pen.moveTo(pts[0])
    for p in pts[1:]:
        pen.lineTo(p)
    pen.closePath()


def _bar(pen, x0, y0, x1, y1, w) -> None:
    """A straight stroke of width w from (x0,y0) to (x1,y1), as a closed quad."""
    dx, dy = x1 - x0, y1 - y0
    n = math.hypot(dx, dy) or 1
    ox, oy = -dy / n * w / 2, dx / n * w / 2
    _poly(pen, [(round(x0 + ox), round(y0 + oy)), (round(x0 - ox), round(y0 - oy)),
                (round(x1 - ox), round(y1 - oy)), (round(x1 + ox), round(y1 + oy))])


def _arc(pen, cx, cy, r, w, a0, a1, steps=10) -> None:
    """A stroked circular arc (angles in degrees), as one polygon."""
    outer = [(cx + (r + w / 2) * math.cos(math.radians(a0 + (a1 - a0) * i / steps)),
              cy + (r + w / 2) * math.sin(math.radians(a0 + (a1 - a0) * i / steps))) for i in range(steps + 1)]
    inner = [(cx + (r - w / 2) * math.cos(math.radians(a1 - (a1 - a0) * i / steps)),
              cy + (r - w / 2) * math.sin(math.radians(a1 - (a1 - a0) * i / steps))) for i in range(steps + 1)]
    _poly(pen, [(round(x), round(y)) for x, y in outer + inner])


def draw_symbol(glyf, silent, vibra, upm: int, cap: int):
    """OUR profile symbols, in the cap-height box: a speaker (+ sound waves, or a cross when silent), vibra = two
    short ticks either side; silent=None = the timed-profile clock. Returns (glyph, advance)."""
    pen = TTGlyphPen(None)
    s = cap / 700                                     # drawn on a 700-unit cap height
    P = lambda x, y: (round(x * s), round(y * s))
    stroke = 70 * s
    if silent is None:
        _arc(pen, *P(350, 330), 300 * s, stroke, 0, 360, steps=24)
        _bar(pen, *P(350, 330), *P(350, 560), stroke)
        _bar(pen, *P(350, 330), *P(520, 250), stroke)
        return pen.glyph(), round(700 * s)
    x0 = 150 if vibra else 40
    _poly(pen, [P(x0, 220), P(x0 + 130, 220), P(x0 + 300, 60), P(x0 + 300, 600), P(x0 + 130, 440), P(x0, 440)])
    if silent:
        _bar(pen, *P(x0 + 360, 190), *P(x0 + 600, 470), stroke)
        _bar(pen, *P(x0 + 360, 470), *P(x0 + 600, 190), stroke)
    else:
        _arc(pen, *P(x0 + 300, 330), 110 * s, stroke, -50, 50)
        _arc(pen, *P(x0 + 300, 330), 240 * s, stroke, -50, 50)
    width = x0 + 640
    if vibra:
        for vx in (40, width + 10):
            _bar(pen, *P(vx, 200), *P(vx + 40, 280), 50 * s)
            _bar(pen, *P(vx + 40, 280), *P(vx, 380), 50 * s)
            _bar(pen, *P(vx, 380), *P(vx + 40, 460), 50 * s)
        width += 110
    return pen.glyph(), round((width + 40) * s)


def s60_pua(f) -> int:
    """Replace the face's U+F800-F8FF with S60's meaning of those codes (aliases + OUR drawn profile symbols)."""
    cmap_tables = [t for t in f["cmap"].tables if t.isUnicode()]
    best = f.getBestCmap()
    glyf, hmtx = f["glyf"], f["hmtx"]
    cap = getattr(f["OS/2"], "sCapHeight", 0) or round(0.7 * f["head"].unitsPerEm)
    for t in cmap_tables:
        for cp in [c for c in t.cmap if 0xF800 <= c <= 0xF8FF]:
            del t.cmap[cp]
    added = 0
    for code, (gname, silent, vibra) in PUA_DRAWN.items():
        g, adv = draw_symbol(glyf, silent, vibra, f["head"].unitsPerEm, cap)
        glyf[gname] = g                               # (also appends gname to the font's glyph order)
        hmtx[gname] = (adv, 0)
        for t in cmap_tables:
            t.cmap[code] = gname
        added += 1
    space = best.get(0x0020)
    for code, target in PUA_ALIASES.items():
        gname = best.get(target) or (space if target in (0x3000, 0x2009) else None)
        if gname:
            for t in cmap_tables:
                t.cmap[code] = gname
    for tag in ("hdmx", "LTSH", "VDMX"):              # per-glyph tables that would not cover the new glyphs
        if tag in f:
            del f[tag]
    return added


def make(out_dir: str, out: str, src: str, family: str, sub: str, weight: int, bold: bool, extent) -> None:
    f = TTFont(os.path.join(SRC, src))
    upm = f["head"].unitsPerEm
    asc, desc = round(extent[0] * upm), round(extent[1] * upm)
    name = f["name"]
    name.names = [r for r in name.names if r.nameID in KEEP_NAME_IDS]
    full = family if sub == "Regular" else f"{family} {sub}"
    ps = full.replace(" ", "")
    for nid, val in ((1, family), (2, sub), (3, f"Phorepoc:{ps}"), (4, full), (5, "Version 1.0 (Phorepoc, from Ubuntu)"),
                     (6, ps)):
        name.setName(val, nid, 3, 1, 0x409)          # Windows, Unicode BMP, en-US
        name.setName(val, nid, 1, 0, 0)              # Macintosh, Roman
    os2, head, hhea = f["OS/2"], f["head"], f["hhea"]
    os2.usWeightClass = weight
    head.macStyle = (head.macStyle & ~1) | (1 if bold else 0)
    os2.fsSelection = (os2.fsSelection & ~0x61) | (0x20 if bold else 0x40)   # BOLD or REGULAR bit, never ITALIC
    hhea.ascent, hhea.descent = asc, -desc
    os2.sTypoAscender, os2.sTypoDescender = asc, -desc
    os2.usWinAscent, os2.usWinDescent = asc, desc
    s60_pua(f)
    f.save(os.path.join(out_dir, out))
    print(f"  {out}: {full!r} from {src} (weight {weight}{', bold' if bold else ''}, extent {asc}+{desc}/{upm})")


def main() -> None:
    out_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(BUILD, "font-gen")
    os.makedirs(out_dir, exist_ok=True)
    for face in FACES:
        make(out_dir, *face)


if __name__ == "__main__":
    main()
