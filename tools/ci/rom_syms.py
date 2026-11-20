# Copyright The Phorepoc Authors
# SPDX-License-Identifier: EPL-2.0 OR GPL-2.0-or-later

"""rom_syms -- the ROM's symbol export, for PhoneLab (fault addresses, hotspots, thread dumps).

    python3 tools/ci/rom_syms.py [--build <PHOREPOC_BUILD>] --out <file.syms>

Joins rombuild's placement map (<build>/rom/romsect.bin.map: "<zname> <code run addr> <code size> <source image>") with
each image's sidecar (<source image>.sym, written by build_mmp: "<link ELF> <code link base>") and the ELF's function
symbols (arm-none-eabi-nm, $NM). Writes the format PhoneLab reads (lib/device/phlink/hotspots.go loadSyms):
"I <run addr> <size> <image>" per XIP image, then "F <run addr> <name>" per function inside it. An image without a
sidecar (the core images built by other emitters) gets its I line only. Exit 0 even when nothing could be mapped.
"""
import argparse
import os
import subprocess
import sys


def here(build, p):
    """A path written inside the build container: the same file under this build tree (the part after a build/)."""
    if not p or os.path.exists(p):
        return p
    q = p.replace("\\", "/")
    for cut in ("/build/", "/arm/"):
        if cut in q:
            r = os.path.join(build, q.rsplit(cut, 1)[1]) if cut == "/build/" else os.path.join(build, "arm", q.rsplit(cut, 1)[1])
            if os.path.exists(r):
                return r
    return p


def functions(nm, elf):
    try:
        out = subprocess.run([nm, "-n", "-C", elf], capture_output=True, text=True, check=False).stdout
    except OSError:
        return []
    res = []
    for ln in out.splitlines():
        f = ln.split(None, 2)
        if len(f) == 3 and f[1] in "tTwW":
            try:
                res.append((int(f[0], 16), f[2]))
            except ValueError:
                pass
    return res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--build", default=os.environ.get("PHOREPOC_BUILD", "build"))
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    nm = os.environ.get("NM") or next((t for t in ("arm-none-eabi-nm", "nm")
                                       if subprocess.run(["sh", "-c", "command -v " + t], capture_output=True).returncode == 0), "nm")
    rmap = os.path.join(a.build, "rom", "romsect.bin.map")
    if not os.path.exists(rmap):
        print("rom_syms: no %s: nothing to export" % rmap)
        return 0
    imgs = fns = 0
    with open(rmap) as fh, open(a.out, "w", newline="\n") as out:
        for ln in fh:
            g = ln.split(None, 3)
            if len(g) < 3:
                continue
            zname, run, size = g[0], int(g[1], 16), int(g[2], 16)
            src = g[3].strip() if len(g) > 3 else ""
            out.write("I %x %x %s\n" % (run, size, zname.replace("\\", "/").rsplit("/", 1)[-1]))
            imgs += 1
            src = here(a.build, src)
            side = src + ".sym"
            if not src or not os.path.exists(side):
                continue
            elf, base = open(side).read().split()[:2]
            base = int(base, 16)
            elf = here(a.build, elf)
            for addr, name in functions(nm, elf):
                if base <= addr < base + size:
                    out.write("F %x %s\n" % (addr - base + run, name))
                    fns += 1
    print("rom_syms: %d images, %d functions -> %s" % (imgs, fns, a.out))
    return 0


if __name__ == "__main__":
    sys.exit(main())
