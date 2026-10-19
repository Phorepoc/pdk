"""svg2svgb -- encode an SVG-Tiny document as SVG-T binary, the format svgtbinencode falls back to for content NVG
cannot hold (dash arrays, text, switch, display, group opacity ...).

The format is the engine's DOM written out element by element (read by svgt/svgtopt/SVG/SVGEngine/src/
Svgdecoder.cpp): a magic number, then for each element its id, its attributes (16-bit id + a typed value) up to
KStartNewElem, its children, and an end marker. Numbers are 16.16 fixed point, colours 0x00RRGGBB.

    python3 tools/svg2svgb.py in.svg [out.svgb]
"""
from __future__ import annotations

import re
import struct
import sys
import xml.etree.ElementTree as ET

import svg2nvg as N

MAGIC = 66737870            # KBinaryFile3: fixed point, RGB colours
START_NEW_ELEM, END_ELEM, END_FILE, CDATA = 1000, 254, 255, 253
NONE_COLOR = 0x01ffffff
ONE_FIX = 1 << 16

ELEMENTS = {"svg": 0, "defs": 3, "desc": 4, "title": 7, "style": 24, "g": 11, "switch": 15, "a": 18,
            "image": 22, "text": 25, "use": 26, "circle": 27, "ellipse": 28, "line": 29, "path": 30,
            "polygon": 31, "polyline": 32, "rect": 33, "linearGradient": 40, "radialGradient": 41, "stop": 42,
            "textArea": 44, "solidColor": 46}

O = 25
# attribute -> (id, kind)
ATTRS = {
    "fill": (0, "paint"), "stroke": (1, "color"), "stroke-width": (2, "fix"), "visibility": (3, "enum"),
    "font-family": (4, "str"), "font-size": (5, "fix"), "font-style": (6, "enum"), "font-weight": (7, "enum"),
    "stroke-dasharray": (8, "dash"), "display": (9, "enum"), "fill-rule": (10, "str"),
    "stroke-linecap": (11, "str"), "stroke-linejoin": (12, "str"), "stroke-dashoffset": (13, "fix"),
    "stroke-miterlimit": (14, "fix"), "color": (15, "color"), "text-anchor": (16, "enum"),
    "text-decoration": (17, "enum"), "fill-opacity": (22, "fix"), "stroke-opacity": (23, "fix"),
    "opacity": (24, "fix"),
    "width": (1 + O, "fix"), "height": (2 + O, "fix"), "r": (3 + O, "fix"), "rx": (4 + O, "fix"),
    "ry": (5 + O, "fix"), "cx": (21 + O, "fix"), "cy": (22 + O, "fix"), "y": (23 + O, "fix"), "x": (24 + O, "fix"),
    "y1": (25 + O, "fix"), "y2": (26 + O, "fix"), "x1": (27 + O, "fix"), "x2": (28 + O, "fix"),
    "transform": (41 + O, "transform"), "version": (51 + O, "fix"), "points": (53 + O, "points"),
    "d": (54 + O, "path"), "stop-color": (56 + O, "color"), "fx": (57 + O, "fix"), "fy": (58 + O, "fix"),
    "offset": (59 + O, "offset"), "spreadMethod": (60 + O, "spread"), "gradientUnits": (61 + O, "units"),
    "stop-opacity": (62 + O, "fix"), "viewBox": (63 + O, "viewbox"), "baseProfile": (64 + O, "str"),
    "zoomAndPan": (65 + O, "zoom"), "id": (67 + O, "str"), "xml:base": (68 + O, "str"),
    "xml:lang": (69 + O, "str"), "xml:space": (70 + O, "str"), "requiredExtensions": (71 + O, "list"),
    "requiredFeatures": (72 + O, "list"), "systemLanguage": (73 + O, "list"),
    "xlink:actuate": (78 + O, "href"), "xlink:arcrole": (79 + O, "href"), "xlink:role": (80 + O, "href"),
    "xlink:show": (81 + O, "href"), "xlink:title": (82 + O, "href"), "xlink:type": (83 + O, "href"),
    "xlink:href": (84 + O, "href"), "gradientTransform": (98 + O, "gtransform"),
}
ENUMS = {"display": {"inline": 0, "none": 16}, "visibility": {"visible": 0, "hidden": 1}}
NS = {"http://www.w3.org/1999/xlink": "xlink:", "http://www.w3.org/XML/1998/namespace": "xml:"}


def name_of(k: str) -> str:
    if k.startswith("{"):
        ns, local = k[1:].split("}")
        return NS[ns] + local if ns in NS else None      # other namespaces (inkscape:, sodipodi: ...) dropped
    return k


def num(v: str) -> int:
    return N.fix(N.NUM.match(v.strip()).group())


def s16(t: str) -> bytes:
    b = t.encode("utf-16-le")
    return struct.pack("<B", len(b)) + b


def color(v: str) -> int:
    if v.strip() == "none":
        return NONE_COLOR
    c = N.color(v)
    return 0 if c is None else c


def path_value(p) -> bytes:
    code = {N.MOVE: 0, N.LINE: 1, N.QUAD: 2, N.CUBIC: 3, N.CLOSE: 4}
    types = bytes(code[s & ~1] for s in p.segs)
    return struct.pack("<H", len(types)) + types + struct.pack("<H", len(p.coords)) + \
        struct.pack(f"<{len(p.coords)}i", *p.coords)


def value(name: str, kind: str, v: str) -> bytes | None:
    if kind == "fix":
        return struct.pack("<i", num(v))
    if kind == "str":
        return s16(v)
    if kind == "color":
        return struct.pack("<I", color(v))
    if kind == "paint":
        m = re.match(r"url\(\s*#([^)\s]+)\s*\)", v.strip())
        if m:
            return b"\1" + s16(m.group(1))
        return b"\0" + struct.pack("<I", color(v))
    if kind == "enum":
        e = ENUMS.get(name, {}).get(v.strip())
        return None if e is None else struct.pack("<i", e)
    if kind == "dash":
        vals = N.nums(v)
        return struct.pack("<b", len(vals)) + struct.pack(f"<{len(vals)}i", *vals)
    if kind == "path":
        return path_value(N.parse_d(v))
    if kind == "viewbox":
        vals = N.nums(v)
        return struct.pack("<4i", *vals[:4]) if len(vals) >= 4 else None
    if kind == "zoom":
        return struct.pack("<b", 1 if v.strip() == "magnify" else 0)
    if kind == "units":
        return struct.pack("<b", 0 if v.strip() == "userSpaceOnUse" else 1)
    if kind == "spread":
        return struct.pack("<b", {"pad": 0, "reflect": 1, "repeat": 2}.get(v.strip(), 0))
    if kind == "offset":
        return struct.pack("<i", min(num(v), ONE_FIX))       # no percentages; clamped to 1
    if kind == "list":
        items = v.split()
        return struct.pack("<b", len(items)) + b"".join(s16(i) for i in items)
    if kind == "href":
        return s16("")              # the encoder writes xlink: values empty
    if kind == "transform":
        m = N.parse_transform(v)
        return struct.pack("<6iI", m.m00, m.m01, m.m02, m.m10, m.m11, m.m12, m.type)
    if kind == "gtransform":
        m = N.parse_transform(v)
        return struct.pack("<6i", m.m00, m.m01, m.m02, m.m10, m.m11, m.m12)
    return None


def attributes(el):
    out = []
    for k, v in el.attrib.items():
        k = name_of(k)
        if k is None:
            continue
        if k == "style":
            for decl in v.split(";"):
                if ":" in decl:
                    dk, dv = decl.split(":", 1)
                    out.append((dk.strip(), dv.strip()))
        else:
            out.append((k, v))
    return out


def points_value(tag: str, v: str) -> bytes:
    vals = N.nums(v)
    p = N.Path()
    p.add(N.MOVE, vals[0], vals[1])
    for k in range(2, len(vals) - 1, 2):
        p.add(N.LINE, vals[k], vals[k + 1])
    if tag == "polygon":
        p.add(N.CLOSE)
    return path_value(p)


def element(el, out: bytearray):
    tag = el.tag.split("}")[-1]
    eid = ELEMENTS.get(tag)
    if eid is None:
        return
    out += bytes([eid])
    for k, v in attributes(el):
        if k not in ATTRS:
            continue
        aid, kind = ATTRS[k]
        if kind == "points":
            b = points_value(tag, v)
        elif tag == "svg" and k in ("width", "height"):
            pct = v.strip().endswith("%")
            b = struct.pack("<Bi", 1 if pct else 0, num(v.strip().rstrip("%")))
        else:
            b = value(k, kind, v)
        if b is None:
            continue
        out += struct.pack("<H", aid) + b
    out += struct.pack("<H", START_NEW_ELEM)
    if tag == "text":
        out += bytes([CDATA]) + s16(el.text or "")
    for ch in el:
        element(ch, out)
    out += bytes([END_ELEM])


FALLBACK_ELEMENTS = {"switch", "foreignObject", "text", "desc", "style"}
FALLBACK_ATTRS = {"requiredExtensions", "content", "class", "display", "opacity"}


def needs_svgb(svg: bytes) -> bool:
    """Whether svgtbinencode writes this document as SVG-T binary instead of NVG: what it checks are the XML
    attributes themselves (a style="opacity:..." or "fill-rule:evenodd" property stays NVG). Agrees with the
    original on 99.6% of 4900 documents of the release (all of the S60 skin's)."""
    root = ET.fromstring(svg)
    for el in root.iter():
        tag = el.tag.split("}")[-1]
        if tag in FALLBACK_ELEMENTS:
            return True
        a = {k.split("}")[-1]: v for k, v in el.attrib.items()}
        if FALLBACK_ATTRS & a.keys():
            return True
        if a.get("fill-rule", "").strip() == "evenodd":
            return True
        if a.get("stroke-dasharray", "none").strip() != "none":
            return True
        if "href" in a and tag in ("linearGradient", "radialGradient"):
            return True
    return False


def encode(svg: bytes) -> bytes:
    root = ET.fromstring(svg)
    out = bytearray(struct.pack("<I", MAGIC))
    element(root, out)
    out += bytes([END_FILE])
    return bytes(out)


if __name__ == "__main__":
    src = sys.argv[1]
    dst = sys.argv[2] if len(sys.argv) > 2 else src + "b"
    with open(src, "rb") as fh:
        data = encode(fh.read())
    with open(dst, "wb") as fh:
        fh.write(data)
