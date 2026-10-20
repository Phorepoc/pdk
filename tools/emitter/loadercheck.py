# Copyright (c) 1996-2009 Nokia Corporation and/or its subsidiary(-ies): the 2010 source this ports (f32image.h)
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: EPL-1.0

"""loadercheck.py -- the REAL F32 loader's acceptance checks for a RAM-loaded E32Image.

A port of the 2010 source's `E32ImageHeaderV::ValidateHeader`, `ValidateExportDescription`,
`ValidateRelocations` and `ValidateImports` (userlibandfileserver/fileserver/inc/f32image.h), for the
uncompressed images we emit. ROM (XIP) images never meet
these checks (rombuild prelinks them). Everything a phone or a Phorepoc disk loads -- `device` (build_mmp.py) and
`sis` (build_mmp_x32.py) images -- must pass here, or the loader answers KErrCorrupt/KErrNotSupported and (for a
load by name) silently takes another copy.

Plus one rule the loader cannot see but the phone's unwinder needs: an ARM `device` image with exceptions carries
an exception descriptor (iExceptionDescriptor | 1) inside its code.
"""

from __future__ import annotations

import struct

from e32image import mem_crc32, uid_checksum, KIMAGE_CRC_INITIALISER

CPU_X86, CPU_ARMV4, CPU_ARMV5, CPU_ARMV6 = 0x1000, 0x2000, 0x2001, 0x2002
KERR_CORRUPT, KERR_NOT_SUPPORTED = "KErrCorrupt", "KErrNotSupported"


def _u32(d: bytes, o: int) -> int:
    return struct.unpack_from("<I", d, o)[0]


def check_loader(d: bytes, need_exception_descriptor: bool = False) -> list[str]:
    """[] if the real loader would accept `d`; else "<error>: <which check>" lines (the first is decisive)."""
    fails: list[str] = []

    def fail(err, why):
        fails.append(f"{err}: {why}")

    size = len(d)
    if size < 0x9C:
        return [f"{KERR_CORRUPT}: file smaller than the V header"]
    uid1, uid2, uid3, uidck, sig, hcrc, modver, comp = struct.unpack_from("<8I", d, 0)
    flags, codesz, datasz, heapmin, heapmax, stack, bss, entry = struct.unpack_from("<8I", d, 0x2C)
    codebase, database, dllref, expoff, expcount, textsz, codeoff, dataoff = struct.unpack_from("<8I", d, 0x4C)
    impoff, crel, drel = struct.unpack_from("<3I", d, 0x6C)
    cpu = struct.unpack_from("<H", d, 0x7A)[0]
    usize = _u32(d, 0x7C)
    exc = _u32(d, 0x90)
    edsize, edtype = struct.unpack_from("<HB", d, 0x98)

    if (flags >> 24) & 0xF != 2:
        fail(KERR_NOT_SUPPORTED, "header format is not V")
    if codeoff > size:
        fail(KERR_CORRUPT, "header larger than the file")
    is_arm = cpu in (CPU_ARMV4, CPU_ARMV5, CPU_ARMV6)
    if not is_arm and cpu != CPU_X86:
        fail(KERR_NOT_SUPPORTED, f"cpu {cpu:#x}")
    align = 3 if is_arm else 0
    if uidck != uid_checksum(uid1, uid2, uid3):
        fail(KERR_CORRUPT, "UID checksum")
    if sig != 0x434F5045:
        fail(KERR_CORRUPT, "signature")
    hdr = bytearray(d[:codeoff])
    struct.pack_into("<I", hdr, 0x14, KIMAGE_CRC_INITIALISER)
    if mem_crc32(bytes(hdr)) != hcrc:
        fail(KERR_CORRUPT, "header CRC")
    if modver >= 0x80000000 or (modver & 0xFFFF) > 0x8000:
        fail(KERR_NOT_SUPPORTED, f"module version {modver:#x}")
    total = size
    if comp:
        if comp not in (0x101F7AFC, 0x102822AA):
            fail(KERR_NOT_SUPPORTED, f"compression {comp:#x}")
        total = codeoff + usize
        return fails
    if flags & 1:
        if uid1 != 0x10000079:
            fail(KERR_NOT_SUPPORTED, "DLL flag with a non-DLL UID1")
    elif uid1 != 0x1000007A:
        fail(KERR_NOT_SUPPORTED, "EXE UID1")
    if is_arm:
        if (flags & 0xE0) != 0x20:
            fail(KERR_NOT_SUPPORTED, "ARM entry-point type is not EKA2")
        if (flags & 0x18) != 0x08:
            fail(KERR_NOT_SUPPORTED, "ARM ABI is not EABI")
    elif flags & 0xF8:
        fail(KERR_NOT_SUPPORTED, "ARM-only flag bits on a non-ARM image")
    if (flags >> 28) > 2:
        fail(KERR_NOT_SUPPORTED, "import format")
    if heapmax < heapmin:
        fail(KERR_CORRUPT, "heap max < min")
    if entry >= codesz or entry + 20 + 4 > codesz:
        fail(KERR_CORRUPT, "entry point outside the code")
    if entry & align or codebase & 3 or database & 3:
        fail(KERR_CORRUPT, "misaligned entry / code base / data base")
    if dllref and not impoff:
        fail(KERR_CORRUPT, "DLL references without an import section")
    ce = codeoff + codesz
    if ce > total:
        fail(KERR_CORRUPT, "code past the end of the file")
    if dataoff:
        if dataoff < ce or dataoff + datasz > total or (dataoff - codeoff) & align:
            fail(KERR_CORRUPT, "data section placement")
    elif datasz:
        fail(KERR_CORRUPT, "data size without data")
    if datasz + bss > 0x7FFF0000:
        fail("KErrNoMemory", "data + bss")
    if expcount > 65535:
        fail(KERR_CORRUPT, "too many exports")
    if expcount:
        es, ee = expoff, expoff + 4 * expcount
        if flags & 0x4:            # KImageNmdExpData: the 0th ordinal word precedes the table
            es -= 4
        if es < codeoff or ee > ce or (es - codeoff) & align:
            fail(KERR_CORRUPT, "export directory not inside the code section")
    if textsz > codesz:
        fail(KERR_CORRUPT, "text size > code size")
    for name, off, mn in (("import section", impoff, 4), ("code relocations", crel, 8), ("data relocations", drel, 8)):
        if off and (off < ce or off + mn > total or (off - ce) & align):
            fail(KERR_CORRUPT, f"{name} placement")
    if exc & 1 and exc >= codesz:
        fail(KERR_CORRUPT, "exception descriptor outside the code")
    ed = (edsize + 3 + 3) & ~3
    if 0x98 + ed != codeoff:
        fail(KERR_CORRUPT, "export description does not end at the code")
    if edtype not in (0, 1, 2):
        fail(KERR_NOT_SUPPORTED, f"export description type {edtype}")
    if edtype == 1 and (expcount + 7) >> 3 != edsize:
        fail(KERR_CORRUPT, "export bitmap size")
    if fails:
        return fails

    # ValidateRelocations (each section) and ValidateImports
    for name, off, secsize in (("code", crel, codesz), ("data", drel, datasz)):
        if not off:
            continue
        rsize, n = _u32(d, off), _u32(d, off + 4)
        if rsize & 3:
            fail(KERR_CORRUPT, f"{name} relocation size not a word multiple")
            continue
        p, end, count = off + 8, off + 8 + rsize, 0
        if end > total:
            fail(KERR_CORRUPT, f"{name} relocations overflow the file")
            continue
        while p != end:
            if p + 8 > end:
                fail(KERR_CORRUPT, f"{name} relocation block overruns")
                break
            page, bs = _u32(d, p), _u32(d, p + 4)
            if page & 0xFFF or bs < 8 or bs & 3 or p + bs > end:
                fail(KERR_CORRUPT, f"{name} relocation block header (page {page:#x}, size {bs})")
                break
            for q in range(p + 8, p + bs, 2):
                e = struct.unpack_from("<H", d, q)[0]
                if not e:
                    continue
                # 1/2/3 = text/data/inferred; 4 = Phorepoc's x32 %rip type (x86 only)
                if e >> 12 not in ((1, 2, 3) if is_arm else (1, 2, 3, 4)):
                    fail(KERR_CORRUPT, f"{name} relocation type {e >> 12}")
                    break
                o = page + (e & 0xFFF)
                if o + 4 > secsize or o & align:
                    fail(KERR_CORRUPT, f"{name} relocation {o:#x} outside the section")
                    break
                count += 1
            p += bs
        if not fails and count != n:
            fail(KERR_CORRUPT, f"{name} relocation count {count} != {n}")
    if impoff:
        isize = _u32(d, impoff)
        p, end = impoff + 4, impoff + isize
        if end > total or end < p:
            fail(KERR_CORRUPT, "import section size")
        fmt = flags >> 28
        for _ in range(dllref if not fails else 0):
            if p + 8 > end:
                fail(KERR_CORRUPT, "import block overruns")
                break
            nameoff, nimp = _u32(d, p), _u32(d, p + 4)
            p += 8
            if not impoff <= impoff + nameoff < end:
                fail(KERR_CORRUPT, "DLL name outside the import section")
                break
            if fmt == 2:           # PE2: no per-import list
                continue
            if p + 4 * nimp > end:
                fail(KERR_CORRUPT, "import list overruns")
                break
            if fmt == 1:
                for k in range(nimp):
                    v = _u32(d, p + 4 * k)
                    if v > codesz - 4 or v & align:
                        fail(KERR_CORRUPT, f"import slot {v:#x} outside the code")
                        break
            p += 4 * nimp

    if need_exception_descriptor and is_arm and not (exc & 1):
        fail("unwinder", "no exception descriptor: a Leave crossing this image cannot unwind (KERN-EXEC 3)")
    return fails
