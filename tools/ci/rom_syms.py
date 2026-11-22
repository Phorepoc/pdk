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
import glob
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


def kernel_elf(build):
    """The ekern.elf link_ekern.sh wrote (.ekern-build/, .ekern-flex-build/, .ekernsmp-flex-build/, here or one level down):
    the newest one, as the ROM was built from the last link."""
    c = []
    for root in (".", build):
        for pat in (".ekern*-build/ekern.elf", "*/.ekern*-build/ekern.elf", "**/ekern.elf"):
            c += glob.glob(os.path.join(root, pat), recursive=True)
    c = [f for f in dict.fromkeys(c) if os.path.isfile(f)]
    if not c:
        print("rom_syms: no ekern.elf found: the kernel gets its I line only")
        return None
    f = max(c, key=os.path.getmtime)
    print("rom_syms: kernel symbols from %s" % f)
    return f


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--build", default=os.environ.get("PHOREPOC_BUILD", "build"))
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    nm = os.environ.get("NM") or next((t for t in ("arm-none-eabi-nm", "nm")
                                       if subprocess.run(["sh", "-c", "command -v " + t], capture_output=True).returncode == 0), "nm")
    # the maps rombuild writes: the section ROM's (<build>/rom/romsect.bin.map: "<zname> <run> <size> <source image>") and the
    # whole kernel ROM's (.bootstrap-build/ekern.rom.map, the ARM boards: "<name> <run> <size> <link base>")
    maps = [os.path.join(a.build, "rom", "romsect.bin.map")]
    maps += sorted(glob.glob(".bootstrap-build/ekern.rom.map") + glob.glob("*/.bootstrap-build/ekern.rom.map")
                   + glob.glob(os.path.join(a.build, "**", "ekern.rom.map"), recursive=True)
                   # the board recipe's copy of the whole-ROM map (adaptation-pinephone recipes/image.sh: <build>/<board>/symbian.img.map)
                   + glob.glob(os.path.join(a.build, "*", "symbian.img.map")))
    maps = [m for m in dict.fromkeys(maps) if os.path.exists(m)]
    if not maps:
        print("rom_syms: no ROM map (romsect.bin.map / ekern.rom.map): nothing to export")
        return 0
    # every image's sidecar under the build tree, by image name (the whole-ROM map names images, not their files)
    sides = {}
    for f in glob.glob(os.path.join(a.build, "**", "*.sym"), recursive=True):
        sides.setdefault(os.path.basename(f)[:-4].lower(), f)
    imgs = fns = 0
    seen = set()
    with open(a.out, "w", newline="\n") as out:
        for rmap in maps:
            print("rom_syms: map %s" % rmap)
            for ln in open(rmap):
                g = ln.split(None, 3)
                if len(g) < 3:
                    continue
                zname, run, size = g[0], int(g[1], 16), int(g[2], 16)
                name = zname.replace("\\", "/").rsplit("/", 1)[-1]
                if (run, name) in seen:
                    continue
                seen.add((run, name))
                out.write("I %x %x %s\n" % (run, size, name))
                imgs += 1
                src = g[3].strip() if len(g) > 3 else ""
                side = here(a.build, src) + ".sym" if src and not src.startswith("0x") else ""
                if not side or not os.path.exists(side):
                    side = sides.get(name.lower()) or sides.get(name.lower().replace(".dll", "_x32.dll").replace(".exe", "_x32.exe"))
                if not side:
                    continue
                elf, base = open(side).read().split()[:2]
                base = int(base, 16)
                elf = here(a.build, elf)
                for addr, fn in functions(nm, elf):
                    if base <= addr < base + size:
                        out.write("F %x %s\n" % (addr - base + run, fn))
                        fns += 1
        # the kernel: the ROM primary (rombuild --primary), in no placement map; link_ekern.sh links ekern.elf at its run
        # address, so its symbols go out as they are (faults in ekern, e.g. boot 2992's undef at 0x8008ee50, had no name)
        kelf = None if any(n.lower() == "ekern.exe" for _, n in seen) else kernel_elf(a.build)
        kf = functions(nm, kelf) if kelf else []
        if kf:
            lo, hi = kf[0][0], kf[-1][0] + 4
            out.write("I %x %x ekern.exe\n" % (lo, hi - lo))
            imgs += 1
            for addr, fn in kf:
                out.write("F %x %s\n" % (addr, fn))
                fns += 1
    print("rom_syms: %d images, %d functions -> %s" % (imgs, fns, a.out))
    return 0


if __name__ == "__main__":
    sys.exit(main())
