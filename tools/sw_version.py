# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""sw_version.py -- OURS: write Z:\\resource\\versions\\sw.txt for a Phorepoc ROM (X4.59).

SysUtil::GetSWVersion() reads this file (or sysutilsetup's property made from it). apparc keeps its
application list in C:\\private\\10003a3f\\AppsListCache and throws the cache away only when the stored
software version differs from GetSWVersion() -- the firmware-update case. A ROM without sw.txt reports
the same (empty) version forever, so a persistent C: disk kept a stale list across ROM rebuilds and a
newly registered app was "not found" (Messaging as the foreground app). One version per ROM build.

Format as the release's own sample (os.deviceplatformrelease/Version/data/sw.txt): UTF-16LE with a BOM,
fields separated by the two characters backslash + n (not a newline):
    V <version>\\n<dd-mm-yy>\\n<model>\\n(c)<vendor>

    python3 tools/sw_version.py <out sw.txt> [model]
"""
import sys
import time

BACKSLASH_N = "\\" + "n"          # the literal two-character separator


def main() -> int:
    out = sys.argv[1]
    t = time.localtime()
    fields = ["V Phorepoc_" + time.strftime("%Y%m%d_%H%M%S", t),
              time.strftime("%d-%m-%y", t),
              (sys.argv[2] if len(sys.argv) > 2 else "Phorepoc"),
              "(c)Phorepoc"]
    with open(out, "wb") as fh:
        fh.write(b"\xff\xfe" + BACKSLASH_N.join(fields).encode("utf-16-le"))
    print(fields[0])
    return 0


if __name__ == "__main__":
    sys.exit(main())
