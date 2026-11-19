# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

"""svgnorm -- rewrite the path data of an SVG into what svgtbinencode's NVG encoder reads: absolute M / L / C / Z only.

The encoder (svg2nvg.parse_d, a port of the release's TSvgPathDataParser) drops a whole path that contains an arc command
(`a`/`A`) and has quirks with relative commands, so an icon set drawn with arcs (Tabler Icons: every rounded corner is
`a2 2 0 0 1 2 -2`) loses its outlines. mif_gen runs the status-indicator art through normalize() when staging it:
arcs become cubic Beziers, relative commands absolute, H/V/S/Q/T line, cubic and smooth forms explicit. The geometry is
unchanged; numbers are written plain (no exponents, which the encoder's lexer reads as 0).
"""
from __future__ import annotations

import math
import re

_TOKEN = re.compile(r"[MmLlHhVvCcSsQqTtAaZz]|[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?")


def _num(v: float) -> str:
    s = ("%.4f" % v).rstrip("0").rstrip(".")
    return "0" if s in ("-0", "") else s


def _arc(x1, y1, rx, ry, phi, fa, fs, x2, y2):
    """SVG arc -> list of cubic segments (x1c, y1c, x2c, y2c, x, y), per the SVG implementation notes F.6.5."""
    if rx == 0 or ry == 0 or (x1 == x2 and y1 == y2):
        return [(x1, y1, x2, y2, x2, y2)] if (x1, y1) != (x2, y2) else []
    rx, ry = abs(rx), abs(ry)
    cp, sp = math.cos(math.radians(phi)), math.sin(math.radians(phi))
    dx, dy = (x1 - x2) / 2, (y1 - y2) / 2
    x1p, y1p = cp * dx + sp * dy, -sp * dx + cp * dy
    lam = x1p ** 2 / rx ** 2 + y1p ** 2 / ry ** 2
    if lam > 1:
        rx, ry = rx * math.sqrt(lam), ry * math.sqrt(lam)
    num = rx ** 2 * ry ** 2 - rx ** 2 * y1p ** 2 - ry ** 2 * x1p ** 2
    den = rx ** 2 * y1p ** 2 + ry ** 2 * x1p ** 2
    co = math.sqrt(max(0.0, num / den)) if den else 0.0
    if fa == fs:
        co = -co
    cxp, cyp = co * rx * y1p / ry, -co * ry * x1p / rx
    cx, cy = cp * cxp - sp * cyp + (x1 + x2) / 2, sp * cxp + cp * cyp + (y1 + y2) / 2

    def ang(ux, uy, vx, vy):
        a = math.atan2(ux * vy - uy * vx, ux * vx + uy * vy)
        return a

    t1 = ang(1, 0, (x1p - cxp) / rx, (y1p - cyp) / ry)
    dt = ang((x1p - cxp) / rx, (y1p - cyp) / ry, (-x1p - cxp) / rx, (-y1p - cyp) / ry)
    if not fs and dt > 0:
        dt -= 2 * math.pi
    elif fs and dt < 0:
        dt += 2 * math.pi
    n = max(1, int(math.ceil(abs(dt) / (math.pi / 2) - 1e-9)))
    step = dt / n
    k = 4 / 3 * math.tan(step / 4)
    out = []
    t = t1
    for _ in range(n):
        c1, s1 = math.cos(t), math.sin(t)
        c2, s2 = math.cos(t + step), math.sin(t + step)
        pts = [(c1 - k * s1, s1 + k * c1), (c2 + k * s2, s2 - k * c2), (c2, s2)]
        seg = []
        for px, py in pts:
            px, py = px * rx, py * ry
            seg += [cp * px - sp * py + cx, sp * px + cp * py + cy]
        out.append(tuple(seg))
        t += step
    # land exactly on the end point
    last = out[-1]
    out[-1] = last[:4] + (x2, y2)
    return out


def normalize_d(d: str) -> str:
    toks = _TOKEN.findall(d)
    i = 0
    out: list[str] = []
    x = y = sx = sy = 0.0
    cmd = ""
    lc = None      # last cubic second control (for S)
    lq = None      # last quad control (for T)

    def nums(k):
        nonlocal i
        v = [float(t) for t in toks[i:i + k]]
        if len(v) < k:
            raise ValueError("path data ends inside a command")
        i += k
        return v

    while i < len(toks):
        if toks[i].isalpha():
            cmd = toks[i]
            i += 1
            if cmd in "Zz":
                out.append("Z")
                x, y = sx, sy
                lc = lq = None
                continue
        elif not cmd:
            raise ValueError("path data starts with a number")
        rel = cmd.islower()
        c = cmd.upper()
        ox, oy = (x, y) if rel else (0.0, 0.0)
        if c == "M":
            a, b = nums(2)
            x, y = a + ox, b + oy
            sx, sy = x, y
            out.append("M%s %s" % (_num(x), _num(y)))
            cmd = "l" if rel else "L"       # further pairs are lines
            lc = lq = None
        elif c == "L":
            a, b = nums(2)
            x, y = a + ox, b + oy
            out.append("L%s %s" % (_num(x), _num(y)))
            lc = lq = None
        elif c == "H":
            (a,) = nums(1)
            x = a + ox
            out.append("L%s %s" % (_num(x), _num(y)))
            lc = lq = None
        elif c == "V":
            (b,) = nums(1)
            y = b + oy
            out.append("L%s %s" % (_num(x), _num(y)))
            lc = lq = None
        elif c == "C":
            a = nums(6)
            p = [a[0] + ox, a[1] + oy, a[2] + ox, a[3] + oy, a[4] + ox, a[5] + oy]
            out.append("C" + " ".join(_num(v) for v in p))
            lc = (p[2], p[3])
            x, y = p[4], p[5]
            lq = None
        elif c == "S":
            a = nums(4)
            x1, y1 = (2 * x - lc[0], 2 * y - lc[1]) if lc else (x, y)
            p = [x1, y1, a[0] + ox, a[1] + oy, a[2] + ox, a[3] + oy]
            out.append("C" + " ".join(_num(v) for v in p))
            lc = (p[2], p[3])
            x, y = p[4], p[5]
            lq = None
        elif c in "QT":
            if c == "Q":
                a = nums(4)
                qx, qy, ex, ey = a[0] + ox, a[1] + oy, a[2] + ox, a[3] + oy
            else:
                a = nums(2)
                qx, qy = (2 * x - lq[0], 2 * y - lq[1]) if lq else (x, y)
                ex, ey = a[0] + ox, a[1] + oy
            p = [x + 2 / 3 * (qx - x), y + 2 / 3 * (qy - y), ex + 2 / 3 * (qx - ex), ey + 2 / 3 * (qy - ey), ex, ey]
            out.append("C" + " ".join(_num(v) for v in p))
            lq = (qx, qy)
            x, y = ex, ey
            lc = None
        elif c == "A":
            rx, ry, phi, fa, fs, a, b = nums(7)
            ex, ey = a + ox, b + oy
            segs = _arc(x, y, rx, ry, phi, int(fa), int(fs), ex, ey)
            for s in segs:
                out.append("C" + " ".join(_num(v) for v in s))
            x, y = ex, ey
            lc = lq = None
        else:
            raise ValueError("unknown path command " + cmd)
    return " ".join(out)


def shift_d(d: str, dx: float, dy: float, scale: float = 1.0) -> str:
    """Scale a normalised (absolute M/L/C/Z) path, then translate it by (dx, dy)."""
    out = []
    for m in re.finditer(r"([MLCZ])([^MLCZ]*)", d):
        v = [float(t) for t in m.group(2).split()]
        out.append(m.group(1) + " ".join(_num(c * scale + (dx if k % 2 == 0 else dy)) for k, c in enumerate(v)))
    return " ".join(out)


def normalize(svg: str, shift: tuple[float, float] = (0.0, 0.0), scale: float = 1.0) -> str:
    """The SVG text with every path's d= rewritten by normalize_d (other elements untouched), scaled then translated."""
    def sub(m):
        d = normalize_d(m.group(2))
        if shift != (0.0, 0.0) or scale != 1.0:
            d = shift_d(d, shift[0], shift[1], scale)
        return m.group(1) + d + m.group(3)
    return re.sub(r'(\sd=")([^"]*)(")', sub, svg)
