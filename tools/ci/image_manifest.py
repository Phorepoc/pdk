#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

"""image_manifest -- what a build tree ships, as a list two builds can be compared by: the proof that a build started from
a published layer (core's or a board's) made what a build from the core layer alone makes.

    image_manifest.py write BUILD [OUT]     one line per file: "E32 <path> <sha256>" for every E32 image (its header's
                                            timestamp and header CRC left out: only the content counts) and
                                            "MAP <path> <sha256>" for every ROM placement map (the list of its files and
                                            their sizes, not where the run put them)
    image_manifest.py diff A B              what only one has, what differs; exit 1 unless they are identical

Object files, dependency files, logs and the regress folders are not part of it. Times, the order the files were made in
and the compile cache's own bookkeeping never are.
"""
import hashlib
import os
import sys

SKIP_DIRS = {".git", "regress", "__pycache__"}
SKIP_EXT = {".o", ".d", ".key", ".cmd", ".log", ".a", ".tmp", ".zst", ".xz", ".gz", ".iso", ".ova", ".img", ".zip"}


def e32_digest(path, head):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        data = bytearray(f.read())
    data[20:24] = b"\0\0\0\0"        # iHeaderCrc (covers the time)
    data[36:44] = b"\0" * 8          # iTimeLo, iTimeHi
    h.update(data)
    return h.hexdigest()


def map_digest(path):
    rows = []
    for line in open(path, encoding="utf8", errors="replace"):
        t = line.split()
        if len(t) >= 3:
            rows.append("%s %s" % (t[0].lower(), t[2]))      # name, size (not the run address)
    return hashlib.sha256("\n".join(sorted(rows)).encode()).hexdigest(), len(rows)


def write(build, out):
    lines = []
    for root, dirs, files in os.walk(build):
        dirs[:] = sorted(d for d in dirs if d not in SKIP_DIRS)
        for n in sorted(files):
            p = os.path.join(root, n)
            ext = os.path.splitext(n)[1].lower()
            if ext in SKIP_EXT or os.path.islink(p) or not os.path.isfile(p):
                continue
            rel = os.path.relpath(p, build).replace(os.sep, "/")
            try:
                if ext == ".map":
                    d, k = map_digest(p)
                    lines.append("MAP %s %s %d" % (rel, d, k))
                    continue
                if os.path.getsize(p) < 64:
                    continue
                with open(p, "rb") as f:
                    head = f.read(24)
                if head[16:20] == b"EPOC":
                    lines.append("E32 %s %s" % (rel, e32_digest(p, head)))
            except OSError:
                pass
    text = "\n".join(sorted(lines)) + "\n"
    if out:
        open(out, "w", newline="\n").write(text)
    else:
        sys.stdout.write(text)
    print("image_manifest: %d E32 images, %d ROM maps" % (sum(l.startswith("E32") for l in lines),
                                                            sum(l.startswith("MAP") for l in lines)), file=sys.stderr)


def read(path):
    d = {}
    for line in open(path, encoding="utf8"):
        t = line.rstrip("\n").split(" ", 2)
        if len(t) == 3:
            d[(t[0], t[1])] = t[2]
    return d


def diff(a, b):
    A, B = read(a), read(b)
    onlya, onlyb = sorted(set(A) - set(B)), sorted(set(B) - set(A))
    differs = sorted(k for k in set(A) & set(B) if A[k] != B[k])
    print("A: %d entries   B: %d entries" % (len(A), len(B)))
    for title, ks in (("only in A", onlya), ("only in B", onlyb), ("different content", differs)):
        print("%s: %d" % (title, len(ks)))
        for k in ks[:200]:
            print("  %s %s" % k)
    same = not (onlya or onlyb or differs)
    print("IDENTICAL" if same else "DIFFERENT")
    return 0 if same else 1


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "write":
        write(sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else None)
    elif len(sys.argv) == 4 and sys.argv[1] == "diff":
        sys.exit(diff(sys.argv[2], sys.argv[3]))
    else:
        sys.exit(__doc__)
