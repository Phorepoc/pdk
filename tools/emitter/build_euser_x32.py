# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_euser_x32 -- euser.dll as a real x32 E32Image (amd64 CPU / x32 ABI).

euser is built as a separate module the way the platform actually ships it -- an E32Image DLL
presenting the REAL euser export table, so an EXE can bind to it by ordinal and a ROM can carry the
two as distinct code segments.

    python3 tools/emitter/build_euser_x32.py [out.dll]     (SKIP_CENSUS=1 reuses existing objects)
    default output: BUILD/x32core/euser_x32.dll (+ .pcrel sidecar)

Everything compiles and links on Linux (x32 multilib GCC); the emitter -- ELF -> E32Image, the ordinal
table, the self-checks -- runs in the same process.

  1. port/euser_census_x32.sh LINK=1 compiles the 86 euser TUs (unmodified baseline + the
     amd64 siblings in port/x64user/) and publishes its exact compile line in flags.txt.
     The port scripts expect a tree with baseline/, toolchain/, tools/ and emitter/ below one root
     (/repo) and write below /out; run_script() builds that view from paths.py (OS_ROOT, SYMBIAN, BUILD)
     and maps the two names, so the scripts run unchanged.
  2. port/x64user/xu_dll.cpp is compiled with those same flags: _E32Dll (the 16-byte entry
     header with UID3 at +12) + _E32Dll_Body (walks .init_array on process attach) +
     __unimpl_export.
  3. Pass-1 link (emitter/x32_dll.ld, --gc-sections, roots = every def symbol we define,
     unresolved tolerated) enumerates what the rooted closure still calls but we do not build;
     each such name becomes a `jmp __unimpl_export` stub so pass 2 links clean and any call
     fails loud (EUSRNIMP, reason = the caller's address) instead of running into stray bytes.
  4. The export table is eabi/euseru.def's 2546 frozen ordinals. Itanium mangling is identical
     on ARM EABI and x86-64 with ONE exception -- va_list: `St9__va_list` on ARM vs
     `P13__va_list_tag` on amd64 (SysV's va_list is an array type that decays to a pointer) --
     so four FormatList/AppendFormatList names go through X86_ALIAS. Ordinals past the table
     mirror the ARM device build (build_euser_device.EXTRA_EXPORTS) so devlink.ordinal_maps()
     resolves imports for both architectures unchanged.

Self-checks: code_base as linked, every export offset inside the code section and non-zero
(offset 0 is the entry point -- DEpocCodeSeg::Lookup reads "export == entry point" as an ABSENT
ordinal), every relocated word pointing inside the image, and the header (signature, UID checksum).
"""
from __future__ import annotations

import json
import os
import re
import struct
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import e32image                                    # noqa: E402
import elf                                         # noqa: E402
from euser_def import parse_def                    # noqa: E402
from paths import OS_ROOT, SYMBIAN, BUILD, resolve   # noqa: E402

CORE_OUT = os.path.join(BUILD, "x32core")           # default output dir of the five core-image builders
CENSUS = os.path.join(BUILD, "euser-census")
OUT = os.path.join(BUILD, "euser-x32")
DEF_PATH = os.path.join(resolve("baseline/kernelhwsrv"), "kernel", "eka", "eabi", "euseru.def")

CODE_BASE = 0x8000                  # x32_dll.ld
STUB_NAME = "__unimpl_export"
ENTRY_NAME = "_E32Dll"
# The two TUs every Path C module links ahead of its own objects: the entry point (target-type
# specific) and xu_image.cpp (__unimpl_export + the static-destructor ABI leaves, which EXEs
# need just as much -- see that file). An EXE builder passes its own entry object here.
ENTRY_OBJS = "xu_dll.o xu_image.o"
# What this DLL exports, for whoever binds to it (tools/rombuild.py's XIP prelinker): ordinal -> symbol/offset, plus the unimpl stub's
# offset so an importer can SAY which of its imports land on a not-yet-from-source ordinal.
EXPORT_MAP = os.path.join(BUILD, "euser-x32", "euser_exports.json")
# euser.mmh: `uid 0x1000008d 0x100039e5` (KSharedLibraryUidValue + euser's UID3). The ARM
# device build now carries the real UIDs too (Dll::Tls() keys on this word).
UID2, UID3 = 0x1000008D, 0x100039E5

# Spare ordinals past the real table, laid out exactly as the ARM device build does
# (build_euser_device.EXTRA_EXPORTS + extra_export_ordinal): max+1 is the bench-only
# euser_selftest slot -- absent here, so it points at the unimpl stub -- then TTrap::Trap and
# TTrap::UnTrap, which our -fno-exceptions TRAP macro needs and euseru.def never listed.
EXTRA_EXPORTS = ["_ZN5TTrap4TrapERi", "_ZN5TTrap6UnTrapEv"]

# LEAVE_TRACE=1: opt-in bring-up diagnostic (port/x64user/xu_leavetrace.cpp). The EXPORTED
# ordinals of User::Leave / User::LeaveIfError point at tracing wrappers, so every Leave raised
# by another image prints its code and call site. Off by default (byte-identical normal build).
LEAVE_TRACE = os.environ.get("LEAVE_TRACE") == "1"
LEAVE_WRAPPERS = {"_ZN4User5LeaveEi": "X32LeaveTrace_Leave",
                  "_ZN4User12LeaveIfErrorEi": "X32LeaveTrace_LeaveIfError",
                  # panics, via ld --wrap (so euser's OWN calls are traced too)
                  "_ZN4User5PanicERK7TDesC16i": "__wrap__ZN4User5PanicERK7TDesC16i"}
LEAVE_WRAP_LD = "--wrap=_ZN4User5PanicERK7TDesC16i " if LEAVE_TRACE else ""

# The ONE mangling difference between ARM EABI and amd64 Itanium in this table.
X86_ALIAS = ("St9__va_list", "P13__va_list_tag")


VIEW = os.path.join(BUILD, "x32view")
SUPCXX = "/usr/lib/gcc/x86_64-linux-gnu/13/x32/libsupc++.a"   # the x32 multilib (GCC 13), as build_mmp_x32.py


def make_view() -> str:
    """The tree the port scripts expect, as symlinks: VIEW/{baseline,toolchain,tools,board,emitter}.
    baseline -> SYMBIAN (kernelhwsrv/, symbian-2010/ ...), the rest from OS_ROOT / this directory."""
    os.makedirs(VIEW, exist_ok=True)
    for name, target in (("baseline", SYMBIAN), ("toolchain", os.path.join(OS_ROOT, "toolchain")),
                         ("tools", os.path.join(OS_ROOT, "tools")), ("board", os.path.join(OS_ROOT, "board")),
                         ("emitter", HERE)):
        link = os.path.join(VIEW, name)
        if os.path.islink(link) and os.readlink(link) == target:
            continue
        if os.path.lexists(link):
            os.remove(link)
        os.symlink(target, link)
    return VIEW


def map_paths(script: str) -> str:
    """The port scripts name their tree /repo and their output dir /out; point both at this checkout."""
    script = re.sub(r"(?<![\w./-])/repo(?![\w-])", VIEW, script)
    return re.sub(r"(?<![\w./-])/out(?![\w-])", BUILD, script)


def _env() -> dict:
    env = dict(os.environ)
    cf = os.path.join(BUILD, "casefix")
    if os.path.isdir(cf):       # last-resort include spellings (tools/casefix.py); -nostdinc leaves CPATH last
        env["CPATH"] = cf + (os.pathsep + env["CPATH"] if env.get("CPATH") else "")
    return env


def _docker(script: str, check: bool = True) -> subprocess.CompletedProcess:
    """Run a shell snippet with /repo and /out mapped (the historical name: there is no container here)."""
    make_view()
    return subprocess.run(["bash", "-c", map_paths(script)], check=check, env=_env(), capture_output=True, text=True)


def run_script(path: str, env_assign: str = "") -> subprocess.CompletedProcess:
    """Run a port script (path relative to OS_ROOT, or absolute) with /repo and /out mapped."""
    make_view()
    path = path if os.path.isabs(path) else os.path.join(OS_ROOT, path)
    text = map_paths(open(path, errors="replace").read())
    tmp = os.path.join(VIEW, "run-" + os.path.basename(path))
    with open(tmp, "w", newline="\n") as fh:
        fh.write(text)
    return subprocess.run(["bash", "-c", f"{env_assign} bash {tmp}"], env=_env(), capture_output=True, text=True)


class _Check:
    def __init__(self):
        self.ok = True
        self.msgs = []


def check_image(img: bytes) -> _Check:
    """Header sanity of an emitted image: 'EPOC' signature and the UID checksum (the loader refuses both)."""
    r = _Check()
    sig = struct.unpack_from("<I", img, 16)[0]
    uids = struct.unpack_from("<4I", img, 0)
    if sig != e32image.E32IMG_SIGNATURE:
        r.ok = False
        r.msgs.append(f"signature 0x{sig:08x}")
    if uids[3] != e32image.uid_checksum(*uids[:3]):
        r.ok = False
        r.msgs.append("UID checksum mismatch")
    return r


def format_result(r: _Check) -> str:
    return "header check: " + ("OK" if r.ok else "FAIL (" + "; ".join(r.msgs) + ")")


def x86_name(name: str) -> str:
    """The amd64 spelling of an EABI-mangled def name (va_list is the only delta)."""
    return name.replace(*X86_ALIAS)


# ---------------------------------------------------------------------------------------------
# container steps
# ---------------------------------------------------------------------------------------------

def compile_objects() -> None:
    """The 86 euser TUs + xu_dll.cpp, all with the census's own compile line."""
    if os.environ.get("SKIP_CENSUS") == "1" and os.path.isdir(os.path.join(CENSUS, "obj")):
        print("SKIP_CENSUS=1: reusing existing euser-census objects")
    else:
        # EXTRA_DEFS=-frtti: the census compile line says -fno-rtti and EXTRA_DEFS comes after it, so this turns RTTI back on
        # -- GCC then emits each class's typeinfo (_ZTI*, the 44 euseru.def exports) where its key function is defined
        r = run_script("toolchain/x32/euser_census_x32.sh", "LINK=1 EXTRA_DEFS=-frtti")
        line = [l for l in r.stdout.splitlines() if l.startswith("=== euser census")]
        print(line[0] if line else r.stdout[-400:])
        if not line or ", 0 FAIL" not in line[0]:
            sys.exit("euser census did not come out clean -- see " + CENSUS + "\n" + r.stdout[-1500:] + r.stderr[-800:])
    # -fcf-protection=none: GCC otherwise opens every function -- naked ones included -- with a
    # 4-byte endbr64, which would push _E32Dll's hand-encoded 16-byte header (and with it the
    # UID3 word Dll::Tls() reads at +12) four bytes along. Caught by check_entry_header().
    _docker("mkdir -p /out/euser-x32 && cd /repo && "
            "F=\"$(cat /out/euser-census/flags.txt)\" && "
            "$F -fcf-protection=none -c "
            "toolchain/x32/x64user/xu_dll.cpp -o /out/euser-x32/xu_dll.o && "
            "$F -c toolchain/x32/x64user/xu_image.cpp -o /out/euser-x32/xu_image.o"
            # the strcmp libsupc++'s type_info::operator== needs (euser has no libc)
            # + sized operator delete and __stack_chk_fail (the distro libsupc++ is
            # stack-protected) for the same members' deleting destructors
            + " && $F -c emitter/euser_rtti_rt.cpp -o /out/euser-x32/euser_rtti_rt.o"
            + " && $F -c emitter/euser_x32_rt.cpp -o /out/euser-x32/euser_x32_rt.o"
            + (" && $F -c toolchain/x32/x64user/xu_leavetrace.cpp"
               " -o /out/euser-x32/xu_leavetrace.o" if LEAVE_TRACE else "")
            # SCHED_TRACE=1 (diagnostic): the active scheduler with a dispatch log for BrowserNG's
            # main thread (toolchain/shims/euser_diag/ub_act.cpp), over the census's own object
            + (" && $F -I baseline/kernelhwsrv/kernel/eka/euser/cbase -c toolchain/shims/euser_diag/ub_act.cpp"
               " -o /out/euser-census/obj/cbase_ub_act_cpp.o" if os.environ.get("SCHED_TRACE") == "1" else ""))


# libsupc++ members euser's typeinfos reach: the three typeinfo class hierarchies' vtables + the std::type_info methods.
CXXABI_MEMBERS = ("class_type_info.o", "si_class_type_info.o", "vmi_class_type_info.o", "tinfo.o")
CXXABI_OBJS = ("/out/euser-x32/cxxabi/*.o /out/euser-x32/euser_rtti_rt.o "
              "/out/euser-x32/euser_x32_rt.o")


def extract_cxxabi_runtime() -> None:
    """The C++ ABI runtime the typeinfos need, linked statically (euser is the base DLL and cannot import one). The x32
    libsupc++.a supplies the __class_type_info / __si_class_type_info / __vmi_class_type_info vtables and the
    std::type_info methods. Their .eh_frame is discarded by x32_dll.ld, so no unwinder is pulled in."""
    _docker("rm -rf /out/euser-x32/cxxabi && mkdir -p /out/euser-x32/cxxabi && cd /out/euser-x32/cxxabi && "
            f"ar x {SUPCXX} " + " ".join(CXXABI_MEMBERS))


# The three helpers below take the build directory and object set as parameters so a SECOND
# x32 DLL can reuse them unchanged (build_efsrv_x32.py; the same relationship build_euser_device
# has with build_euser_dll on ARM). `work` is the /out subdirectory this DLL builds in, `objs`
# the container-side glob of its compiled TUs.

def defined_symbols(work: str, objs: str, entry_objs: str = ENTRY_OBJS) -> set[str]:
    r = _docker(f"nm --defined-only "
                + " ".join(f"/out/{work}/{o}" for o in entry_objs.split()) + f" {objs}")
    names = set()
    for line in r.stdout.splitlines():
        parts = line.split()
        if len(parts) == 3 and parts[1] in ("T", "W", "R", "D", "B", "V"):
            names.add(parts[2])
    return names


def link(roots: list[str], stub_obj: str | None, out_elf: str,
         work: str = "euser-x32", objs: str = "/out/euser-census/obj/*.o",
         entry_objs: str = ENTRY_OBJS, ld_script: str = "x32_dll.ld") -> None:
    """ld -q --gc-sections with an @file of -u roots. Pass 1 tolerates unresolved.
    `entry_objs` go first so the entry point owns code offset 0 (the .ld puts .text.entry
    first); `ld_script` is x32_exe.ld for an EXE."""
    with open(os.path.join(BUILD, work, "roots.rsp"), "w", newline="\n") as fh:
        fh.write("\n".join(f"-u {n}" for n in roots) + "\n")
    tolerate = "--unresolved-symbols=ignore-all " if stub_obj is None else ""
    wrap = LEAVE_WRAP_LD if work == "euser-x32" else ""
    _docker(f"cd /out/{work} && ld -m elf32_x86_64 -n -q --gc-sections --no-demangle "
            f"{tolerate}{wrap}-T /repo/emitter/{ld_script} @roots.rsp -Map {out_elf}.map "
            f"-o {out_elf} {entry_objs} {stub_obj or ''} {objs}")


def undefined_symbols(elf_name: str, work: str = "euser-x32") -> list[str]:
    r = _docker(f"nm --undefined-only /out/{work}/{elf_name}")
    return sorted({l.split()[-1] for l in r.stdout.splitlines() if l.split()})


def build_stubs(missing: list[str], work: str = "euser-x32") -> str:
    """One `jmp __unimpl_export` per unbuilt symbol, each in its own section so --gc-sections
    drops the unreferenced ones. A jmp (not a call) leaves the ORIGINAL caller's return address
    on the stack, which is what makes the panic reason name the real call site."""
    path = os.path.join(BUILD, work, "unimpl_stubs.s")
    with open(path, "w", newline="\n") as fh:
        for name in missing:
            fh.write(f'\t.section .text.unimpl.{name},"ax",@progbits\n'
                     f"\t.globl {name}\n\t.type {name},@function\n"
                     f"{name}:\n\tjmp {STUB_NAME}\n\t.size {name}, .-{name}\n")
        fh.write('\t.section .note.GNU-stack,"",@progbits\n')
    _docker(f"cd /out/{work} && gcc -mx32 -c unimpl_stubs.s -o unimpl_stubs.o")
    return "unimpl_stubs.o"


def check_relocs(ext) -> tuple[int, int]:
    """Audit the relocation set. Returns (unclassified, folded-addend).

    NOT "every relocated word holds an in-image address" -- that was the first formulation here
    and it is wrong. A relocation is `S + A`, and the compiler routinely folds a large negative
    addend into the stored word when it indexes an array off a base symbol (HAL's
    InitialiseData does exactly that: `HalInternal::Properties - 0x1020e306`). The word is then
    nowhere near the image, yet the relocation is both real and necessary, because the runtime
    delta is determined by where S lives.

    So the real invariant is the one elf.extract enforces: each reloc is classified TEXT or DATA
    by its TARGET SYMBOL's address, and a symbol in neither range raises there. This re-checks
    that every emitted reloc carries a classification, and counts the folded-addend ones so an
    unusual image says so out loud instead of looking like a failure.
    """
    code_lo, code_hi = ext.code_base, ext.code_base + len(ext.code)
    data_lo, data_hi = ext.data_base, ext.data_base + len(ext.data) + ext.bss_size
    unclassified = folded = 0
    for buf, offs, types in ((ext.code, ext.code_relocs, ext.code_reloc_types),
                             (ext.data, ext.data_relocs, ext.data_reloc_types)):
        for off in offs:
            if off not in types:
                unclassified += 1
                continue
            w = struct.unpack_from("<I", buf, off)[0]
            if not (code_lo <= w <= code_hi or data_lo <= w <= data_hi):
                folded += 1
    return unclassified, folded


def check_entry_header(code: bytes, entry_off: int, uid3: int = UID3) -> list[str]:
    """The entry point is a byte-layout contract, not just an address (uc_dll.cia): a jump over
    a 16-byte header, a zero word at +8 (the loader stores the RAM code-segment id there) and
    UID3 at +12, which is where Dll::Tls() looks. Anything the compiler slips in front of the
    hand-encoded bytes -- an endbr64, say -- silently shifts all three."""
    problems = []
    if code[entry_off:entry_off + 2] != b"\xeb\x0e":
        got = " ".join(f"{b:02x}" for b in code[entry_off:entry_off + 4])
        problems.append(f"entry does not start with the `jmp +16` header (got {got})")
    if struct.unpack_from("<I", code, entry_off + 8)[0] != 0:
        problems.append("entry+8 (code-segment id slot) is not zero")
    uid3_word = struct.unpack_from("<I", code, entry_off + 12)[0]
    if uid3_word != uid3:
        problems.append(f"entry+12 is 0x{uid3_word:08x}, expected this module's UID3 "
                        f"0x{uid3:08x}")
    return problems


# ---------------------------------------------------------------------------------------------

def main() -> int:
    os.makedirs(CORE_OUT, exist_ok=True)
    out_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(CORE_OUT, "euser_x32.dll")
    os.makedirs(OUT, exist_ok=True)
    compile_objects()
    extract_cxxabi_runtime()

    defmap = parse_def(DEF_PATH)
    entry_objs = ENTRY_OBJS + (" xu_leavetrace.o" if LEAVE_TRACE else "")
    defined = defined_symbols("euser-x32", "/out/euser-census/obj/*.o", entry_objs)
    # def ordinal -> the amd64 symbol that serves it (None = not built from source yet)
    served: dict[int, str] = {}
    aliased = 0
    for ordn, name in defmap.by_ordinal.items():
        sym = name if name in defined else x86_name(name)
        if sym in defined:
            served[ordn] = sym
            aliased += sym != name
    for name in EXTRA_EXPORTS:
        if name not in defined:
            sys.exit(f"EXTRA_EXPORTS symbol {name!r} is not defined by the x32 objects")

    roots = sorted(set(served.values()) | set(EXTRA_EXPORTS) | {STUB_NAME, ENTRY_NAME}
                   | (set(LEAVE_WRAPPERS.values()) if LEAVE_TRACE else set()))
    objs = "/out/euser-census/obj/*.o " + CXXABI_OBJS
    link(roots, None, "euser.pass1.elf", entry_objs=entry_objs, objs=objs)
    missing = undefined_symbols("euser.pass1.elf")
    stub_obj = build_stubs(missing)
    link(roots, stub_obj, "euser.elf", entry_objs=entry_objs, objs=objs)

    with open(os.path.join(OUT, "euser.elf"), "rb") as fh:
        ext = elf.extract(fh.read())
    if ext.code_base != CODE_BASE:
        sys.exit(f"code_base 0x{ext.code_base:X} != 0x{CODE_BASE:X}")
    for name in (STUB_NAME, ENTRY_NAME):
        if name not in ext.symbols:
            sys.exit(f"required symbol {name!r} was garbage-collected out of the link")
    entry_off = ext.symbols[ENTRY_NAME] - ext.code_base
    stub_off = ext.symbols[STUB_NAME] - ext.code_base

    # --- the export table: ordinal 1..max from the def, then the spare ordinals -------------
    export_offsets: list[int] = []
    real = 0
    for ordn in range(1, defmap.max_ordinal + 1):
        sym = served.get(ordn)
        if LEAVE_TRACE and sym in LEAVE_WRAPPERS:
            print(f"LEAVE_TRACE: ordinal {ordn} {sym} -> {LEAVE_WRAPPERS[sym]}")
            sym = LEAVE_WRAPPERS[sym]
        if sym and sym in ext.symbols:
            export_offsets.append(ext.symbols[sym] - ext.code_base)
            real += 1
        else:
            export_offsets.append(stub_off)         # unbuilt, or gc'd as unreachable
    export_offsets.append(stub_off)                 # max+1: the bench-only euser_selftest slot
    for name in EXTRA_EXPORTS:
        export_offsets.append(ext.symbols[name] - ext.code_base)

    served_all = dict(served)
    for i, name in enumerate(EXTRA_EXPORTS):
        served_all[defmap.max_ordinal + 2 + i] = name
    with open(EXPORT_MAP, "w", newline="\n") as fh:
        json.dump({"code_base": ext.code_base, "stub_offset": stub_off,
                   "entry_offset": entry_off, "count": len(export_offsets),
                   "uid3": UID3,
                   "ordinals": {str(o): [served_all.get(o), export_offsets[o - 1]]
                                for o in range(1, len(export_offsets) + 1)}}, fh, indent=1)

    print(f"euser.dll (x32): code {len(ext.code)} B @0x{ext.code_base:X}   "
          f"data {len(ext.data)} B   bss {ext.bss_size} B")
    print(f"exports: {len(export_offsets)} ordinals ({real}/{defmap.max_ordinal} real from "
          f"source, {defmap.max_ordinal - real} -> unimpl stub; {aliased} via the va_list "
          f"alias); {len(missing)} link stubs")
    print(f"relocs: {len(ext.code_relocs)} code, {len(ext.data_relocs)} data; "
          f"entry {ENTRY_NAME} +0x{entry_off:x}, {STUB_NAME} +0x{stub_off:x}")

    img = e32image.build_dll(
        ext.code, export_offsets, cpu=e32image.CPU_X86,
        uid2=UID2, uid3=UID3, entry_point=entry_off,
        code_relocs=ext.code_relocs, code_reloc_types=ext.code_reloc_types,
        code_base=ext.code_base, data=ext.data, data_base=ext.data_base,
        bss_size=ext.bss_size, data_relocs=ext.data_relocs,
        data_reloc_types=ext.data_reloc_types,
        cap1=0xFFFFFFFF, cap2=0xFFFFFFFF)
    with open(out_path, "wb") as fh:
        fh.write(img)
    e32image.write_pcrel_sidecar(out_path, ext)

    # --- self-checks -------------------------------------------------------------------------
    bad_reloc, folded = check_relocs(ext)
    bad_export = [i + 1 for i, off in enumerate(export_offsets)
                  if off == 0 or off >= len(ext.code)]
    entry_problems = check_entry_header(ext.code, entry_off)
    result = check_image(img)
    print(f"\nWrote {out_path} ({len(img)} bytes), cpu 0x{struct.unpack_from('<H', img, 122)[0]:04x}")
    print(f"self-check: {bad_reloc} unclassified relocs ({folded} with a folded addend), "
          f"{len(bad_export)} exports outside the code section or at the entry point, "
          f"entry header {'OK' if not entry_problems else 'BAD'}")
    for p in entry_problems:
        print("  ! " + p)
    print(format_result(result))
    ok = (bad_reloc == 0 and not bad_export and not entry_problems and result.ok
          and img[:4] == struct.pack("<I", e32image.UID1_DLL))
    print("X32 EUSER DLL OK" if ok else "X32 EUSER DLL FAIL")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
