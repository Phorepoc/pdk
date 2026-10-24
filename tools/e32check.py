#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
"""e32check.py -- host check of an E32 image against the loader's own validation rules.

Applies the checks of E32ImageHeaderV::ValidateHeader / ValidateExportDescription /
ValidateRelocations / ValidateImports from the file server's loader (f32image.h) to a
DLL or EXE, and reports every rule that fails (the loader stops at the first).
This is what the on-device installer and the RAM loader run; a ROM (XIP) image
skips it, which is why an image that boots from the ROM can still be refused from C:.

  e32check.py A.dll [B.dll ...]       exit status 1 if any image fails
"""
import struct
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "emitter"))
from e32image import mem_crc32, KIMAGE_CRC_INITIALISER  # noqa: E402

KImageDll = 1
KImageABIMask, KImageABI_EABI = 0x18, 0x08
KImageEptMask, KImageEpt_Eka2 = 0xE0, 0x20
KImageNmdExpData = 0x400
KImageHdrFmtMask, KImageHdrFmt_V = 0x0F000000, 0x02000000
KImageImpFmtMask, KImageImpFmt_PE2 = 0xF0000000, 0x20000000
KDynamicLibraryUid, KExecutableImageUid = 0x10000079, 0x1000007A
KCodeSegIdOffset = 12
BYTEPAIR, DEFLATE = 0x102822AA, 0x101F7AFC


def u32(d, o):
    return struct.unpack_from("<I", d, o)[0]


def s32(d, o):
    return struct.unpack_from("<i", d, o)[0]


def check(d: bytes):
    """Return a list of (rule, detail) failures."""
    bad = []

    def fail(rule, detail=""):
        bad.append((rule, detail))

    fsz = len(d)
    if fsz < 0x9C:
        return [("file smaller than the header", str(fsz))]
    flags = u32(d, 0x2C)
    if (flags & KImageHdrFmtMask) != KImageHdrFmt_V:
        return [("header format not V", hex(flags))]
    code_off, code_sz = u32(d, 0x64), u32(d, 0x30)
    hdr_sz = code_off
    if hdr_sz > fsz:
        fail("iCodeOffset beyond file", hex(hdr_sz))
    uid1, uid2, uid3 = u32(d, 0), u32(d, 4), u32(d, 8)
    if u32(d, 0x10) != 0x434F5045:
        fail("iSignature != 'EPOC'")
    hb = bytearray(d[:hdr_sz])
    struct.pack_into("<I", hb, 0x14, KIMAGE_CRC_INITIALISER)
    if mem_crc32(bytes(hb)) != u32(d, 0x14):
        fail("iHeaderCrc", "have %08x want %08x" % (u32(d, 0x14), mem_crc32(bytes(hb))))
    mv = u32(d, 0x18)
    if mv >= 0x80000000 or (mv & 0xFFFF) > 0x8000:
        fail("iModuleVersion (minor > 0x8000 or major >= 0x8000)", hex(mv))
    comp = u32(d, 0x1C)
    unc = fsz
    if comp:
        if comp not in (BYTEPAIR, DEFLATE):
            fail("unknown compression", hex(comp))
        unc = hdr_sz + u32(d, 0x7C)
    if unc > 0x0FFFFFFF:
        fail("uncompressed size > descriptor max")
    want_uid1 = KDynamicLibraryUid if flags & KImageDll else KExecutableImageUid
    if uid1 != want_uid1:
        fail("iUid1 vs KImageDll", "uid1=%08x" % uid1)
    if (flags & KImageEptMask) != KImageEpt_Eka2:
        fail("entry point type not EKA2", hex(flags))
    if (flags & KImageABIMask) != KImageABI_EABI:
        fail("ABI not EABI", hex(flags))
    if (flags & KImageImpFmtMask) > KImageImpFmt_PE2:
        fail("import format unknown", hex(flags))
    heap_min, heap_max, stack, bss = s32(d, 0x38), s32(d, 0x3C), s32(d, 0x40), s32(d, 0x44)
    if heap_min < 0:
        fail("iHeapSizeMin < 0")
    if heap_max < heap_min:
        fail("iHeapSizeMax < iHeapSizeMin")
    if stack < 0:
        fail("iStackSize < 0")
    if bss < 0:
        fail("iBssSize < 0")
    entry = u32(d, 0x48)
    if entry >= code_sz:
        fail("iEntryPoint >= iCodeSize", hex(entry))
    if entry + KCodeSegIdOffset + 4 > code_sz:
        fail("iEntryPoint+12+4 > iCodeSize", hex(entry))
    if entry & 3:
        fail("iEntryPoint unaligned")
    if u32(d, 0x4C) & 3:
        fail("iCodeBase unaligned")
    if u32(d, 0x50) & 3:
        fail("iDataBase unaligned")
    ndll, imp_off = s32(d, 0x54), u32(d, 0x6C)
    if ndll < 0:
        fail("iDllRefTableCount < 0")
    if ndll and not imp_off:
        fail("DLL refs but no import offset")
    code_end = code_off + code_sz
    if code_end > unc:
        fail("code part beyond file", "end %x > %x" % (code_end, unc))
    data_off, data_sz = u32(d, 0x68), u32(d, 0x34)
    data_end = data_off + data_sz
    if not data_off:
        if data_end:
            fail("iDataSize without iDataOffset")
    else:
        if data_off < code_end:
            fail("data overlaps code")
        if data_end > unc:
            fail("data part beyond file")
        if (data_off - code_off) & 3:
            fail("data misaligned vs code")
    if data_sz + bss > 0x7FFF0000:
        fail("data+bss too big (KErrNoMemory)")
    ecount, edir = u32(d, 0x5C), u32(d, 0x58)
    if ecount > 65535:
        fail("too many exports", str(ecount))
    if ecount:
        es = edir - (4 if flags & KImageNmdExpData else 0)
        ee = edir + ecount * 4
        if es < code_off or ee > code_end:
            fail("export dir outside code part", "%x..%x vs %x..%x" % (es, ee, code_off, code_end))
        if (es - code_off) & 3:
            fail("export dir unaligned")
    if u32(d, 0x60) > code_sz:
        fail("iTextSize > iCodeSize")
    for name, off, minsz in (("import", imp_off, 4), ("code reloc", u32(d, 0x70), 8),
                             ("data reloc", u32(d, 0x74), 8)):
        if off:
            if off < code_end:
                fail(name + " section inside code part")
            if off + minsz > unc:
                fail(name + " section beyond file")
            if (off - code_end) & 3:
                fail(name + " section unaligned")
    exd = u32(d, 0x90)
    if exd & 1 and exd >= code_sz:
        fail("exception descriptor outside code")
    # export description
    eds, edt = struct.unpack_from("<HB", d, 0x98)
    sz = (eds + 3 + 3) & ~3
    if 0x98 + sz != hdr_sz:
        fail("export description size vs header size", "0x98+%d != %d" % (sz, hdr_sz))
    bm = (ecount + 7) >> 3
    if edt == 1 and bm != eds:
        fail("full export bitmap size", "%d != %d" % (bm, eds))
    elif edt == 2:
        meta = (bm + 7) >> 3
        if meta > eds:
            fail("sparse bitmap does not fit")
        else:
            tot = meta + sum(bin(b).count("1") for b in d[0x9B:0x9B + meta])
            if tot != eds:
                fail("sparse bitmap byte count", "%d != %d" % (tot, eds))
    elif edt not in (0, 1, 2):
        fail("export description type", str(edt))
    # sections that need the uncompressed body
    if comp:
        return bad + [("note", "compressed (%s): relocation/import bodies not walked"
                       % ("bytepair" if comp == BYTEPAIR else "deflate"))]
    rest = d[code_end:]
    if imp_off and imp_off >= code_end and imp_off + 4 <= fsz:
        p0 = imp_off - code_end
        isz = u32(rest, p0)
        send = p0 + isz
        if send < p0 + 4 or send > len(rest):
            fail("import section size", "iSize %x, %x bytes available" % (isz, len(rest) - p0))
        else:
            p = p0 + 4
            fmt = flags & KImageImpFmtMask
            for _ in range(max(ndll, 0)):
                if p + 8 > send:
                    fail("import block header overflows section")
                    break
                noff, nimp = struct.unpack_from("<II", rest, p)
                p += 8
                if noff >= send or noff < 0:
                    fail("import dll name outside section", hex(noff))
                    break
                e = rest.find(bytes(1), p0 + noff)
                if e < 0 or e >= send:
                    fail("import dll name overflows section")
                if nimp >= 0x20000000:
                    fail("import count too large", hex(nimp))
                    break
                if fmt == KImageImpFmt_PE2:
                    continue
                if p + 4 * nimp > send:
                    fail("import list overflows section", "%d imports" % nimp)
                    break
                for i in range(nimp):
                    v = u32(rest, p + 4 * i)
                    if v > code_sz - 4 or v & 3:
                        fail("import slot outside code / unaligned", hex(v))
                        break
                p += 4 * nimp
    for name, off, size in (("code", u32(d, 0x70), code_sz), ("data", u32(d, 0x74), data_sz)):
        if not off or off < code_end or off + 8 > fsz:
            continue
        p = off - code_end
        ssz, nrel = struct.unpack_from("<II", rest, p)
        p += 8
        if ssz & 3:
            fail(name + " reloc iSize not word multiple")
        end = p + ssz
        if end > len(rest):
            fail(name + " reloc section overflows buffer", "iSize %x" % ssz)
            continue
        left = nrel
        while p != end:
            if p + 8 > end:
                fail(name + " reloc block header overflows section")
                break
            page, bsz = struct.unpack_from("<II", rest, p)
            if page & 0xFFF:
                fail(name + " reloc block not page aligned", hex(page))
            if bsz < 8 or bsz & 3 or p + bsz > end:
                fail(name + " reloc block size", hex(bsz))
                break
            for e in struct.unpack_from("<%dH" % ((bsz - 8) // 2), rest, p + 8):
                if not e:
                    continue
                t = e & 0xF000
                if t not in (0x1000, 0x2000, 0x3000):
                    fail(name + " reloc type", "%04x" % e)
                o = page + (e & 0xFFF)
                if o + 4 > size:
                    fail(name + " reloc outside its section", "off %x size %x" % (o, size))
                if o & 3:
                    fail(name + " reloc unaligned", hex(o))
                left -= 1
            p += bsz
        if left:
            fail(name + " reloc count mismatch", "header says %d, %d remain" % (nrel, left))
    return bad


def main(argv):
    rc = 0
    for f in argv:
        d = Path(f).read_bytes()
        res = check(d)
        errs = [r for r in res if r[0] != "note"]
        print("%s: %s" % (f, "OK" if not errs else "FAIL (%d)" % len(errs)))
        for r, det in res:
            print("   %s %s" % (r, det))
        rc |= bool(errs)
    return rc


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]) if len(sys.argv) > 1 else print(__doc__))
