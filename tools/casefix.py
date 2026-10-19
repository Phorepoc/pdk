"""casefix -- make the case-insensitive source tree usable on a case-sensitive file system.

The 2010 source was written for Windows: `#include <f32plugin.h>` finds `F32plugin.h`, `"FbsMessage.h"` finds
`FBSMESSAGE.H`, `<graphics/x.h>` finds `inc/Graphics/x.h`. On Linux those includes fail. This script scans every
#include in the source tree and, for each spelling that names a header under another case, links that spelling into
.casefix/ (`.casefix/FbsMessage.h -> .../FBSMESSAGE.H`). The compiler and the preprocessor search the directory last
(-idirafter), so it only supplies a header the exact-case search did not find. No source file is touched.

    python3 tools/casefix.py [package ...]    (after tools/epoc32_export.py: the exported headers are indexed too)
    python3 tools/casefix.py --all            every package of both source trees (epoc32_export.py --all runs this itself)

The packages (directories of symbian-2010/ or ../symbian-rcl3/<package>) default to the set epoc32_export.py exports;
name more to cover further components. The links go to $PHOREPOC_BUILD/casefix (add it to the compiler with -idirafter).
"""
import os
import re

import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import SYMBIAN, BUILD, EPOC32_ROOT  # noqa: E402

OUT = os.path.join(BUILD, "casefix")
HEADER_EXT = {".h", ".inl", ".hrh", ".mmh", ".rh", ".ra", ".rsg", ".hpp", ".mph", ".inc", ".pan", ".rls", ".loc"}
SCAN_EXT = HEADER_EXT | {".cpp", ".c", ".cia", ".rss", ".mmp", ".cc", ".s"}
INCLUDE = re.compile(rb'^\s*#\s*include\s*[<"]([^>"\r\n]+)[>"]', re.M)


def main(pkgs: list[str] | None = None) -> None:
    import epoc32_export
    if pkgs is None:
        pkgs = epoc32_export.all_packages() if sys.argv[1:2] == ["--all"] else sys.argv[1:] or epoc32_export.DEFAULT_PKGS
    roots = [os.path.join(EPOC32_ROOT, "include")] + [os.path.normpath(os.path.join(SYMBIAN, "symbian-2010", p)) for p in pkgs]
    by_base, by_tail = {}, {}
    scan = []
    for top in roots:
        for root, dirs, files in os.walk(top):
            dirs[:] = [d for d in dirs if d != ".git"]
            for f in files:
                ext = os.path.splitext(f)[1].lower()
                p = os.path.join(root, f)
                if ext in HEADER_EXT:
                    low = f.lower()
                    by_base.setdefault(low, p)
                    by_tail.setdefault(os.path.basename(root).lower() + "/" + low, p)
                if ext in SCAN_EXT and top != roots[0]:
                    scan.append(p)
    spellings = set()
    for p in scan:
        try:
            data = open(p, "rb").read()
        except OSError:
            continue
        spellings.update(m.group(1).decode("latin-1").strip() for m in INCLUDE.finditer(data))
    os.makedirs(OUT, exist_ok=True)
    n = 0
    for s in sorted(spellings):
        if "\\" in s or s.startswith(("/", ".")) or ".." in s:
            continue
        parts = s.lower().split("/")
        hit = by_tail.get("/".join(parts[-2:])) if len(parts) > 1 else None
        hit = hit or by_base.get(parts[-1])
        if hit is None:
            continue
        link = os.path.join(OUT, *s.split("/"))
        if os.path.lexists(link):
            continue
        os.makedirs(os.path.dirname(link), exist_ok=True)
        os.symlink(hit, link)
        n += 1
    print("casefix: %d links in %s" % (n, OUT))


if __name__ == "__main__":
    main()
