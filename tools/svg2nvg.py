"""svg2nvg -- encode an SVG-Tiny icon as NVG-CS, the vector format AknIcon renders (nvgdecoder).

What svgtbinencode does, written from its outputs and the release's decoder (svgt/svgtopt/nvgdecoder/src/nvg.cpp):
the release ships the encoder as a binary only, and its Linux build does not run on a current system.

The file: a 52-byte header ("nvg", version 2, path data type 3 = 16.16 fixed point, the viewBox), an offset table, the
command list, the path count, the path sections, then the paint sections (fill/stroke paints and colour ramps, each
written once and shared by every command that uses it).
Numbers are read the way the SVG engine reads them: decimal -> float32 -> 16.16 fixed point, truncated toward zero
(svgScalarFromFloat), so the encoder's arithmetic is fixed-point too.

    python3 tools/svg2nvg.py in.svg [out.svgb]
"""
from __future__ import annotations

import re
import struct
import sys
import xml.etree.ElementTree as ET

ONE = 1 << 16

# path segment commands (OpenVG): absolute; | 1 = relative
CLOSE, MOVE, LINE, HLINE, VLINE, QUAD, CUBIC, SQUAD, SCUBIC = 0, 2, 4, 6, 8, 10, 12, 14, 16
SCCWARC, SCWARC, LCCWARC, LCWARC = 18, 20, 22, 24
NCOORD = {CLOSE: 0, MOVE: 2, LINE: 2, HLINE: 1, VLINE: 1, QUAD: 4, CUBIC: 6, SQUAD: 2, SCUBIC: 4,
          SCCWARC: 5, SCWARC: 5, LCCWARC: 5, LCWARC: 5}

CMD_FILL, CMD_STROKE, CMD_RAMP, CMD_PATH = 4, 5, 6, 7
CMD_XFORM, CMD_SWIDTH, CMD_JOINCAP, CMD_MITER = 8, 9, 10, 11


# ---------------------------------------------------------------- numbers

def f32(x: float) -> float:
    return struct.unpack("<f", struct.pack("<f", x))[0]


def fix_from_float(x: float) -> int:
    """svgScalarFromFloat: the float32's mantissa shifted to 16.16, truncated toward zero."""
    a, = struct.unpack("<i", struct.pack("<f", x))
    sign = -1 if a < 0 else 0
    exponent = (127 + 15) - ((a >> 23) & 0xff)
    if exponent >= 32 or exponent < 0:
        r = 0 if exponent >= 32 else 0x7fffffff
    else:
        r = (((a << 8) & 0xffffffff) | 0x80000000) >> exponent
    return (r ^ sign) - sign


def fix(s: str) -> int:
    return fix_from_float(f32(float(s)))


def tofloat(v: int) -> float:
    """svgFixedToFloat: 16.16 -> float32 keeping the top 24 significant bits (truncated, not rounded)"""
    if not v:
        return 0.0
    m = abs(v)
    drop = max(m.bit_length() - 24, 0)
    m = (m >> drop) << drop
    return (m if v > 0 else -m) / ONE


def out_fix_round(v: int) -> int:
    """a circle's or ellipse's coordinate as written: through a rounding float conversion and back"""
    return fix_from_float(f32(v / ONE))


def out_fix(v: int) -> int:
    """a path coordinate as written: through the renderer's float (svgFixedToFloat) and back"""
    return fix_from_float(tofloat(v))


def fmul(a: int, b: int) -> int:
    return (a * b) >> 16


NUM = re.compile(r"[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?")


def nums(s: str) -> list[int]:
    return [fix(m) for m in NUM.findall(s or "")]


# ---------------------------------------------------------------- style

COLORS = {"black": 0x000000, "white": 0xffffff, "red": 0xff0000, "green": 0x008000, "blue": 0x0000ff,
          "yellow": 0xffff00, "gray": 0x808080, "grey": 0x808080, "silver": 0xc0c0c0, "maroon": 0x800000,
          "purple": 0x800080, "fuchsia": 0xff00ff, "lime": 0x00ff00, "olive": 0x808000, "navy": 0x000080,
          "teal": 0x008080, "aqua": 0x00ffff}
INHERITED = ("fill", "fill-opacity", "stroke", "stroke-opacity", "stroke-width", "stroke-linejoin",
             "stroke-linecap", "stroke-miterlimit", "fill-rule", "visibility")


def color(v: str):
    v = v.strip()
    if v.startswith("#"):
        h = v[1:]
        if len(h) == 3:
            h = "".join(c * 2 for c in h)
        return int(h[:6], 16)
    m = re.match(r"rgb\(\s*([^,]+),\s*([^,]+),\s*([^)]+)\)", v)
    if m:
        def c(x):
            x = x.strip()
            return round(float(x[:-1]) * 255 / 100) if x.endswith("%") else int(float(x))
        return (c(m.group(1)) << 16) | (c(m.group(2)) << 8) | c(m.group(3))
    return COLORS.get(v.lower())


def attrs(el) -> dict:
    a = {k.split("}")[-1]: v for k, v in el.attrib.items()}
    for decl in a.pop("style", "").split(";"):
        if ":" in decl:
            k, v = decl.split(":", 1)
            a[k.strip()] = v.strip()
    return a


# ---------------------------------------------------------------- geometry

class Path:
    def __init__(self):
        self.rounded = False
        self.segs: list[int] = []
        self.coords: list[int] = []

    def add(self, seg: int, *c: int):
        self.segs.append(seg)
        self.coords.extend(c)


def path_number(d: str, i: int):
    """TSvgPointLexer::SkipToNumEndWithoutLex: digits accumulated in a float32 (n = 10n + digit), every '.' starts or
    extends the fraction, one division at the end; a number with an exponent is 0. -> (value or None, next index)"""
    n = 0.0
    found = False
    frac = 0
    while i < len(d) and (d[i].isdigit() or d[i] == "."):
        if d[i] != ".":
            found = True
            n = f32(10 * n + (ord(d[i]) - 48))      # 10n + digit evaluated wider, rounded once (x87)
            if frac:
                frac += 1
        else:
            frac += 1
        i += 1
        if i < len(d) and d[i] in "eE":
            i += 1
            if i < len(d) and d[i] == "-":
                i += 1
            while i < len(d) and (d[i].isdigit() or d[i] == "."):
                i += 1
            return (0.0 if found else None), i
    if not found:
        return None, i
    k = 10 ** max(frac - 1, 0)
    return f32(n / f32(k)), i


def parse_d(d: str) -> Path:
    """TSvgPathDataParser::ParsePathDataL over CGfxGeneralPath, quirks included: no arcs (a path with one is
    dropped), S/T without a preceding curve take the subpath start as the
    control point, numbers after a z without a command drop the path."""
    types: list[int] = []
    co: list[int] = []
    last = [0, 0]           # iLastX/iLastY: the start of the current subpath
    i = 0
    prev = ""               # lPrevCom
    prev2 = ""              # lPrevCom2

    def get_command():
        nonlocal i
        while i < len(d):
            ch = d[i]
            if not (ch.isdigit() or ch in ".-"):
                if ch.isascii() and ch.isalpha():
                    i += 1
                    return True, ch
                i += 1
            else:
                return True, ""
        return False, ""

    def get_nums(k):
        nonlocal i
        out = []
        for _ in range(k):
            while i < len(d) and d[i] in " \n,\r":
                i += 1
            if i >= len(d):
                return None
            neg = False
            if d[i] == "-":
                neg = True
                i += 1
                if i >= len(d):
                    return None
            elif d[i] == "+":
                i += 1
            v, i = path_number(d, i)
            if v is None:
                return None
            out.append(fix_from_float(-v if neg else v))
        return out

    def base():
        if types and types[-1] == CLOSE:
            return last[0], last[1]
        return co[-2], co[-1]

    def rel(v, absolute):
        if not absolute and len(co) >= 2:
            bx, by = base()
            v = [v[j] + (bx if j % 2 == 0 else by) for j in range(len(v))]
        return v

    def ref_point():
        if len(co) < 4:
            return 0, 0
        return co[-2] * 2 - co[-4], co[-1] * 2 - co[-3]

    while True:
        ok, com = get_command()
        if not ok:
            break
        if com == "":
            if prev in "zZ" and prev:
                return Path()
            com = {"m": "l", "M": "L"}.get(prev, prev)
        else:
            prev = com
        c = com.upper()
        absolute = com.isupper()
        need = {"M": 2, "L": 2, "H": 1, "V": 1, "C": 6, "S": 4, "Q": 4, "T": 2}.get(c)
        if c == "Z":
            if types and types[-1] != CLOSE:
                types.append(CLOSE)
            prev2 = com
            continue
        if need is None:
            return Path()
        v = get_nums(need)
        if v is None:
            return Path()
        if c == "M":
            v = rel(v, absolute)
            types.append(MOVE)
            co.extend(v)
            last[:] = v
        elif c == "L":
            types.append(LINE)
            co.extend(rel(v, absolute))
        elif c == "H":
            x = v[0]
            if not absolute and len(co) >= 2:
                x += last[0] if types[-1] == CLOSE else co[-2]
            y = 0 if len(co) < 1 else co[-1]
            types.append(LINE)
            co.extend([x, y])
        elif c == "V":
            y = v[0]
            if not absolute and len(co) >= 2:
                y += last[1] if types[-1] == CLOSE else co[-1]
            x = 0 if len(co) < 2 else co[-2]
            types.append(LINE)
            co.extend([x, y])
        elif c == "C":
            types.append(CUBIC)
            co.extend(rel(v, absolute))
        elif c == "S":
            v = rel(v, absolute)
            x1, y1 = ref_point()
            if types[-1] != CUBIC:
                x1, y1 = last
            types.append(CUBIC)
            co.extend([x1, y1] + v)
        elif c == "Q":
            types.append(QUAD)
            co.extend(rel(v, absolute))
        elif c == "T":
            if absolute and prev2 not in ("Q", "q", "T", "t"):
                types.append(QUAD)
                co.extend([last[0], last[1]] + v)
            elif absolute or v[0] != 0 or v[1] != 0:
                if not absolute and len(co) >= 2:
                    bx, by = base()
                    v = [v[0] + bx, v[1] + by]
                x1, y1 = ref_point()
                types.append(QUAD)
                co.extend([x1, y1] + v)
        prev2 = com
    p = Path()
    j = 0
    for t in types:
        n = NCOORD[t]
        p.add(t, *co[j:j + n])
        j += n
    return p


def shape_path(tag: str, a: dict) -> Path | None:
    g = lambda k: fix(a[k]) if k in a else 0   # noqa: E731
    p = Path()
    if tag == "path":
        return parse_d(a.get("d", ""))
    if tag == "rect":
        x, y, w, h = g("x"), g("y"), g("width"), g("height")
        if w <= 0 or h <= 0:
            return None
        p.add(MOVE, x, y)
        p.add(LINE | 1, w, 0)
        p.add(LINE | 1, 0, h)
        p.add(LINE | 1, -w, 0)
        p.add(CLOSE)
        return p
    if tag in ("circle", "ellipse"):
        cx, cy = g("cx"), g("cy")
        rx, ry = (g("r"), g("r")) if tag == "circle" else (g("rx"), g("ry"))
        if rx <= 0 or ry <= 0:
            return None
        p.rounded = True
        p.add(MOVE, cx + rx, cy)
        p.add(SCCWARC | 1, rx, ry, 0, -2 * rx, 0)
        p.add(SCCWARC | 1, rx, ry, 0, 2 * rx, 0)
        p.add(CLOSE)
        return p
    if tag == "line":
        p.add(MOVE, g("x1"), g("y1"))
        p.add(LINE, g("x2"), g("y2"))
        return p
    if tag in ("polygon", "polyline"):
        v = nums(a.get("points", ""))
        if len(v) < 2:
            return None
        p.add(MOVE, v[0], v[1])
        for k in range(2, len(v) - 1, 2):
            p.add(LINE, v[k], v[k + 1])
        if tag == "polygon":
            p.add(CLOSE)
        return p
    return None


# ---------------------------------------------------------------- transforms (gfx2d TGfxAffineTransform, 16.16)

T_IDENTITY, T_TRANSLATE, T_SCALE, T_SHEAR = 0, 1, 2, 4


def _s32(x: int) -> int:
    x &= 0xffffffff
    return x - (1 << 32) if x & 0x80000000 else x


def _mul64(a: int, b: int):
    """GfxMath::hgMul64: signed 32x32 -> 64 as (high, low), in 32-bit pieces"""
    hh = _s32((a >> 16) * (b >> 16)) & 0xffffffff
    lh = _s32((a & 0xffff) * (b >> 16)) & 0xffffffff
    hl = _s32((a >> 16) * (b & 0xffff)) & 0xffffffff
    ll = ((a & 0xffff) * (b & 0xffff)) & 0xffffffff
    hh = (hh + (_s32(lh) >> 16)) & 0xffffffff
    hh = (hh + (_s32(hl) >> 16)) & 0xffffffff
    old = ll
    ll = (ll + (lh << 16)) & 0xffffffff
    if ll < old:
        hh = (hh + 1) & 0xffffffff
    old = ll
    ll = (ll + (hl << 16)) & 0xffffffff
    if ll < old:
        hh = (hh + 1) & 0xffffffff
    return _s32(hh), _s32(ll)


def _mulu64(a: int, b: int):
    a &= 0xffffffff
    b &= 0xffffffff
    p = a * b
    return _s32(p >> 32), _s32(p)


def _mulu64h(a: int, b: int) -> int:
    return ((a & 0xffffffff) * (b & 0xffffffff)) >> 32


def _lsl64h(a, sh: int) -> int:
    h, l = a
    return _s32((h << sh) | ((l & 0xffffffff) >> (32 - sh)))


def _add64(a, b):
    hi = _s32(a[0] + b[0])
    lo = _s32(a[1] + b[1])
    if (lo & 0xffffffff) < (a[1] & 0xffffffff):
        hi = _s32(hi + 1)
    return hi, lo


def _rcos(xx: int) -> int:
    """GfxMath::svgiRCos"""
    fpi = 0x6487ED51
    x = abs(xx) & 0xffffffff
    y = x
    if y > fpi >> 1:
        y = (fpi - y) & 0xffffffff
    y = (y << 1) & 0xffffffff
    c = _mulu64(y, (y << 1) & 0xffffffff)
    x2 = _lsl64h(c, 1)
    t = _mulu64h(x2, x2)
    for k in (-(0x55555555 >> 1) - 1, 0x5B05B05B >> 4, -(0x68068068 >> 8) + 1, (0x49F93EDD >> 12) + 1,
              -(0x47BB63BF >> 17)):
        c = _add64(c, _mul64(_s32(t), k))
        t = _mulu64h(t, x2)
    r = (0x40000000 - c[0]) >> 1
    return -r if x > (fpi >> 1) else r


def scalar_cos(r1: int) -> int:
    foo = _lsl64h(_mul64(_s32(r1), 0x517CC1B7), 15)
    foo = _s32(_mulu64h(foo, 0x6487ED51))
    if foo >= 0x6487ED51 >> 1:
        foo = _s32(foo - 0x6487ED51)
    return _rcos(_s32(foo << 1)) >> 13


def _scalar_sin(r1: int) -> int:
    r1 = _s32(r1 - (0x6487ED51 >> 14))
    foo = _lsl64h(_mul64(r1, 0x517CC1B7), 15)
    foo = _s32(_mulu64h(foo, 0x6487ED51))
    if foo >= 0x6487ED51 >> 1:
        foo = _s32(foo - 0x6487ED51)
    return _rcos(_s32(foo << 1)) >> 13


def scalar_tan(r1: int) -> int:
    s = _scalar_sin(r1)
    if not s:
        return -1
    c = scalar_cos(r1)
    if not c:
        return -1
    q = abs(s << 15) // abs(c)
    return _s32((q if (s < 0) == (c < 0) else -q) << 1)


DEG = f32(f32(3.1415926) / f32(180.0))      # KGradiansPerDegree


class Affine:
    """TGfxAffineTransform: m00 m10 m01 m11 m02 m12 in 16.16 and the transform type it keeps"""

    def __init__(self, m00=ONE, m10=0, m01=0, m11=ONE, m02=0, m12=0, t=T_IDENTITY):
        self.m00, self.m10, self.m01, self.m11, self.m02, self.m12, self.type = m00, m10, m01, m11, m02, m12, t

    def key(self):
        return (self.m00, self.m10, self.m01, self.m11, self.m02, self.m12)

    def copy(self):
        return Affine(*self.key(), self.type)

    def update_state(self):
        t = T_IDENTITY
        if self.m02 or self.m12:
            t |= T_TRANSLATE
        if self.m00 != ONE or self.m11 != ONE:     # the encoder's build: scale only when it is not 1
            t |= T_SCALE
        if self.m01 or self.m10:
            t |= T_SHEAR
        if t == T_SCALE and self.m00 == ONE and self.m11 == ONE:
            t = T_IDENTITY
        self.type = t

    def concatenate(self, a: "Affine"):
        if self.type == T_IDENTITY:
            self.m00, self.m01, self.m02, self.m11, self.m10, self.m12 = a.m00, a.m01, a.m02, a.m11, a.m10, a.m12
            self.type = a.type
        elif a.type == T_IDENTITY:
            pass
        else:
            m0, m1 = self.m00, self.m01
            self.m00 = fmul(a.m00, m0) + fmul(a.m10, m1)
            self.m01 = fmul(a.m01, m0) + fmul(a.m11, m1)
            self.m02 += fmul(a.m02, m0) + fmul(a.m12, m1)
            m0, m1 = self.m10, self.m11
            self.m11 = fmul(a.m01, m0) + fmul(a.m11, m1)
            self.m10 = fmul(a.m00, m0) + fmul(a.m10, m1)
            self.m12 += fmul(a.m02, m0) + fmul(a.m12, m1)
            self.type |= a.type

    @staticmethod
    def rotation(theta: float):
        r = int(theta * 0x10000)            # (TInt32)(aTheta * TReal(0x10000))
        cs, sn = scalar_cos(r), _scalar_sin(r)
        return Affine(cs, sn, -sn, cs, 0, 0, T_SHEAR | T_SCALE)


def parse_transform(t: str) -> Affine:
    """CSvgElementImpl's transform attribute: each item concatenated onto the running transform"""
    m = Affine()
    for name, args in re.findall(r"(matrix|translate|scale|rotate|skewX|skewY)\s*\(([^)]*)\)", t):
        v = [f32(float(x)) for x in NUM.findall(args)]
        if name == "matrix" and len(v) >= 6:
            n = Affine(*(fix_from_float(x) for x in v[:6]))
            n.update_state()
            m.concatenate(n)
        elif name == "translate" and v:
            m.concatenate(Affine(ONE, 0, 0, ONE, fix_from_float(v[0]), fix_from_float(v[1] if len(v) > 1 else 0.0),
                                 T_TRANSLATE))
            m.type |= T_TRANSLATE
        elif name == "scale" and v:
            sx = v[0]
            sy = v[1] if len(v) > 1 else sx
            m.concatenate(Affine(fix_from_float(sx), 0, 0, fix_from_float(sy), 0, 0, T_SCALE))
            m.type |= T_SCALE
        elif name == "rotate" and v:
            theta = f32(v[0] * DEG)
            r = Affine.rotation(theta)
            if len(v) >= 3:
                x, y = fix_from_float(v[1]), fix_from_float(v[2])
                r.m02 = fmul(x, ONE - r.m00) + fmul(y, r.m10)
                r.m12 = fmul(y, ONE - r.m00) - fmul(x, r.m10)
                r.type |= T_TRANSLATE
                m.concatenate(r)
                m.type |= T_SHEAR | T_SCALE | T_TRANSLATE
            else:
                m.concatenate(r)
                m.type |= T_SHEAR | T_SCALE
        elif name in ("skewX", "skewY") and v:
            tn = scalar_tan(int(f32(v[0] * DEG) * 0x10000))
            sh = Affine(ONE, tn if name == "skewY" else 0, tn if name == "skewX" else 0, ONE, 0, 0, T_SHEAR)
            m.concatenate(sh)
            m.type |= T_SHEAR
    return m


IDENTITY = (ONE, 0, 0, ONE, 0, 0)


def xform(m) -> bytes:
    """KCMD_SET_TRANSFORMATION: type 1 = identity; scale, translation or both; 0 = the whole matrix.
    m = (a b c d e f) = (m00 m10 m01 m11 m02 m12)"""
    a, b, c, d, e, f = m
    if m == IDENTITY:
        return struct.pack("<I", (CMD_XFORM << 24) | (1 << 16))
    if b or c:
        t, vals = 0, (a, d, c, b, e, f)
    else:
        t, vals = 0, ()
        if (a, d) != (ONE, ONE):
            t, vals = t | 2, vals + (a, d)
        if e or f:
            t, vals = t | 16, vals + (e, f)
    return struct.pack("<I", (CMD_XFORM << 24) | (t << 16)) + struct.pack(f"<{len(vals)}f", *map(tofloat, vals))


# ---------------------------------------------------------------- encoder

class Encoder:
    def __init__(self, root):
        self.root = root
        self.ids = {}
        for el in root.iter():
            i = el.get("id")
            if i:
                self.ids[i] = el
        self.pos = {el: k for k, el in enumerate(root.iter())}   # document order
        self.cur = None                    # the element being drawn
        self.ctm = IDENTITY                # the transform last set in the command list
        self.pending = {}                  # gradient element -> ramps waiting for it
        self.stroke_state = {}             # stroke width, join/cap, miter limit last written
        self.paints: list[bytes] = []      # paint + ramp sections, shared
        self.paths: list[bytes] = []
        self.cmds: list[bytes] = []

    def paint_ix(self, data: bytes) -> int:
        if data in self.paints:
            return self.paints.index(data)
        self.paints.append(data)
        return len(self.paints) - 1

    def grad_stops(self, el):
        while el is not None and not [s for s in el if s.tag.split("}")[-1] == "stop"]:
            href = el.get("{http://www.w3.org/1999/xlink}href", "")
            el = self.ids.get(href[1:]) if href.startswith("#") else None
        out = []
        for s in (el if el is not None else []):
            if s.tag.split("}")[-1] != "stop":
                continue
            a = attrs(s)
            off = a.get("offset", "0").strip()
            o = fix(off[:-1]) // 100 if off.endswith("%") else fix(off)
            c = color(a.get("stop-color", "black"))
            op = fix(a.get("stop-opacity", "1"))
            out.append((o, c, op))
        return out

    def ramp(self, stops) -> bytes:
        b = struct.pack("<I", (len(stops) << 16) | 4)
        for o, c, op in stops:
            b += struct.pack("<5f", tofloat(o), f32(((c >> 16) & 0xff) / 255), f32(((c >> 8) & 0xff) / 255),
                             f32((c & 0xff) / 255), tofloat(op))
        return b

    def paint(self, value: str, opacity: int, stroke: bool):
        """-> list of command words, or None for no paint."""
        value = value.strip()
        alpha = (opacity * 255) >> 16
        op = CMD_STROKE if stroke else CMD_FILL
        if value == "none":
            return None
        m = re.match(r"url\(\s*#([^)\s]+)\s*\)", value)
        if m:
            el = self.ids.get(m.group(1))
            if el is None:
                return None
            tag = el.tag.split("}")[-1]
            a = attrs(el)
            stops = self.grad_stops(el)
            mat = nums(a.get("gradientTransform", "").replace("matrix", "")) if "matrix" in a.get(
                "gradientTransform", "") else None
            if tag == "linearGradient":
                vals = [fix(a.get(k, d)) for k, d in (("x1", "0"), ("y1", "0"), ("x2", "1"), ("y2", "0"))]
                t = 2
            else:
                cx, cy = fix(a.get("cx", "0.5")), fix(a.get("cy", "0.5"))
                vals = [cx, cy, fix(a["fx"]) if "fx" in a else cx, fix(a["fy"]) if "fy" in a else cy,
                        fix(a.get("r", "0.5"))]
                t = 3
            spec = 1 if mat else 0
            b = struct.pack("<I", (spec << 16) | t) + struct.pack(f"<{len(vals)}f", *map(tofloat, vals))
            if mat:
                a_, b_, c_, d_, e_, f_ = mat
                b += struct.pack("<6f", *map(tofloat, (a_, c_, e_, b_, d_, f_)))
            words = [(op << 24) | (alpha << 16) | self.paint_ix(b)]
            # a gradient defined after the element that uses it: its ramp section is numbered when the gradient
            # element is reached
            if self.pos.get(el, 0) > self.pos.get(self.cur, 0):
                self.pending.setdefault(el, []).append(self.ramp(stops))
                words.append(("ramp", self.ramp(stops)))
            else:
                words.append((CMD_RAMP << 24) | self.paint_ix(self.ramp(stops)))
            return words
        c = color(value)
        if c is None:
            return None
        b = struct.pack("<II", 1, (c << 8) | 0xff)
        return [(op << 24) | (alpha << 16) | self.paint_ix(b)]

    def path_section(self, p: Path) -> bytes:
        b = struct.pack("<H", len(p.segs)) + bytes(p.segs)
        b += b"\0" * ((-len(b)) % 4)
        conv = out_fix_round if p.rounded else out_fix
        b += struct.pack(f"<{len(p.coords)}i", *map(conv, p.coords))
        # the encoder sizes an arc segment as 6 coordinates and writes 5
        b += b"\0" * (4 * sum(1 for s in p.segs if (s & ~1) in (SCCWARC, SCWARC, LCCWARC, LCWARC)))
        return b

    def walk(self, el, style: dict, opacity: int, ctm=None):
        tag = el.tag.split("}")[-1]
        if tag in ("linearGradient", "radialGradient"):
            for r in self.pending.pop(el, []):
                self.paint_ix(r)
            return
        if tag == "defs":
            for ch in el:
                self.walk(ch, style, opacity, ctm)
            return
        if tag in ("title", "desc", "metadata", "switch", "foreignObject", "text"):
            return
        a = attrs(el)
        if a.get("display") == "none":
            return
        st = dict(style)
        for k in INHERITED:
            if k in a:
                st[k] = a[k]
        if "opacity" in a:
            opacity = fmul(opacity, fix(a["opacity"]))
        if "transform" in a:
            ctm = ctm.copy() if ctm else Affine()
            ctm.concatenate(parse_transform(a["transform"]))
        if tag in ("svg", "g", "a"):
            for ch in el:
                self.walk(ch, st, opacity, ctm)
            return
        self.cur = el
        p = shape_path(tag, a)
        if p is None or not p.segs:
            return
        if st.get("visibility") == "hidden":
            return
        # the transform is set when the next shape is rendered, drawn or not (fill and stroke none)
        if (ctm.key() if ctm else IDENTITY) != self.ctm:
            self.ctm = ctm.key() if ctm else IDENTITY
            self.cmds.append(xform(self.ctm))
        flags = 0
        words = []
        fill = self.paint(st.get("fill", "black"), fmul(opacity, fix(st.get("fill-opacity", "1"))), False)
        if fill:
            words += fill
            flags |= 2
        # a line has no inside: without a stroke of its own it is stroked with its fill colour (not a gradient)
        dstroke = "none"
        if tag == "line" and not st.get("fill", "black").startswith("url("):
            dstroke = st.get("fill", "black")
        stroke = self.paint(st.get("stroke", dstroke), fmul(opacity, fix(st.get("stroke-opacity", "1"))), True)
        if stroke:
            if not fill:
                flags = 0xcc       # the encoder leaves the flags byte unset (MSVC heap fill) for a stroke-only path
            words += stroke
            sw = fix(st.get("stroke-width", "1"))
            # stroke width, join/cap and miter limit are written when they change from the last stroke
            if sw != self.stroke_state.get("width"):
                words.append(struct.pack("<If", CMD_SWIDTH << 24, tofloat(sw)))
            join = {"bevel": 1, "miter": 2, "round": 3}.get(st.get("stroke-linejoin", "miter"), 2)
            cap = {"butt": 1, "square": 2, "round": 3}.get(st.get("stroke-linecap", "butt"), 1)
            if (join, cap) != self.stroke_state.get("joincap", (2, 1)):
                words.append(struct.pack("<I", (CMD_JOINCAP << 24) | (join << 8) | cap))
            miter = fix(st.get("stroke-miterlimit", "4"))
            if miter != self.stroke_state.get("miter"):
                words.append(struct.pack("<If", CMD_MITER << 24, tofloat(miter)))
            self.stroke_state = {"width": sw, "joincap": (join, cap), "miter": miter}
            flags |= 1
        if not flags:
            return
        self.paths.append(self.path_section(p))
        words.append(("path", flags, len(self.paths) - 1))
        self.cmds.extend(words)

    def encode(self) -> bytes:
        a = attrs(self.root)
        vb = nums(a.get("viewBox", ""))
        if len(vb) != 4:
            vb = [0, 0, fix(a.get("width", "0")), fix(a.get("height", "0"))]
        self.walk(self.root, {}, ONE)
        self.cmds = [(CMD_RAMP << 24) | self.paint_ix(w[1]) if isinstance(w, tuple) and w[0] == "ramp" else w
                     for w in self.cmds]
        npaint = len(self.paints)
        words = []
        for w in self.cmds:
            if isinstance(w, tuple):
                _, flags, k = w
                words.append(struct.pack("<I", (CMD_PATH << 24) | (flags << 16) | (npaint + k)))
            elif isinstance(w, bytes):
                words.append(w)
            else:
                words.append(struct.pack("<I", w))
        sections = self.paths + self.paints
        order = self.paints + self.paths    # offset-table order: paints, then paths
        nofs = len(order)
        hdr = 52
        cmd_at = hdr + 2 + 2 * nofs
        if nofs % 2 == 0:
            cmd_at += 2
        cmd_at += 4
        body_at = cmd_at + sum(len(w) for w in words) + 2
        body_at += (-body_at) % 4 if self.paths or self.paints else 0
        offs = {}
        pos = body_at
        data = b""
        for i, s in enumerate(sections):
            key = ("path", i) if i < len(self.paths) else ("paint", i - len(self.paths))
            offs[key] = pos
            data += s
            pos += len(s)
        table = [offs[("paint", i)] for i in range(npaint)] + [offs[("path", i)] for i in range(len(self.paths))]
        out = bytearray(b"nvg" + bytes([2]) + struct.pack("<hH", hdr, 0) + bytes(18))
        out += struct.pack("<h", 3) + struct.pack("<6f", 16.0, 0.0, *map(tofloat, vb))
        out += struct.pack("<H", nofs) + struct.pack(f"<{nofs}H", *table)
        if nofs % 2 == 0:
            out += b"\0\0"
        out += struct.pack("<HH", len(words), 0)
        for w in words:
            out += w
        out += struct.pack("<H", len(self.paths))
        out += bytes(body_at - len(out))
        out += data
        return bytes(out)


def encode(svg: bytes) -> bytes:
    root = ET.fromstring(svg)
    return Encoder(root).encode()


if __name__ == "__main__":
    src = sys.argv[1]
    dst = sys.argv[2] if len(sys.argv) > 2 else src + "b"
    with open(src, "rb") as fh:
        data = encode(fh.read())
    with open(dst, "wb") as fh:
        fh.write(data)
