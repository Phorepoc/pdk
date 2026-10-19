"""build_estart_x32 -- ESTART (e32strt.exe, the base startup process) as an x32 E32Image.

The file server's startup ends in `RProcess::Create(Z:\\Sys\\Bin\\ESTART.EXE)`. This is the real
image: estart.cpp +
estartmain.cpp, unmodified, compiled by port/estart_census_x32.sh and linked over the same three
DLLs as EFILE.

    python3 tools/emitter/build_estart_x32.py [out.exe]  (default BUILD/x32core/estart_x32.exe; SKIP_CENSUS=1 reuses objects)

It is build_efile_x32.py with two differences:

  * TARGETTYPE exe, not exexp -- ESTART exports nothing, so there is no .def, no export table
    and no export sidecar. The roots are just the entry point.
  * Process fields from estart.mmp: UID 0/0x10272C04, VENDORID 0x70000001, and NO
    EPOCSTACKSIZE/EPOCHEAPSIZE, so the makmake defaults (stack 0x2000, heap 0x1000..0x100000).

Like EFILE it has writable static data (census: data 4, bss 12), which is what the .pcrel sidecar
carries for the ROM prelinker. Same self-checks as EFILE minus the export ones.
"""
from __future__ import annotations

import os
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import build_efile_x32 as EFILE                     # noqa: E402  (ordinal_maps, provider check)
import build_euser_x32 as EU                        # noqa: E402
import devlink                                      # noqa: E402
import e32image                                     # noqa: E402
import elf                                          # noqa: E402
import x32_client_dll as CD                         # noqa: E402

WORK = "estart-x32"
CENSUS = os.path.join(EU.BUILD, "estart-census")
NAME = "estart"
ENTRY_NAME = "_E32Startup"
ENTRY_OBJS = "xu_exe.o xu_image.o"

# estart.mmp: `UID 0 0x10272C04`, VENDORID 0x70000001; stack/heap are the makmake defaults.
UID2, UID3 = 0, 0x10272C04
STACK_SIZE, HEAP_MIN, HEAP_MAX = 0x2000, 0x1000, 0x100000
VENDOR_ID = 0x70000001


def compile_objects() -> None:
    if os.environ.get("SKIP_CENSUS") == "1" and os.path.isdir(os.path.join(CENSUS, "obj")):
        print("SKIP_CENSUS=1: reusing existing estart-census objects")
    else:
        r = EU.run_script("tools/phorepoc-x86/port/estart_census_x32.sh", "LINK=1")
        line = [l for l in r.stdout.splitlines() if l.startswith("=== estart census")]
        print(line[0] if line else r.stdout[-400:])
        if not line or ", 0 FAIL" not in line[0]:
            sys.exit(f"estart census did not come out clean -- see {CENSUS}\n" + r.stdout[-1500:] + r.stderr[-800:])
    # -fcf-protection=none for the entry stub, as in build_efile_x32.py.
    EU._docker(
        f"mkdir -p /out/{WORK} && cd /repo && F=\"$(cat /out/{NAME}-census/flags.txt)\" && "
        f"$F -fcf-protection=none -c {EFILE.EXE_STUB} -o /out/{WORK}/xu_exe.o && "
        f"$F -c {CD.IMAGE} -o /out/{WORK}/xu_image.o && "
        f"$F -c {CD.CXXRT} -o /out/{WORK}/xu_cxxrt.o && "
        f"$F -c {CD.GCCHELP} -o /out/{WORK}/gcchelp.o")


def main() -> int:
    out_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(EU.CORE_OUT, "estart_x32.exe")
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    out_dir = os.path.join(EU.BUILD, WORK)
    os.makedirs(out_dir, exist_ok=True)
    compile_objects()
    objs = " ".join([f"/out/{WORK}/xu_cxxrt.o", f"/out/{WORK}/gcchelp.o",
                     f"/out/{NAME}-census/obj/*.o"])
    roots = [ENTRY_NAME]

    EU.link(roots, None, f"{NAME}.pass1.elf", WORK, objs, ENTRY_OBJS, "x32_exe.ld")
    missing = EU.undefined_symbols(f"{NAME}.pass1.elf", WORK)
    resolve = EFILE.ordinal_maps()
    imports = {n: resolve[n] for n in missing if n in resolve}
    unprovided = [n for n in missing if n not in resolve]

    veneer_s = devlink.make_veneers_x32(out_dir, imports.keys(), NAME)
    EU._docker(f"cd /out/{WORK} && gcc -mx32 -c {os.path.basename(veneer_s)} -o veneers.o")
    stub_obj = EU.build_stubs(unprovided, WORK) if unprovided else ""
    EU.link(roots, f"veneers.o {stub_obj}".strip(), f"{NAME}.elf", WORK, objs,
            ENTRY_OBJS, "x32_exe.ld")

    with open(os.path.join(out_dir, f"{NAME}.elf"), "rb") as fh:
        ext = elf.extract(fh.read())
    if ext.code_base != EU.CODE_BASE:
        sys.exit(f"code_base 0x{ext.code_base:X} != 0x{EU.CODE_BASE:X}")
    entry_off = ext.symbols[ENTRY_NAME] - ext.code_base

    blocks = devlink.import_blocks(ext, imports, dll_order=EFILE.DLL_ORDER, arch="x32")
    blocks = [(d, e) for d, e in blocks if e]
    bound = sum(len(e) for _d, e in blocks)
    print(f"ESTART.EXE (x32): code {len(ext.code)} B @0x{ext.code_base:X}   "
          f"data {len(ext.data)} B   bss {ext.bss_size} B")
    print(f"imports: {bound} across {len(blocks)} DLLs  ("
          + ", ".join(f"{dll} {len(e)}" for dll, e in blocks)
          + f"); {len(imports) - bound} veneers gc'd, {len(unprovided)} names with no provider")
    for n in unprovided[:10]:
        print(f"  no provider: {n}")

    img = e32image.build_dll(
        ext.code, [], exe=True, cpu=e32image.CPU_X86,
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
    entry_problems = EU.check_entry_header(ext.code, entry_off, uid3=0)
    code_offset = struct.unpack_from("<I", img, 100)[0]
    bad_import = 0
    for _dll, entries in blocks:
        for (off, ordn, adj) in entries:
            if struct.unpack_from("<I", img, code_offset + off)[0] != (ordn | (adj << 16)):
                bad_import += 1
    provider_problems, on_stub = EFILE.check_import_providers(imports, ext)
    result = EU.check_image(img)
    print(f"\nWrote {out_path} ({len(img)} bytes), "
          f"cpu 0x{struct.unpack_from('<H', img, 122)[0]:04x}, "
          f"stack 0x{STACK_SIZE:X}, heap 0x{HEAP_MIN:X}..0x{HEAP_MAX:X}")
    print(f"self-check: {bad_reloc} unclassified relocs ({folded} with a folded addend), "
          f"{bad_import} bad import words, entry header "
          f"{'OK' if not entry_problems else 'BAD'}; {len(on_stub)} imports bind an ordinal "
          f"its provider serves with the unimpl stub")
    for s in on_stub[:20]:
        print(f"    on stub: {s}")
    for p in entry_problems + provider_problems:
        print("  ! " + p)
    print(EU.format_result(result))
    ok = (bad_reloc == 0 and not bad_import and not entry_problems
          and not provider_problems and not unprovided and result.ok and bound > 0
          and img[:4] == struct.pack("<I", e32image.UID1_EXE))
    print("X32 ESTART EXE OK" if ok else "X32 ESTART EXE FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
