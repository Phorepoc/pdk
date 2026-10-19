#!/usr/bin/env python3
"""cenrep_set.py -- write a copy of a CenRep initialisation text file (UTF-16, as BUILD/cenrep-gen/*.txt) with some int keys changed.

    python3 tools/cenrep_set.py <in.txt> <out.txt> 0x6=3600 0x8=3600

Each KEY=VALUE replaces the value of the existing `KEY int <value> ...` line (the key may be written 0x6 / 0x00000006: the match is numeric);
a missing key is an error. Used by tools/make_phorepoc_rom.sh for per-run overrides (e.g. ALF test images keep the display on).
"""
import re
import sys


def main():
    src, dst, *pairs = sys.argv[1:]
    text = open(src, encoding="utf-16", newline="").read()
    for pair in pairs:
        key, val = pair.split("=")
        k = int(key, 0)
        out, n = [], 0
        for line in text.split("\n"):
            m = re.match(r"^\s*0x([0-9A-Fa-f]+)\s+int\s+(-?\d+)(.*)$", line.rstrip("\r"))
            if m and int(m.group(1), 16) == k:
                line = "0x%08X int %s%s" % (k, val, m.group(3)) + ("\r" if line.endswith("\r") else "")
                n += 1
            out.append(line)
        if n != 1:
            raise SystemExit("cenrep_set: key %s found %d times in %s" % (key, n, src))
        text = "\n".join(out)
    with open(dst, "w", encoding="utf-16", newline="") as fh:
        fh.write(text)


if __name__ == "__main__":
    main()
