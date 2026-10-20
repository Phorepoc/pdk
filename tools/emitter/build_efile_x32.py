# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_efile_x32 -- EFILE.EXE (the F32 file server) as an x32 E32Image.

The first x32 image that is not a DLL: 48/48 TUs compile unmodified (surface = euser 436 /
efsrv 39 / hal 1 / 7 static runtime bits); this links the census objects against the three
DLLs it needs (euser, efsrv, hal).

    python3 tools/emitter/build_efile_x32.py [out.exe]   (default BUILD/x32core/efile_x32.exe; SKIP_CENSUS=1 reuses objects)

Three things make it an EXE rather than a fourth pass through x32_client_dll.py:

  * TARGETTYPE exexp -- an EXE that ALSO exports. Its entry point is `_E32Startup`
    (port/x64user/xu_exe.cpp, the eexe stub) rather than `_E32Dll`, so the module is linked
    with emitter/x32_exe.ld and the byte-exact 16-byte entry header carries a ZERO word at +12
    (the loader's code-segment-id slot) where a DLL carries UID3. It still presents a frozen
    export table -- eabi/efileu.def, the CFileSystem/CMountCB/CProxyDrive plugin framework that
    a `.FSY` links against ("LIBRARY efile.lib") -- so `__unimpl_export` matters here exactly as
    it does for a DLL. That is why it lives in xu_image.cpp rather than xu_dll.cpp.
  * Imports spread over THREE DLLs, not one. Each undefined name is looked up in a combined
    euser/efsrv/hal ordinal map (euser wins overlaps, as the ARM side's devlink.ordinal_maps
    does), and devlink.import_blocks groups the veneers into one E32 import block per DLL --
    which is what gives the image a non-empty TDllRefTable once it is prelinked into a ROM.
  * Process fields from efile.mmp: UID 0x1000008c/0x100039e3, EPOCSTACKSIZE 0x6000,
    EPOCHEAPSIZE 0x1000..0x800000, VENDORID 0x70000001.

Self-checks: the same set x32_client_dll applies (code base as linked, every export offset
inside the code and non-zero, every reloc classified, every import word holding its ordinal,
the entry header's bytes, the image header check), plus -- because the providers are
now three separate modules -- every import is checked against the PROVIDING DLL's export
sidecar, so an import landing on that DLL's unimpl stub is named rather than merely counted.
"""
from __future__ import annotations

import json
import os
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import build_efsrv_x32 as EFSRV                     # noqa: E402  (its CFG owns the .def path)
import build_euser_x32 as EU                        # noqa: E402
import build_hal_x32 as HAL                         # noqa: E402
import devlink                                      # noqa: E402
import e32image                                     # noqa: E402
import elf                                          # noqa: E402
import x32_client_dll as CD                         # noqa: E402
from euser_def import parse_def                     # noqa: E402

FS = EFSRV.FS
DEF_PATH = os.path.join(FS, "eabi", "efileu.def")

WORK = "efile-x32"
CENSUS = os.path.join(EU.BUILD, "efile-census")
NAME = "efile"
ENTRY_NAME = "_E32Startup"
ENTRY_OBJS = "xu_exe.o xu_image.o"
EXPORT_MAP = os.path.join(EU.BUILD, WORK, "efile_exports.json")

# efile.mmh: UID 0x1000008c 0x100039e3 / EPOCSTACKSIZE / EPOCHEAPSIZE / VENDORID.
UID2, UID3 = 0x1000008C, 0x100039E3
STACK_SIZE, HEAP_MIN, HEAP_MAX = 0x6000, 0x1000, 0x800000
VENDOR_ID = 0x70000001

EUSER_DLL, EFSRV_DLL, HAL_DLL = "euser.dll", "efsrv.dll", "hal.dll"
DLL_ORDER = (EUSER_DLL, EFSRV_DLL, HAL_DLL)

# Compiled here on top of the 48 census objects:
#   xu_exe.cpp    the eexe entry stub (_E32Startup + X64U_RunThread -> E32Main)
#   xu_image.cpp  __unimpl_export + the static-destructor ABI leaves (__dso_handle,
#                 __cxa_atexit -- EFILE is the first x32 image with a file-scope object
#                 whose destructor g++ wants to register)
#   xu_cxxrt.cpp  sized delete / __cxa_pure_virtual, which no 2010 TU provides
#   gcchelp.cpp   REAL operator new/delete over User::Alloc -- euseru.def exports none of
#                 them, because on the real platform every image links scppnwdl.lib for its own
EXE_STUB = "tools/phorepoc-x86/port/x64user/xu_exe.cpp"


def ordinal_maps() -> dict[str, tuple[str, int]]:
    """symbol -> (dll, ordinal) over euser/efsrv/hal in the amd64 spelling. The x32 sibling of
    devlink.ordinal_maps(), which reads the ARM device build's def paths and drags its
    toolchain in with them; the resolution ORDER is the same -- euser wins an overlap."""
    resolve: dict[str, tuple[str, int]] = {}
    for dll, def_path in ((HAL_DLL, HAL.CFG.def_path), (EFSRV_DLL, EFSRV.CFG.def_path)):
        for ordn, name in parse_def(def_path).by_ordinal.items():
            resolve[EU.x86_name(name)] = (dll, ordn)
    for name, ordn in CD.euser_ordinal_map().items():
        resolve[name] = (EUSER_DLL, ordn)
    return resolve


def compile_objects() -> None:
    """The 48 EFILE TUs (efile_census_x32.sh) + the four TUs above, all on the census's own
    compile line -- which is what keeps this image's ABI identical to the objects it links."""
    if os.environ.get("SKIP_CENSUS") == "1" and os.path.isdir(os.path.join(CENSUS, "obj")):
        print("SKIP_CENSUS=1: reusing existing efile-census objects")
        if not os.path.exists(os.path.join(CENSUS, "flags.txt")):
            sys.exit("efile-census/flags.txt is missing -- re-run without SKIP_CENSUS=1 once "
                     "(older census runs did not publish it)")
    else:
        # EFILE_EXTRA_DEFS: extra -D's for the census, e.g. -DTRACE_CHECK_FAILURES = the loader
        # prints the file:line of every check that rejects an image (sf_ldr.h)
        extra = os.environ.get("EFILE_EXTRA_DEFS", "")
        r = EU.run_script("tools/phorepoc-x86/port/efile_census_x32.sh", f"EXTRA_DEFS='{extra}' LINK=1")
        line = [l for l in r.stdout.splitlines() if l.startswith("=== efile census")]
        print(line[0] if line else r.stdout[-400:])
        if not line or ", 0 FAIL" not in line[0]:
            sys.exit(f"efile census did not come out clean -- see {CENSUS}\n" + r.stdout[-1500:] + r.stderr[-800:])
    # -fcf-protection=none for the entry stub: an endbr64 would shift _E32Startup's hand-encoded
    # 16-byte header (see xu_exe.cpp). Asserted below by check_entry_header().
    EU._docker(
        f"mkdir -p /out/{WORK} && cd /repo && F=\"$(cat /out/{NAME}-census/flags.txt)\" && "
        f"$F -fcf-protection=none -c {EXE_STUB} -o /out/{WORK}/xu_exe.o && "
        f"$F -c {CD.IMAGE} -o /out/{WORK}/xu_image.o && "
        f"$F -c {CD.CXXRT} -o /out/{WORK}/xu_cxxrt.o && "
        f"$F -c {CD.GCCHELP} -o /out/{WORK}/gcchelp.o")


def check_import_providers(imports: dict[str, tuple[str, int]], ext) -> tuple[list[str], list[str]]:
    """Cross-check every emitted import against the PROVIDING DLL's export sidecar: the ordinal
    must be inside that module's table, and one served by its unimpl stub is named -- those are
    the calls that would panic EUSRNIMP at run time. Returns (problems, on-stub names)."""
    maps = {}
    for dll in DLL_ORDER:
        path = (EU.EXPORT_MAP if dll == EUSER_DLL
                else CD.export_map_path(dll[:-len(".dll")]))
        if not os.path.exists(path):
            return ([f"{dll}: no export sidecar at {path} -- build it first"], [])
        with open(path) as fh:
            maps[dll] = json.load(fh)
    problems, on_stub = [], []
    for name, (dll, ordn) in sorted(imports.items()):
        if name not in ext.symbols:
            continue                                # veneer collected by --gc-sections
        m = maps[dll]
        if not 1 <= ordn <= m["count"]:
            problems.append(f"{name}: ordinal {ordn} outside {dll}'s {m['count']} exports")
        elif m["ordinals"][str(ordn)][1] == m["stub_offset"]:
            on_stub.append(f"{name} @ {dll} {ordn}")
    return problems, on_stub


def main() -> int:
    out_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(EU.CORE_OUT, "efile_x32.exe")
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    out_dir = os.path.join(EU.BUILD, WORK)
    os.makedirs(out_dir, exist_ok=True)
    compile_objects()
    objs = " ".join([f"/out/{WORK}/xu_cxxrt.o", f"/out/{WORK}/gcchelp.o",
                     f"/out/{NAME}-census/obj/*.o"])

    defmap = parse_def(DEF_PATH)
    defined = EU.defined_symbols(WORK, objs, ENTRY_OBJS)
    served = {o: (n if n in defined else EU.x86_name(n)) for o, n in defmap.by_ordinal.items()}
    served = {o: n for o, n in served.items() if n in defined}
    roots = sorted(set(served.values()) | {EU.STUB_NAME, ENTRY_NAME})

    # Pass 1: what does the rooted closure still call that we do not define?
    EU.link(roots, None, f"{NAME}.pass1.elf", WORK, objs, ENTRY_OBJS, "x32_exe.ld")
    missing = EU.undefined_symbols(f"{NAME}.pass1.elf", WORK)
    resolve = ordinal_maps()
    imports = {n: resolve[n] for n in missing if n in resolve}
    unprovided = [n for n in missing if n not in resolve]

    # Pass 2: with one import veneer per resolved name, and a loud stub for anything else.
    veneer_s = devlink.make_veneers_x32(out_dir, imports.keys(), NAME)
    EU._docker(f"cd /out/{WORK} && gcc -mx32 -c {os.path.basename(veneer_s)} -o veneers.o")
    stub_obj = EU.build_stubs(unprovided, WORK) if unprovided else ""
    EU.link(roots, f"veneers.o {stub_obj}".strip(), f"{NAME}.elf", WORK, objs,
            ENTRY_OBJS, "x32_exe.ld")

    with open(os.path.join(out_dir, f"{NAME}.elf"), "rb") as fh:
        ext = elf.extract(fh.read())
    if ext.code_base != EU.CODE_BASE:
        sys.exit(f"code_base 0x{ext.code_base:X} != 0x{EU.CODE_BASE:X}")
    for name in (ENTRY_NAME, EU.STUB_NAME):
        if name not in ext.symbols:
            sys.exit(f"required symbol {name!r} was garbage-collected out of the link")
    entry_off = ext.symbols[ENTRY_NAME] - ext.code_base
    stub_off = ext.symbols[EU.STUB_NAME] - ext.code_base

    export_offsets, real = [], 0
    for ordn in range(1, defmap.max_ordinal + 1):
        sym = served.get(ordn)
        if sym and sym in ext.symbols:
            export_offsets.append(ext.symbols[sym] - ext.code_base)
            real += 1
        else:
            export_offsets.append(stub_off)
    with open(EXPORT_MAP, "w", newline="\n") as fh:
        json.dump({"code_base": ext.code_base, "stub_offset": stub_off,
                   "entry_offset": entry_off, "count": len(export_offsets), "uid3": UID3,
                   "ordinals": {str(o): [served.get(o), export_offsets[o - 1]]
                                for o in range(1, len(export_offsets) + 1)}}, fh, indent=1)

    blocks = devlink.import_blocks(ext, imports, dll_order=DLL_ORDER, arch="x32")
    bound = sum(len(e) for _d, e in blocks)
    print(f"EFILE.EXE (x32): code {len(ext.code)} B @0x{ext.code_base:X}   "
          f"data {len(ext.data)} B   bss {ext.bss_size} B")
    print(f"exports: {len(export_offsets)} ordinals ({real}/{defmap.max_ordinal} real from "
          f"source, {defmap.max_ordinal - real} -> unimpl stub)")
    print(f"imports: {bound} across {len(blocks)} DLLs  ("
          + ", ".join(f"{dll} {len(e)}" for dll, e in blocks)
          + f"); {len(imports) - bound} veneers gc'd, {len(unprovided)} names with no provider")
    for n in unprovided[:10]:
        print(f"  no provider: {n}")

    img = e32image.build_dll(
        ext.code, export_offsets, exe=True, cpu=e32image.CPU_X86,
        uid2=UID2, uid3=UID3, entry_point=entry_off, imports=blocks,
        stack_size=STACK_SIZE, heap_min=HEAP_MIN, heap_max=HEAP_MAX, vendor_id=VENDOR_ID,
        code_relocs=ext.code_relocs, code_reloc_types=ext.code_reloc_types,
        code_base=ext.code_base, data=ext.data, data_base=ext.data_base,
        bss_size=ext.bss_size, data_relocs=ext.data_relocs,
        data_reloc_types=ext.data_reloc_types, cap1=0xFFFFFFFF, cap2=0xFFFFFFFF)
    with open(out_path, "wb") as fh:
        fh.write(img)
    e32image.write_pcrel_sidecar(out_path, ext)

    # --- self-checks ---------------------------------------------------------------------
    bad_reloc, folded = EU.check_relocs(ext)
    bad_export = [i + 1 for i, off in enumerate(export_offsets)
                  if off == 0 or off >= len(ext.code)]
    # An EXE's entry header is uc_exe.cia's: `jmp +16` then zeros, the word at +12 being the
    # loader's code-segment-id slot rather than a DLL's UID3.
    entry_problems = EU.check_entry_header(ext.code, entry_off, uid3=0)
    code_offset = struct.unpack_from("<I", img, 100)[0]
    bad_import = 0
    for _dll, entries in blocks:
        for (off, ordn, adj) in entries:
            if struct.unpack_from("<I", img, code_offset + off)[0] != (ordn | (adj << 16)):
                bad_import += 1
    provider_problems, on_stub = check_import_providers(imports, ext)
    result = EU.check_image(img)
    print(f"\nWrote {out_path} ({len(img)} bytes), "
          f"cpu 0x{struct.unpack_from('<H', img, 122)[0]:04x}, "
          f"stack 0x{STACK_SIZE:X}, heap 0x{HEAP_MIN:X}..0x{HEAP_MAX:X}")
    print(f"self-check: {bad_reloc} unclassified relocs ({folded} with a folded addend), "
          f"{len(bad_export)} bad exports, {bad_import} bad import words, entry header "
          f"{'OK' if not entry_problems else 'BAD'}; {len(on_stub)} imports bind an ordinal "
          f"its provider serves with the unimpl stub")
    for s in on_stub[:12]:
        print(f"    on stub: {s}")
    for p in entry_problems + provider_problems:
        print("  ! " + p)
    print(EU.format_result(result))
    ok = (bad_reloc == 0 and not bad_export and not bad_import and not entry_problems
          and not provider_problems and not unprovided and result.ok and bound > 0
          and len(blocks) == len(DLL_ORDER)
          and img[:4] == struct.pack("<I", e32image.UID1_EXE))
    print("X32 EFILE EXE OK" if ok else "X32 EFILE EXE FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
