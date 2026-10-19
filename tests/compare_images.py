#!/usr/bin/env python3
"""compare_images -- byte-compare images built by the pdk tools with images built by another build of the same keys.

    compare_images.py [--ref ROOT] [--build DIR] [--table tests/compare_*.tsv ...]
    compare_images.py A B                      two files, or two directories (same relative names)

A table has one row per image:  key <TAB> arch <TAB> reference path (relative to --ref) <TAB> core path (relative to
--build) <TAB> note.  --ref defaults to $PHOREPOC_OS_ROOT, --build to $PHOREPOC_BUILD (<core>/build).  Result per row:
IDENTICAL, or DIFFERENT with the size of both files, the number of differing bytes and the first offsets.
Exit status 0 when every row is identical (a row whose note starts with "expected:" is reported but does not fail).
"""
import glob
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
CORE = os.path.abspath(os.environ.get("PHOREPOC_CORE") or os.path.join(os.path.dirname(HERE), "..", "core"))


def diff(a: bytes, b: bytes) -> str:
    if a == b:
        return "IDENTICAL"
    n = min(len(a), len(b))
    offs = [i for i in range(n) if a[i] != b[i]]
    head = ", ".join("0x%X" % o for o in offs[:6])
    return "DIFFERENT (%d vs %d bytes, %d bytes differ%s%s)" % (
        len(a), len(b), len(offs) + abs(len(a) - len(b)), "; first at " + head if offs else "",
        "; length differs" if len(a) != len(b) else "")


def pair(a: str, b: str) -> str:
    if not os.path.isfile(a):
        return "MISSING reference " + a
    if not os.path.isfile(b):
        return "MISSING core " + b
    return diff(open(a, "rb").read(), open(b, "rb").read())


def main(argv) -> int:
    ref = os.environ.get("PHOREPOC_OS_ROOT", "")
    build = os.environ.get("PHOREPOC_BUILD", os.path.join(CORE, "build"))
    tables, pos = [], []
    it = iter(argv)
    for a in it:
        if a == "--ref":
            ref = next(it)
        elif a == "--build":
            build = next(it)
        elif a == "--table":
            tables.append(next(it))
        else:
            pos.append(a)
    if len(pos) == 2 and not tables:
        a, b = pos
        if os.path.isdir(a):
            bad = 0
            for root, _, files in os.walk(a):
                for f in sorted(files):
                    pa = os.path.join(root, f)
                    pb = os.path.join(b, os.path.relpath(pa, a))
                    r = pair(pa, pb)
                    bad += r != "IDENTICAL"
                    print("%-60s %s" % (os.path.relpath(pa, a), r))
            return 1 if bad else 0
        r = pair(a, b)
        print(r)
        return 0 if r == "IDENTICAL" else 1
    tables = tables or sorted(glob.glob(os.path.join(HERE, "compare_*.tsv")))
    bad = total = 0
    print("%-28s %-5s %s" % ("key", "arch", "result"))
    for t in tables:
        for line in open(t, encoding="utf-8"):
            if not line.strip() or line.startswith("#"):
                continue
            f = line.rstrip("\n").split("\t") + [""] * 5
            key, arch, rp, cp, note = f[:5]
            r = pair(os.path.join(ref, rp), os.path.join(build, cp))
            total += 1
            expected = note.startswith("expected:")
            if r != "IDENTICAL" and not expected:
                bad += 1
            print("%-28s %-5s %s%s" % (key, arch, r, ("   [" + note + "]") if note else ""))
    print("%d compared, %d not identical" % (total, bad))
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
