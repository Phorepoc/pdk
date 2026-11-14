# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

"""unimpl_exports -- the provider side of the stub audit: exports an image serves with the unimplemented-export stub.

    python3 tools/unimpl_exports.py [--arch x32|arm] [--rom ARGS] [--names] [--only image,image]

The emitter writes `<image>.unimpl` next to every image: one line per .def ordinal the image does not implement, as
    ABSENT <ordinal> <name>     the .def marks it ABSENT: retired in the release the .def comes from -- the real
                                library does not provide it either (fine)
    GAP    <ordinal> <name>     a real export nothing provides: a caller that binds it dies EUSRNIMP (a gap)
Prints the count of each per image (the GAPs are the work), and with --names every GAP. --rom limits the report to the
images a ROM stages (an args file's Sys\\bin\\X=<built file> lines); stub_audit.py check covers the consumer side.
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import stub_audit as SA  # noqa: E402


def main():
    args = sys.argv[1:]
    arch = os.environ.get("PHOREPOC_ARCH", "x32")
    rom, names, only = None, False, None
    while args:
        a = args.pop(0)
        if a == "--arch":
            arch = args.pop(0)
        elif a == "--rom":
            rom = args.pop(0)
        elif a == "--names":
            names = True
        elif a == "--only":
            only = set(args.pop(0).split(","))
        else:
            sys.exit(__doc__)
    rows, norec = [], []
    for image, built in SA.images(arch, rom):
        if only and image not in only:
            continue
        side = built + ".unimpl"
        if not os.path.exists(side):
            norec.append(image)
            continue
        ents = [l.split(None, 2) for l in open(side).read().splitlines() if l.strip()]
        # a typeinfo ordinal (_ZTI/_ZTS) of a component built without RTTI: its importers synthesize the typeinfo from the
        # name (build_mmp*.py), so it is no EUSRNIMP gap -- counted apart
        ti = [e for e in ents if e[0] == "GAP" and e[2].startswith(("_ZTI", "_ZTS"))]
        rows.append((image, [e for e in ents if e[0] == "GAP" and e not in ti], sum(1 for e in ents if e[0] == "ABSENT"), len(ti)))
    gaps = sum(len(g) for _, g, _, _ in rows)
    print(f"{arch}: {len(rows)} images with a record, {gaps} GAP ordinals in {sum(1 for _, g, _, _ in rows if g)} images, "
          f"{sum(a for _, _, a, _ in rows)} ABSENT (fine), {len(norec)} without a record")
    for image, g, absent, ti in sorted(rows, key=lambda r: -len(r[1])):
        if g:
            print(f"  {image}: {len(g)} GAP, {absent} ABSENT, {ti} typeinfo")
            if names:
                for dm, (_k, o, n) in zip(SA.demangle([e[2] for e in g]), g):
                    print(f"      @{o} {dm}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
