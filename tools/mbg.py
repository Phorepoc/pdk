# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""mbg -- generate the icon-ID headers (<x>.mbg) that S60's mifconv build step emits.

Sources compile against `#include <avkon.mbg>` etc. -- an enum of MIF icon ids.
The build declares them in bld.inf:

    START EXTENSION s60/mifconv
    OPTION TARGETFILE avkon2.mif
    OPTION HEADERFILE avkon.mbg
    OPTION SOURCEFILE Iconlist.txt          (or: OPTION SOURCES -c8,8 icon1 -c8 icon2 ...)
    END

Format replicated from mifconv_bitmapheaderconverter.cpp (ids start at 16384, every
icon takes TWO ids -- bitmap + mask slot -- the `_mask` enumerator only when the
depth spec has a mask depth, e.g. `/c8,8`).

    python3 tools/mbg.py [package ...]      # default: the mw packages we build from (writes BUILD/epoc32/include)
    python3 tools/mbg.py --all              # every package of both source trees
"""
from __future__ import annotations

import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import SYMBIAN, EPOC32_ROOT  # noqa: E402

BASE = os.path.join(SYMBIAN, "symbian-2010")
OUT = os.path.join(EPOC32_ROOT, "include")
DEFAULT_PKGS = ["../symbian-rcl3/oss.FCL.sf.mw.classicui", "../symbian-rcl3/oss.FCL.sf.mw.uiresources",
                "../symbian-rcl3/oss.FCL.sf.mw.appsupport", "../symbian-rcl3/oss.FCL.sf.mw.inputmethods"]
BLOCK = re.compile(r"START\s+EXTENSION\s+\S*mifconv\b(.*?)^\s*END\b", re.I | re.S | re.M)


def block_options(blk: str) -> dict[str, str]:
    """The OPTIONs of one mifconv block. `OPTION SOURCES -c8,8 a \\` + continuation lines = one
    option; a line that does not start a new OPTION continues the previous one -- which also covers
    preprocessed text (inf_text), where cpp keeps the lines but drops the backslashes."""
    opts: dict[str, str] = {}
    key = None
    blk = re.sub(r"/\*.*?\*/", " ", blk, flags=re.S)        # gsconnsettingsplugin comments its HEADERFILE
    for line in re.sub(r"\\[ \t]*\r?\n", " ", blk).splitlines():
        m = re.match(r"\s*OPTION\s+(\w+)\s*(.*)", line, re.I)   # vmbxengine writes `option`
        if m:
            key = m.group(1).upper()
            opts[key] = (opts.get(key, "") + " " + m.group(2).rstrip(" \\")).strip()
        elif key and line.strip() and not line.lstrip().startswith("#"):
            opts[key] = (opts[key] + " " + line.strip().rstrip(" \\")).strip()
    return opts


def inf_text(path: str) -> str:
    """A bld.inf as the build sees it. With conditionals (`#ifdef RD_MULTIPLE_DRIVE ... #else`
    inside a mifconv block: filemanager) BOTH branches used to be read -> duplicate enum entries;
    such a file goes through the same cpp + platform/variant macros as tools/epoc32_export.py."""
    text = open(path, errors="replace").read()
    if not re.search(r"^\s*#\s*if", text, re.M):
        return text
    import epoc32_export as EX
    return EX.preprocess(path) or text
DEPTH = re.compile(r"^[-/]c?\d+(,\d+)?$", re.I)


def unadorned(name: str) -> str:
    stem = re.split(r"[\\/]", name)[-1].split(".")[0].lower()
    return stem[:1].upper() + stem[1:]


def parse_items(tokens: list[str]) -> list[tuple[str, bool]]:
    """[-/]c<depth>[,<maskdepth>] <file> ... -> [(file, has_mask)]"""
    items, mask = [], False
    for t in tokens:
        if DEPTH.match(t):
            mask = "," in t
        elif t and not t.startswith(("-", "/")) or re.search(r"\.(svg|bmp)$", t, re.I):
            items.append((t, mask))
            mask = False
    return items


def write_mbg(header: str, items: list[tuple[str, bool]]) -> str:
    hb = unadorned(header)
    out = ["", "/* This file has been generated, DO NOT MODIFY. */", f"enum TMif{hb}", "\t{"]
    cur = 16384
    for name, mask in items:
        out.append(f"\tEMbm{hb}{unadorned(name)} = {cur},")
        if mask:
            out.append(f"\tEMbm{hb}{unadorned(name)}_mask = {cur + 1},")
        cur += 2
    out += [f"\tEMbm{hb}LastElement", "\t};", ""]
    if not os.path.basename(header):        # HEADERFILE naming a directory (ipconnmgmt): nothing to write
        print(f"  ! HEADERFILE {header!r} has no file name -- skipped")
        return None
    path = os.path.join(OUT, os.path.basename(header).lower())
    try:
        with open(path, "w") as fh:
            fh.write("\n".join(out))
    except OSError as e:        # a bld.inf whose HEADERFILE is an unexpanded macro / not a file name: skip it, not the whole run
        print(f"  ! HEADERFILE {header!r}: {e} -- skipped")
        return None
    return path


BITMAP = re.compile(r"^\s*START\s+BITMAP\s+(\S+)(.*?)^\s*END\b", re.I | re.S | re.M)


def write_bmconv_mbg(mbm: str, blk: str) -> str | None:
    """An mmp `START BITMAP <x>.mbm ... HEADER ... END` block -> the bmconv header <x>.mbg: one
    plain enumerator per SOURCE bitmap, in order, numbered from 0 (masks are their own entries;
    format of bmconv's BitmapCompiler::WriteHeader)."""
    if not re.search(r"^\s*HEADER\b", blk, re.I | re.M) or not re.match(r"^[\w./\\-]+$", mbm):
        return None                             # no header, or a macro target (`?target`)
    hb = unadorned(mbm)
    names = []
    for m in re.finditer(r"^\s*SOURCE\s+(.*)$", blk, re.I | re.M):
        names += [t for t in m.group(1).split() if not DEPTH.match(t) and not re.match(r"^c?\d+$", t, re.I)]
    stem = os.path.basename(mbm).split(".")[0].lower()
    out = [f"// {stem}.mbg", "// Generated by BitmapCompiler", "//", "", f"enum TMbm{hb}", "\t{"]
    out += [f"\tEMbm{hb}{unadorned(n)}," for n in names]
    out[-1] = out[-1].rstrip(",")
    out += ["\t};", ""]
    path = os.path.join(OUT, stem + ".mbg")
    with open(path, "w", newline="\n") as fh:
        fh.write("\n".join(out))
    return path


def mk_mifconv(path: str) -> list[tuple[str, str, list[str]]]:
    """An icon MAKEFILE (the older S60 form, e.g. pushmtm/Group/PushMtmIcons.mk: a RESOURCE target running
    `mifconv $(ICONTARGETFILENAME) /h$(HEADERFILENAME) /c8,1 a.bmp ...`) -> [(mif file, header file, tokens)].
    The make variables are resolved from the file's own `NAME=value` lines."""
    text = open(path, errors="replace").read()
    text = re.sub(r"\\[ \t]*\r?\n", " ", text)                  # continuation lines
    var = {m.group(1): m.group(2).strip() for m in re.finditer(r"^(\w+)\s*=\s*(.*)$", text, re.M)}

    def expand(s: str) -> str:
        for _ in range(4):
            s = re.sub(r"\$\((\w+)\)", lambda m: var.get(m.group(1), ""), s)
        return s
    out = []
    for m in re.finditer(r"^\s*mifconv\s+(.*)$", text, re.M | re.I):
        line = m.group(1)
        # $< = the rule's first prerequisite (AvKon: `$(TARGETS_AVKON) ... : iconlist.txt ...` then `mifconv ... /F$<`)
        rule = [r for r in re.finditer(r"^[^\s#][^:=\n]*:(?!=)\s*(\S+)", text[:m.start()], re.M)]
        if rule:
            line = line.replace("$<", expand(rule[-1].group(1)))
        toks = expand(line).split()
        if not toks:
            continue
        mif = re.split(r"[\\/]", toks[0])[-1]
        hdr = next((re.split(r"[\\/]", t[2:])[-1] for t in toks[1:] if t[:2].lower() in ("/h", "-h")), None)
        rest = []
        for t in toks[1:]:
            if t[:2].lower() in ("/h", "-h"):
                continue
            # /F<file> = mifconv's response file, relative to the makefile (CommonDialogs: /Ficonlist.txt). Unexpanded, mifconv got
            # no icons -> an empty commondialogs.mif -> File Manager's lazy icon load failed and the error code became a list icon
            # index (EIKCTL 42, EEikPanicColumnListInvalidBitmapIndex) in any folder holding .sis/.zip/.jar files (2026-10-12).
            if t[:2].lower() in ("/f", "-f"):
                rf = os.path.join(os.path.dirname(path), t[2:])
                if not os.path.exists(rf):      # the makefile's case may not be the file's (iconlist.txt / Iconlist.txt)
                    d = os.path.dirname(rf) or "."
                    rf = next((os.path.join(d, f) for f in (os.listdir(d) if os.path.isdir(d) else [])
                               if f.lower() == os.path.basename(rf).lower()), rf)
                if os.path.exists(rf):
                    rest += open(rf, errors="replace").read().split()
                    continue
            rest.append(t)
        if hdr:
            out.append((mif, hdr, rest))
    return out


def run(pkgs: list[str]) -> None:
    n = 0
    for pkg in pkgs:                       # icon makefiles (*.mk running mifconv)
        for root, _dirs, files in os.walk(os.path.join(BASE, pkg)):
            for f in files:
                if f.lower().endswith(".mk"):
                    for _mif, hdr, toks in mk_mifconv(os.path.join(root, f)):
                        if write_mbg(hdr, parse_items(toks)):
                            n += 1
    for pkg in pkgs:                       # bmconv headers from mmp START BITMAP blocks
        for root, _dirs, files in os.walk(os.path.join(BASE, pkg)):
            for f in files:
                if f.lower().endswith((".mmp", ".mmpi", ".mmh")):
                    for mbm, blk in BITMAP.findall(open(os.path.join(root, f), errors="replace").read()):
                        if write_bmconv_mbg(mbm, blk):
                            n += 1
    for pkg in pkgs:
        for root, _dirs, files in os.walk(os.path.join(BASE, pkg)):
            for f in files:
                if f.lower() != "bld.inf":
                    continue
                text = inf_text(os.path.join(root, f))
                for blk in BLOCK.findall(text):
                    opts = block_options(blk)
                    if "HEADERFILE" not in opts:
                        continue
                    if "SOURCEFILE" in opts:
                        lst = os.path.join(root, opts["SOURCEFILE"].strip())
                        if not os.path.isfile(lst):
                            cand = [x for x in os.listdir(root)
                                    if x.lower() == opts["SOURCEFILE"].strip().lower()]
                            if not cand:
                                print(f"  ! {opts['HEADERFILE']}: list {opts['SOURCEFILE']} missing")
                                continue
                            lst = os.path.join(root, cand[0])
                        toks = open(lst, errors="replace").read().split()
                    else:
                        toks = opts.get("SOURCES", "").split()
                    items = parse_items(toks)
                    write_mbg(opts["HEADERFILE"].strip(), items)
                    n += 1
    print(f"mbg: {n} headers -> {OUT}")


if __name__ == "__main__":
    if sys.argv[1:2] == ["--all"]:
        import epoc32_export
        run(epoc32_export.all_packages())
    else:
        run(sys.argv[1:] or DEFAULT_PKGS)
