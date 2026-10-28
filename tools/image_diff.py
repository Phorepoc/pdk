# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""image_diff -- compare two Phorepoc ROM placement maps (the <image>.map next to an ARM/x86 image): which files one
image has and the other lacks, and which have a different size.

    python3 tools/image_diff.py A.map B.map [--min N] [--ext dll,exe,...]

A map line is `<name> <run address> <size> <alignment>`; names are compared case-insensitively. A different size means a
different build of the same component (compile flags, an extra source, a trace shim, a different registry entry): look the
component up in the registry (core/registry/components.py and the adaptations' registry/) to see what drives it.
"""
import argparse


def read(path, exts):
    d = {}
    for line in open(path, encoding="utf8", errors="replace"):
        t = line.split()
        if len(t) < 3:
            continue
        name = t[0].lower()
        if exts and name.rsplit(".", 1)[-1] not in exts:
            continue
        try:
            d[name] = int(t[2], 16)
        except ValueError:
            continue
    return d


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("a")
    ap.add_argument("b")
    ap.add_argument("--min", type=int, default=1, help="ignore size differences smaller than this many bytes")
    ap.add_argument("--ext", default="dll,exe,ldd,pdd,fsy,csy,nif,prt,tsy,mdl,dsy,plg,pdl,agt,drv",
                    help="comma list of extensions to compare (empty = all)")
    a = ap.parse_args()
    exts = set(x for x in a.ext.split(",") if x)
    A, B = read(a.a, exts), read(a.b, exts)
    print("A: %d files   B: %d files" % (len(A), len(B)))
    print("only in A (%d): %s" % (len([k for k in A if k not in B]), " ".join(sorted(k for k in A if k not in B))))
    print("only in B (%d): %s" % (len([k for k in B if k not in A]), " ".join(sorted(k for k in B if k not in A))))
    diff = sorted(((k, A[k], B[k]) for k in A if k in B and abs(A[k] - B[k]) >= a.min), key=lambda t: -abs(t[1] - t[2]))
    print("size differs (%d), largest first:" % len(diff))
    for k, x, y in diff:
        print("  %-36s %10d %10d %+d" % (k, x, y, y - x))


if __name__ == "__main__":
    main()
