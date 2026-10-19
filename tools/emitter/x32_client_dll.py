"""x32_client_dll -- build an x32 E32Image DLL that sits ON TOP of euser.

euser.dll is special: nothing is underneath it, so `build_euser_x32.py` has no imports to
resolve. Every OTHER core x32 DLL has the same shape as the next one -- compile a census, link
against euser by ordinal, present its own frozen `.def` export table -- so that shape lives here
once and `build_efsrv_x32.py` / `build_hal_x32.py` are a dozen lines of configuration each.

What "sits on top of euser" means concretely, and why each piece is needed:
  * Unresolved symbols are NOT all unimplemented. Each is looked up in euser's ordinal map
    first: a hit becomes an import veneer (`jmp *0x2(%rip)` + a slot holding `ordinal | adj<<16`,
    devlink.make_veneers_x32) plus an E32 import-block entry that the loader -- or the XIP
    prelinker in tools/rombuild.py -- overwrites with the resolved export address. Only a name
    with no provider anywhere falls to the unimpl stub.
  * `xu_cxxrt.cpp` supplies the modern-g++ runtime leaves (sized delete, __cxa_pure_virtual) and
    `common/gcchelp.cpp` the image's own operator new/delete: euseru.def exports none of them,
    because on the real platform every image statically links scppnwdl.lib for its own copy.
  * `xu_dll.cpp` is compiled with -DDLL_UID3=<this module's UID3>, so the word at _E32Dll+12 --
    where Dll::Tls() looks -- names THIS module. `xu_image.cpp` beside it carries the loud
    `__unimpl_export` and the static-destructor ABI leaves (see that file).

Each build writes an export sidecar (`<name>_exports.json`, same shape as euser's) so whatever
links ON TOP of this DLL can say which of its imports land on an ordinal we do not build yet.

Self-checks: code base as linked; every export offset inside the code and non-zero (offset 0 is
the entry point, which DEpocCodeSeg::Lookup reads as an ABSENT ordinal); every relocated word
inside the image; every import word holding its ordinal; the entry header's byte layout carrying
this module's UID3; and the image header check (signature, UID checksum). Imports that bind a euser ordinal euser
itself serves with the unimpl stub are counted and reported -- they would panic EUSRNIMP if called.
"""
from __future__ import annotations

import json
import os
import struct
import sys
from dataclasses import dataclass, field

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import build_euser_x32 as EU                    # noqa: E402  (shell helpers + the alias rule)
import devlink                                  # noqa: E402
import e32image                                 # noqa: E402
import elf                                      # noqa: E402
from euser_def import parse_def                 # noqa: E402

GCCHELP = "baseline/kernelhwsrv/kernel/eka/common/gcchelp.cpp"
CXXRT = "tools/phorepoc-x86/port/x64user/xu_cxxrt.cpp"
IMAGE = "tools/phorepoc-x86/port/x64user/xu_image.cpp"
EUSER_DLL = "euser.dll"


@dataclass
class Cfg:
    name: str               # "efsrv" -- names the build dir, census script and banner
    def_path: str           # the frozen export table (eabi/<name>u.def)
    uid2: int
    uid3: int
    census_marker: str      # the census script's summary line, e.g. "=== efsrv census"
    extra_objs: list = field(default_factory=list)   # /out-relative paths, beyond the census objects

    @property
    def work(self) -> str:
        return f"{self.name}-x32"

    @property
    def census_dir(self) -> str:
        return os.path.join(EU.BUILD, f"{self.name}-census")


def export_map_path(name: str) -> str:
    """Where `<name>.dll`'s export sidecar lives -- the euser one's sibling (EU.EXPORT_MAP)."""
    return os.path.join(EU.BUILD, f"{name}-x32", f"{name}_exports.json")


def euser_ordinal_map() -> dict[str, int]:
    """symbol -> euser ordinal in the amd64 spelling: the frozen table plus the spare ordinals
    past it, laid out exactly as the ARM device build lays them (one rule for both arches)."""
    eu = parse_def(EU.DEF_PATH)
    m = {EU.x86_name(n): o for o, n in eu.by_ordinal.items()}
    for i, n in enumerate(EU.EXTRA_EXPORTS):
        m[n] = eu.max_ordinal + 2 + i
    return m


def compile_objects(cfg: Cfg) -> None:
    if os.environ.get("SKIP_CENSUS") == "1" and os.path.isdir(os.path.join(cfg.census_dir, "obj")):
        print(f"SKIP_CENSUS=1: reusing existing {cfg.name}-census objects")
    else:
        r = EU.run_script(f"tools/phorepoc-x86/port/{cfg.name}_census_x32.sh", "LINK=1")
        line = [l for l in r.stdout.splitlines() if l.startswith(cfg.census_marker)]
        print(line[0] if line else r.stdout[-400:])
        if not line or ", 0 FAIL" not in line[0]:
            sys.exit(f"{cfg.name} census did not come out clean -- see {cfg.census_dir}\n" + r.stdout[-1500:] + r.stderr[-800:])
    # -fcf-protection=none: an endbr64 would shift _E32Dll's byte-exact entry header (see
    # build_euser_x32). -DDLL_UID3: the word at +12 must name THIS module.
    EU._docker(
        f"mkdir -p /out/{cfg.work} && cd /repo && F=\"$(cat /out/{cfg.name}-census/flags.txt)\" && "
        f"$F -fcf-protection=none -DDLL_UID3={cfg.uid3:#x} -c "
        f"tools/phorepoc-x86/port/x64user/xu_dll.cpp -o /out/{cfg.work}/xu_dll.o && "
        f"$F -c {IMAGE} -o /out/{cfg.work}/xu_image.o && "
        f"$F -c {CXXRT} -o /out/{cfg.work}/xu_cxxrt.o && "
        f"$F -c {GCCHELP} -o /out/{cfg.work}/gcchelp.o")


def build(cfg: Cfg, out_path: str) -> int:
    out_dir = os.path.join(EU.BUILD, cfg.work)
    os.makedirs(out_dir, exist_ok=True)
    compile_objects(cfg)
    objs = " ".join([f"/out/{cfg.work}/xu_cxxrt.o", f"/out/{cfg.work}/gcchelp.o",
                     *cfg.extra_objs, f"/out/{cfg.name}-census/obj/*.o"])

    defmap = parse_def(cfg.def_path)
    defined = EU.defined_symbols(cfg.work, objs)
    served = {o: (n if n in defined else EU.x86_name(n)) for o, n in defmap.by_ordinal.items()}
    served = {o: n for o, n in served.items() if n in defined}
    roots = sorted(set(served.values()) | {EU.STUB_NAME, EU.ENTRY_NAME})

    EU.link(roots, None, f"{cfg.name}.pass1.elf", cfg.work, objs)
    missing = EU.undefined_symbols(f"{cfg.name}.pass1.elf", cfg.work)
    euser_ords = euser_ordinal_map()
    imports = {n: euser_ords[n] for n in missing if n in euser_ords}
    unprovided = [n for n in missing if n not in euser_ords]

    veneer_s = devlink.make_veneers_x32(out_dir, imports.keys(), cfg.name)
    EU._docker(f"cd /out/{cfg.work} && gcc -mx32 -c {os.path.basename(veneer_s)} -o veneers.o")
    stub_obj = EU.build_stubs(unprovided, cfg.work) if unprovided else ""
    EU.link(roots, f"veneers.o {stub_obj}".strip(), f"{cfg.name}.elf", cfg.work, objs)

    with open(os.path.join(out_dir, f"{cfg.name}.elf"), "rb") as fh:
        ext = elf.extract(fh.read())
    if ext.code_base != EU.CODE_BASE:
        sys.exit(f"code_base 0x{ext.code_base:X} != 0x{EU.CODE_BASE:X}")
    entry_off = ext.symbols[EU.ENTRY_NAME] - ext.code_base
    stub_off = ext.symbols[EU.STUB_NAME] - ext.code_base

    export_offsets, real = [], 0
    for ordn in range(1, defmap.max_ordinal + 1):
        sym = served.get(ordn)
        if sym and sym in ext.symbols:
            export_offsets.append(ext.symbols[sym] - ext.code_base)
            real += 1
        else:
            export_offsets.append(stub_off)

    with open(export_map_path(cfg.name), "w", newline="\n") as fh:
        json.dump({"code_base": ext.code_base, "stub_offset": stub_off,
                   "entry_offset": entry_off, "count": len(export_offsets), "uid3": cfg.uid3,
                   "ordinals": {str(o): [served.get(o), export_offsets[o - 1]]
                                for o in range(1, len(export_offsets) + 1)}}, fh, indent=1)

    blocks = devlink.import_blocks(ext, {n: (EUSER_DLL, o) for n, o in imports.items()},
                                   dll_order=(EUSER_DLL,), arch="x32")
    bound = sum(len(e) for _d, e in blocks)
    print(f"{cfg.name}.dll (x32): code {len(ext.code)} B @0x{ext.code_base:X}   "
          f"data {len(ext.data)} B   bss {ext.bss_size} B")
    print(f"exports: {len(export_offsets)} ordinals ({real}/{defmap.max_ordinal} real from "
          f"source, {defmap.max_ordinal - real} -> unimpl stub)")
    print(f"imports: {bound} bound to {EUSER_DLL} by ordinal ({len(imports)} resolved, "
          f"{len(imports) - bound} veneers gc'd); {len(unprovided)} names with no provider")
    for n in unprovided[:10]:
        print(f"  no provider: {n}")

    img = e32image.build_dll(
        ext.code, export_offsets, cpu=e32image.CPU_X86,
        uid2=cfg.uid2, uid3=cfg.uid3, entry_point=entry_off, imports=blocks,
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
    entry_problems = EU.check_entry_header(ext.code, entry_off, cfg.uid3)
    code_offset = struct.unpack_from("<I", img, 100)[0]
    with open(EU.EXPORT_MAP) as fh:
        emap = json.load(fh)
    bad_import, on_stub = 0, []
    for _dll, entries in blocks:
        for (off, ordn, adj) in entries:
            if struct.unpack_from("<I", img, code_offset + off)[0] != (ordn | (adj << 16)):
                bad_import += 1
            elif emap["ordinals"][str(ordn)][1] == emap["stub_offset"]:
                on_stub.append(ordn)
    result = EU.check_image(img)
    print(f"\nWrote {out_path} ({len(img)} bytes), "
          f"cpu 0x{struct.unpack_from('<H', img, 122)[0]:04x}")
    print(f"self-check: {bad_reloc} unclassified relocs ({folded} with a folded addend), "
          f"{len(bad_export)} bad exports, {bad_import} bad import words, entry header "
          f"{'OK' if not entry_problems else 'BAD'}; {len(on_stub)} imports bind a euser "
          f"ordinal served by the unimpl stub")
    for p in entry_problems:
        print("  ! " + p)
    print(EU.format_result(result))
    ok = (bad_reloc == 0 and not bad_export and not bad_import and not entry_problems
          and not unprovided and result.ok and bound > 0
          and img[:4] == struct.pack("<I", e32image.UID1_DLL))
    print(f"X32 {cfg.name.upper()} DLL OK" if ok else f"X32 {cfg.name.upper()} DLL FAIL")
    return 0 if ok else 1
