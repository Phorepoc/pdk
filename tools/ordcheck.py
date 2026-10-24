#!/usr/bin/env python3
"""ordcheck.py -- do an app's imports by ordinal land on real code in a DLL?

  ordcheck.py APP.exe DLLNAME DLL.dll [FROZEN.def]
  ordcheck.py APP.exe --dir DIR            every import block against DIR/<name>.dll (version/uid tags in names dropped)

Reads the ELF-format import section of APP for the block whose name starts with DLLNAME (case-insensitive,
e.g. QtCore), takes each imported ordinal from the code word at the import slot, and looks it up in DLL's export
table. An ordinal past the table, or an entry that points at the DLL's unimplemented-export stub (from its
.meta sidecar), is reported; with FROZEN.def the symbol name of each bad ordinal is printed. Exit status 1 on any finding.
"""
import re
import struct
import sys


def u32(d, o):
    return struct.unpack_from("<I", d, o)[0]


def imports(d, want):
    code_off = u32(d, 0x64)
    imp = u32(d, 0x6C)
    ndll = u32(d, 0x54)
    p = imp + 4
    out = {}
    for _ in range(ndll):
        noff, n = struct.unpack_from("<II", d, p)
        p += 8
        name = d[imp + noff:d.index(bytes(1), imp + noff)].decode()
        slots = [u32(d, p + 4 * i) for i in range(n)]
        p += 4 * n
        if name.lower().startswith(want.lower()):
            out[name] = [(s, u32(d, code_off + s) & 0xFFFF) for s in slots]
    return out


def exports(d):
    off, cnt = u32(d, 0x58), u32(d, 0x5C)
    return [u32(d, off + 4 * i) for i in range(cnt)]


def def_names(path):
    names = {}
    for line in open(path, errors="replace"):
        m = re.match(r"\s*(\S+)\s+@\s*(\d+)\s", line)
        if m:
            names[int(m.group(2))] = m.group(1)
    return names


def stub_address(path, d):
    """The unimplemented-export stub of an image: its <image>.meta sidecar (stub=0xADDR), else None.
    (Several real exports may legitimately share one address, so 'most common address' is not a stub test.)"""
    import os
    m = path + ".meta"
    if os.path.isfile(m):
        t = open(m).read().strip()
        if t.startswith("stub="):
            return int(t[5:].split()[0], 16)
    return None


def main_dir(a):
    import os
    app = open(a[0], "rb").read()
    files = {f.lower(): os.path.join(a[2], f) for f in os.listdir(a[2])}
    bad = 0
    for blk, lst in imports(app, "").items():
        stem = blk.split("{")[0].split("[")[0].lower()
        stem = stem[:-4] if stem.endswith(".dll") else stem
        path = files.get(stem + ".dll") or files.get(stem + ".exe")
        if not path:
            print("%s: not in %s" % (blk, a[2]))
            continue
        ex = exports(open(path, "rb").read())
        stub = stub_address(path, None)
        ords = sorted({o for _s, o in lst})
        miss = [o for o in ords if o < 1 or o > len(ex)]
        stubs = [o for o in ords if stub is not None and 1 <= o <= len(ex) and ex[o - 1] == stub]
        if miss or stubs:
            print("%s: %d ordinals; beyond the %d exports: %s; on the shared stub: %s"
                  % (blk, len(ords), len(ex), miss[:30], stubs[:30]))
            bad += 1
    return 1 if bad else 0


def main(a):
    if len(a) > 1 and a[1] == "--dir":
        return main_dir(a)
    app, want, dll = open(a[0], "rb").read(), a[1], open(a[2], "rb").read()
    names = def_names(a[3]) if len(a) > 3 else {}
    ex = exports(dll)
    stub = stub_address(a[2], None)
    bad = 0
    for blk, lst in imports(app, want).items():
        ords = sorted({o for _s, o in lst})
        miss = [o for o in ords if o < 1 or o > len(ex)]
        stubs = [o for o in ords if 1 <= o <= len(ex) and ex[o - 1] == stub]
        print("%s: %d imports, %d distinct ordinals; %d beyond the export table, %d on the shared stub"
              % (blk, len(lst), len(ords), len(miss), len(stubs)))
        for tag, group in (("MISSING", miss), ("STUB", stubs)):
            for o in group[:40]:
                print("   %s %d %s" % (tag, o, names.get(o, "")))
        bad += len(miss) + len(stubs)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]) if len(sys.argv) > 2 else print(__doc__))
