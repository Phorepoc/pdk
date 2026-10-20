# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_mmp_x32 -- build a registered component from its REAL 2010 .mmp for the amd64 / x32 port.

The x32 sibling of build_mmp.py (the ARM builder). Same registry (components.py), same .mmp parser
(mmp.py -- parsed with the SAME platform macros as the ARM build, so an mmp selects exactly the sources
that are already proven to compile), same provider rule (only LIBRARY-listed .defs become imports, euser
wins). What differs is the back end (runs on Linux with the x32 multilib GCC):

  * compile: the user-side line + the mmp's includes and macros + the codegen rules symcc taught the
    ARM side (no strict aliasing, keep null checks, no lifetime DSE, no thread-safe statics,
    per-function sections for --gc-sections)
  * entry/runtime: port/x64user xu_dll.cpp (UID3 at _E32Dll+12) or xu_exe.cpp, xu_image.cpp
    (__unimpl_export), xu_cxxrt.cpp, the image's own gcchelp.cpp operator new/delete, and for a
    DLL the REAL Dll::Tls/FileName (edllstub.lib: up_dll_tls.cpp, up_dll_file.cpp)
  * link: ld -m elf32_x86_64 -q --gc-sections with x32_dll.ld / x32_exe.ld; imports through
    devlink.make_veneers_x32; unresolved names -> `jmp __unimpl_export` stubs
  * image: e32image.build_dll(cpu=CPU_X86) + the .pcrel sidecar the XIP prelinker needs

.def names are EABI-mangled; x86_name() gives the amd64 spelling (va_list is the only delta).
Resources are arch-neutral: this builder compiles no resources. They are built once by the resource
step (build_mmp.py --rsc <key>, output below BUILD/rsc) and shared with the ARM build; the ROM
manifest generators pick them up from there.

Directories (paths.py): BUILD/x32img holds the images (+ .pcrel, .exports.json), BUILD/mmp-x32 the work
files. PHOREPOC_S4=1 uses BUILD/s4, PHOREPOC_QT48=1 BUILD/qt48 instead (providers resolve there first,
then from the plain images).

    python3 tools/emitter/build_mmp_x32.py <key> [<key> ...]
    python3 tools/emitter/build_mmp_x32.py --closure <key>     # + every registry dependency, providers first
    python3 tools/emitter/build_mmp_x32.py --all | --list-closure <key> | --list-closure-roms <key>
"""
from __future__ import annotations

import concurrent.futures as cf
import hashlib
import json
import os
import re
import shutil
import struct
import subprocess
import time
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

from paths import OS_ROOT, BUILD, SYMBIAN, prefix_maps, resolve as pathres           # noqa: E402
import mmp as MMP                                   # noqa: E402
MMP.CPP = "cpp"                                     # same -undef -nostdinc run as the ARM build
# Some mmps `#include <vg\openvguids.hrh>` (backslash): a Linux cpp rejects that. Preprocess such a file from a
# with the original dir on -I.
_pp = MMP.preprocess
_BSLASH_INC = re.compile(r"^\s*#\s*include\s*[<\"][^>\"]*\\", re.M)
_INC_PATH = re.compile(r"^(\s*#\s*include\s*[<\"])([^>\"]*)", re.M)


def _preprocess(path, extra_defs=(), markers=False):
    text = open(path, errors="replace").read()
    if not _BSLASH_INC.search(text):
        return _pp(path, extra_defs, markers=markers)
    fixed = _INC_PATH.sub(lambda m: m.group(1) + m.group(2).replace("\\", "/"), text)
    tmp = os.path.join("/tmp/mmpfix", path.lstrip("/"))
    os.makedirs(os.path.dirname(tmp), exist_ok=True)
    open(tmp, "w", newline="\n").write(fixed)
    out = _pp(tmp, tuple(extra_defs) + ("-I", os.path.dirname(path)), markers=markers)
    return out.replace('"/tmp/mmpfix/', '"/') if markers else out


MMP.preprocess = _preprocess
import components as C                              # noqa: E402
import devlink                                      # noqa: E402
import e32image                                     # noqa: E402
import elf                                          # noqa: E402
from euser_def import DefMap, parse_def             # noqa: E402
STRINGTABLE_PL = os.path.join(MMP.BASE, "oss.FCL.sf.os.ossrv", "lowlevellibsandfws", "apputils",
                              "stringtools", "stringtable.pl")

PORT = os.path.join(OS_ROOT, "tools", "phorepoc-x86", "port")       # port_prelude.h, x64user/ ...
XU = os.path.join(PORT, "x64user")
# the board's variant.mmh (board-specific mmps such as the key map #include it)
MMP_DEFS = ("-I", os.path.join(OS_ROOT, "tools", "phorepoc-x86", "board"))
# PHOREPOC_S4=1 (Symbian^4): a SEPARATE output root -- work files and images go to BUILD/s4, while providers
# resolve there first and then from the plain images (one OS, built once).
# PHOREPOC_QT48=1 (the Qt 4.8 set): its own root BUILD/qt48; the Qt 4.7 images in BUILD/s4 stay untouched.
S4 = os.environ.get("PHOREPOC_S4") == "1"
S4_BUILD = os.path.join(BUILD, "qt48" if os.environ.get("PHOREPOC_QT48") == "1" else "s4")
WORK_ROOT = os.path.join(S4_BUILD if S4 else BUILD, "mmp-x32")
IMG = os.path.join(BUILD, "x32img")                 # the plain images (providers)
IMG_OUT = os.path.join(S4_BUILD, "x32img") if S4 else IMG
CASEFIX = os.path.join(BUILD, "casefix")
CASEFIX_FLAGS = ["-idirafter", CASEFIX] if os.path.isdir(CASEFIX) else []
# last of all: the Symbian source root, so our files can include an unmodified source file as <symbian-2010/...>
# (never a path relative to a checkout layout)
CASEFIX_FLAGS += ["-idirafter", SYMBIAN]


def img_dir(rom: str) -> str:
    """Where a provider image lives: the S^4 output first (that build), else the plain images."""
    if S4 and os.path.isfile(os.path.join(IMG_OUT, rom)):
        return IMG_OUT
    return IMG


STUB = "__unimpl_export"
SUPCXX = "/usr/lib/gcc/x86_64-linux-gnu/13/x32/libsupc++.a"   # the x32 multilib (GCC 13, Ubuntu 24.04)
JOBS = int(os.environ.get("MMP_JOBS", str(os.cpu_count() or 8)))
DEFAULT_STACK = 0x5000

KHS = pathres("baseline/kernelhwsrv")               # the kernelhwsrv checkout
EKA = os.path.join(KHS, "kernel", "eka")
FS = os.path.join(KHS, "userlibandfileserver", "fileserver")

# The user-side compile line, plus symcc's codegen rules.
BASE_FLAGS = ["-mx32", "-ffreestanding", "-fno-pic", "-fno-stack-protector", "-nostdinc",
              "-std=gnu++03", "-fno-exceptions", "-fno-rtti", "-fpermissive", "-w", "-masm=intel", "-O2",
              "-fno-builtin", "-fno-tree-loop-distribute-patterns", "-fshort-wchar",
              "-fno-access-control", "-fno-threadsafe-statics", "-fno-strict-aliasing",
              "-fno-delete-null-pointer-checks", "-fno-lifetime-dse", "-fno-short-enums",
              "-ffunction-sections", "-fdata-sections", "-fcf-protection=none",
              # `TPckg<TInt> p(x.MaxLength()); ... SendReceive(.., &p)` -- TPckg keeps the ADDRESS
              # of a temporary that is dead by the IPC. RVCT left such slots alone; GCC reuses them and the
              # server reads garbage. ~20 sites across the tree (skins, tz, alarms, ESOCK,
              # nifman, contacts...). named_vars = never reuse a temporary's slot: the RVCT behaviour, no
              # source change. (Named locals still share slots.)
              "-fstack-reuse=named_vars"] + prefix_maps()
BASE_DEFS = ["-D__CPU_X86", "-D__GCC32__", "-D__EPOC32__", "-D__SYMBIAN32__", "-D_UNICODE",
             "-D__NO_CLASS_CONSTS__", "-DSYMBIAN_ENABLE_64_BIT_FILE_SERVER_API",
             "-D__IEEE_LITTLE_ENDIAN"]   # libc machine/ieeefp.h knows __i386__ and __arm__, not x86-64
# toolchain/overlay FIRST: the modern-GCC fixes that are not ARM-specific -- TTrap::Trap declared
# returns_twice (else -O2 keeps TRAP-crossing locals in registers and a Leave restores stale
# values) and the _LIT_SECURE_ID temporary-reference fix. See the two files.
BASE_INCS = ["-I", os.path.join(OS_ROOT, "toolchain", "overlay"), "-I", XU, "-I", os.path.join(EKA, "include"),
             "-I", os.path.join(EKA, "euser"), "-I", os.path.join(EKA, "common"),
             "-I", os.path.join(KHS, "halservices", "hal", "inc"),
             "-I", os.path.join(OS_ROOT, "toolchain", "kinc"),
             "-I", os.path.join(OS_ROOT, "toolchain", "generated"), "-I", PORT] + CASEFIX_FLAGS
PRE = ["-include", os.path.join(PORT, "port_prelude.h"), "-include", "e32def.h"]

# name -> (rom name, def path, export-sidecar path)
BUILTIN = {
    "euser": ("euser.dll", os.path.join(EKA, "eabi", "euseru.def"),
              os.path.join(BUILD, "euser-x32", "euser_exports.json")),
    "efsrv": ("efsrv.dll", os.path.join(FS, "eabi", "efsrvu.def"),
              os.path.join(BUILD, "efsrv-x32", "efsrv_exports.json")),
    "hal": ("hal.dll", os.path.join(KHS, "halservices", "hal", "eabi", "halu.def"),
            os.path.join(BUILD, "hal-x32", "hal_exports.json")),
    "efile": ("efile.exe", os.path.join(FS, "eabi", "efileu.def"),
              os.path.join(BUILD, "efile-x32", "efile_exports.json")),
}
EUSER_EXTRA = ["_ZN5TTrap4TrapERi", "_ZN5TTrap6UnTrapEv"]     # build_euser_x32.EXTRA_EXPORTS
VA_ALIAS = ("St9__va_list", "P13__va_list_tag")               # build_euser_x32.X86_ALIAS


def portable_flags(flags: list[str]) -> list[str]:
    """The registry's ARM `flags` minus the machine options (-m...): what is left -- include paths,
    -include'd prefix files, -D/-U, -f codegen switches -- means the same on x32 (hnengine's
    -I.../xml/dom was lost when the x32 line ignored `flags` altogether)."""
    out, skip = [], False
    for i, f in enumerate(flags):
        if skip:
            skip = False
            continue
        if f.startswith("-m"):
            if f in ("-mcpu", "-march", "-mfpu", "-mfloat-abi"):
                skip = True
            continue
        out.append(f)
    return out


def mmp_of(comp) -> str:
    return comp.get("x32_mmp", comp["mmp"])


def x86_name(name: str) -> str:
    # RVCT mangles an extern "C" FUNCTION TYPE with a 'Y' after the 'F' (Itanium's optional marker); GCC
    # never emits it -- Qt's qHBNewFace(void*, HB_GetFontTableFunc) is `...PvPFY8HB_Error...` in the frozen .def,
    # `...PvPF8HB_Error...` here, so neither the export nor QtGui's import ever matched (EUSRNIMP)
    return re.sub(r"(?<=[PR])FY", "F", name.replace(*VA_ALIAS))


LD = "ld" if os.environ.get("X32_LD") == "bfd" or not shutil.which("ld.lld") else "ld.lld"


def run(cmd, **kw) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, capture_output=True, text=True, errors="replace", **kw)


def nm(mode: str, *files: str) -> set[str]:
    out = run(["nm", mode, *files]).stdout
    names = set()
    for line in out.splitlines():
        p = line.split()
        if mode == "--defined-only":
            if len(p) == 3 and p[1] in ("T", "W", "R", "D", "B", "V"):
                names.add(p[2])
        elif p:
            names.add(p[-1])
    return names


RETURN_CENSUS = os.environ.get("X32_RETURN_CENSUS")   # a report path: opt-in, see return_census()


def return_census(src: str, flags: list[str]) -> None:
    """Falling off the end of a non-void function is undefined; GCC treats that end as unreachable
    and deletes code on the way to it (Speed dial's CreateIndexIconsL lost its loop exit), RVCT just
    returned garbage. The build runs with -w: re-run the TU syntax-only with -Wreturn-type and append
    every such site to the report. Opt-in (X32_RETURN_CENSUS=<file>); a normal build is unchanged."""
    cmd = ["gcc", *[f for f in flags if f != "-w"], "-fsyntax-only", "-Wno-all", "-Wreturn-type", src]
    r = run(cmd, cwd=BUILD)
    hits = [l for l in (r.stdout + r.stderr).splitlines()
            if "control reaches end of non-void function" in l or "no return statement in function" in l]
    if hits:
        with open(RETURN_CENSUS, "a") as fh:
            fh.write("\n".join(hits) + "\n")


def compile_one(src: str, obj: str, flags: list[str]) -> tuple[str, int, str]:
    if RETURN_CENSUS:
        return_census(src, flags)
    cmd = ["gcc", *flags, "-c", src, "-o", obj, "-MMD", "-MF", obj + ".d"]
    key = hashlib.sha1((" ".join(cmd)).encode()).hexdigest()
    stamp = obj + ".key"
    if (os.path.exists(obj) and os.path.exists(stamp) and open(stamp).read() == key
            and os.path.getmtime(obj) >= os.path.getmtime(src) and not deps_newer(obj)):
        return src, 0, ""
    r = run(cmd, cwd=BUILD)
    # the container's filesystem is neither. On "No such file", resolve the name that way against
    # this compile's include path and retry with a shim dir of symlinks .
    # The shim goes FIRST (a backslash name is EINVAL on the bind mount, which ends the search) and
    # is per include-path configuration, so a name resolved for one component shadows no other's.
    shim = os.path.join(CASESHIM, hashlib.sha1(" ".join(flags).encode()).hexdigest()[:12])
    for _ in range(4):
        if r.returncode == 0:
            break
        if not case_shim(r.stderr, src, flags, shim):
            break
        if shim not in cmd:
            cmd = cmd[:1] + ["-I", shim] + cmd[1:]
        r = run(cmd, cwd=BUILD)
    if r.returncode == 0:
        open(stamp, "w").write(key)
    elif os.environ.get("X32_FULLERR"):
        # (the command itself, to rerun one TU by hand: -E, -H, -dM ...)
        return src, r.returncode, "$ " + " ".join(cmd) + "\n" + r.stdout + r.stderr
    return src, r.returncode, r.stdout + r.stderr


CASESHIM = "/tmp/caseshim"                      # container-local: symlinks do not survive the bind mount
_LISTING: dict = {}


def _ci_child(d: str, name: str):
    if d not in _LISTING:
        try:
            _LISTING[d] = {n.lower(): n for n in os.listdir(d)}
        except OSError:
            _LISTING[d] = {}
    real = _LISTING[d].get(name.lower())
    return os.path.join(d, real) if real else None


def _ci_resolve(base: str, rel: str):
    p = base
    for part in [x for x in rel.split("/") if x]:
        if part == "..":
            p = os.path.dirname(p)
            continue
        p = _ci_child(p, part)
        if not p:
            return None
    return p if os.path.isfile(p) else None


def case_shim(stderr: str, src: str, flags: list[str], shim: str) -> bool:
    """Symlink every 'fatal error: X: No such file' that a case-insensitive, slash-agnostic lookup
    finds in the include path into CASESHIM under the exact name requested. True if any was made."""
    import re as _re
    made = False
    for m in _re.finditer(r"^(.*?):\d+:\d+: fatal error: (.+?): (?:No such file or directory|Invalid argument)",
                          stderr, _re.M):
        includer, req = m.group(1), m.group(2)
        if "\\" in req and req.startswith("/"):           # EINVAL reports <include dir>/<requested name>
            req = req[req.rfind("/", 0, req.find("\\")) + 1:]
        dirs = [os.path.dirname(os.path.join(BUILD, includer)), os.path.dirname(os.path.join(BUILD, src))]
        for i, f in enumerate(flags):
            if f in ("-I", "-iquote") and i + 1 < len(flags):
                dirs.append(flags[i + 1])
            elif f.startswith("-I") and len(f) > 2:
                dirs.append(f[2:])
        rel = req.replace("\\", "/")
        hit = next((h for h in (_ci_resolve(os.path.join(BUILD, d), rel) for d in dirs) if h), None)
        if not hit:
            continue
        link = os.path.join(shim, req)
        os.makedirs(os.path.dirname(link), exist_ok=True)
        try:
            os.symlink(hit, link)
        except FileExistsError:
            pass
        made = True
    return made


def deps_newer(obj: str) -> bool:
    try:
        text = open(obj + ".d", errors="replace").read()
    except OSError:
        return False
    text = text.replace("\\\n", " ")
    body = text.split(": ", 1)[1] if ": " in text else ""
    t = os.path.getmtime(obj)
    for d in body.split():
        try:
            if os.path.getmtime(d) > t:
                return True
        except OSError:
            return True
    return False


# LIBRARY names the release uses but never builds, and the DLL that really provides what their users
# import. aknlayout.lib (S60's layout import library; no mmp anywhere in the release): its users' only
# import is CdlEngine::GetFunction, called by the Layout_Meta_Data / layout .cdl.h inlines -- which lives
# in cdlengine.dll (Speed dial's status pane, the SMIL player, the ITU-T keypad, the FEP UI)
LIB_ALIASES = {"aknlayout": "cdlengine", "mpm": "mpmclient"}
if os.environ.get("PHOREPOC_QT48") == "1":
    LIB_ALIASES["qtmain"] = "qt48_main"     # the STATICLIBRARY every Qt program links (static_archive below)
    LIB_ALIASES.update({m: "qt48_" + m[2:] for m in ("qtcore", "qtgui", "qtnetwork", "qtsvg", "qtsql", "qtxml", "qtscript", "qtdeclarative", "qtmultimediakit")})


def provider_maps(libs):
    """name -> (rom, ordinal) over the LIBRARY list (euser last = wins) + DATA-export names +
    rom -> export sidecar (for the stub check)."""
    global STUBBED_LIBS
    STUBBED_LIBS = []
    order, data_names = [], set()
    stems = [lib.rsplit(".", 1)[0].lower() for lib in libs]
    libs = [LIB_ALIASES.get(s, s) + ".lib" if s in LIB_ALIASES and LIB_ALIASES[s] not in stems else lib
            for s, lib in zip(stems, libs)]
    for lib in libs:
        stem = lib.rsplit(".", 1)[0].lower()
        if stem in BUILTIN:
            rom, defp, side = BUILTIN[stem]
        elif stem in C.COMPONENTS:
            m = MMP.parse(os.path.join(MMP.BASE, mmp_of(C.COMPONENTS[stem])), MMP_DEFS)
            rom = C.rom_name(stem, m.targettype)
            defp = (os.path.join(MMP.BASE, C.COMPONENTS[stem]["def"])
                    if "def" in C.COMPONENTS[stem] else m.def_path())
            side = os.path.join(img_dir(rom), rom + ".exports.json")
            if not os.path.isfile(os.path.join(img_dir(rom), rom)):
                # registered but not built (it failed, or is not reached yet): binding to its .def
                # would put an import of a DLL that does not exist into this image, and the loader
                # (rombuild's prelink first) refuses the WHOLE image. Stub instead: only the calls
                # that actually run fail. Rebuild this component once the provider exists.
                print(f"  ! LIBRARY {lib}: provider {rom} not built (its imports -> stubs)")
                STUBBED_LIBS.append(stem)
                continue
        else:
            print(f"  ! LIBRARY {lib}: no provider registered (its imports -> stubs)")
            STUBBED_LIBS.append(stem)
            continue
        if not defp:
            print(f"  ! LIBRARY {lib}: no .def found")
            continue
        order.append((stem, rom, defp, side))
    resolve, sidecars = {}, {}
    for stem, rom, defp, side in sorted(order, key=lambda t: t[0] == "euser"):
        d = parse_def(defp)
        for ordn, name in d.by_ordinal.items():
            if name not in d.absent:                # an ABSENT slot provides nothing
                resolve[x86_name(name)] = (rom, ordn)
        data_names.update(x86_name(n) for n in d.data_names)
        if stem == "euser":
            for i, n in enumerate(EUSER_EXTRA):
                resolve[n] = (rom, d.max_ordinal + 2 + i)
        sidecars[rom] = side
    # a `libs` entry naming another .def of a DLL the mmp already lists (cone2.lib beside cone.lib) must not
    # give that DLL a second import block
    return resolve, list(dict.fromkeys(rom for _, rom, _, _ in order)), data_names, sidecars


SF_ROOT = "/tmp/sflayout"


def sf_layout(comp, m) -> list[str]:
    """`sf_layout`: the component quotes a SIBLING package by a relative path written for the original
    source layout, sf/os/<package> (`#include "../../../../commsfw/..."` from networkingsrv). Mirror
    that layout: the component's own source dirs as REAL (empty) directories under SF_ROOT/os/<pkg>,
    every other oss.FCL.sf.os.* package as a symlink beside them, and each dir on -iquote -- so the
    `..` walk stays inside the mirror (a symlinked own dir would `..` back into baseline/)."""
    if not comp.get("sf_layout"):
        return []
    rel = os.path.relpath(m.path, MMP.BASE).replace("\\", "/")
    own = rel.split("/")[0]
    osroot = os.path.join(SF_ROOT, "os")
    os.makedirs(osroot, exist_ok=True)
    for pkg in os.listdir(MMP.BASE):
        if pkg.startswith("oss.FCL.sf.os.") and pkg != own:
            link = os.path.join(osroot, pkg[len("oss.FCL.sf.os."):])
            if not os.path.lexists(link):
                os.symlink(os.path.join(MMP.BASE, pkg), link)
    out = []
    for d in sorted({os.path.dirname(s) for s in m.sources}):
        r = os.path.relpath(d, os.path.join(MMP.BASE, own))
        mirror = os.path.normpath(os.path.join(osroot, own[len("oss.FCL.sf.os."):], r))
        os.makedirs(mirror, exist_ok=True)
        out += ["-iquote", mirror]
    return out


def build(key: str) -> int:
    comp = C.COMPONENTS[key]
    m = MMP.parse(os.path.join(MMP.BASE, mmp_of(comp)), MMP_DEFS)
    rom = C.rom_name(key, m.targettype)
    work = os.path.join(WORK_ROOT, key)
    os.makedirs(work, exist_ok=True)
    os.makedirs(IMG_OUT, exist_ok=True)
    print(f"=== {key}: {m.target} ({m.targettype}) -> {rom}   [{len(m.sources)} sources]")

    incs = [x for p in m.userinc for x in ("-iquote", p)] + sf_layout(comp, m)
    noinc = comp.get("noinc", [])
    user_sys = [f"-I{p}" for p in m.userinc + m.sysinc
                if not any(n in p.replace("\\", "/").lower() for n in noinc)]
    kind = "-D__EXE__" if m.is_exe else "-D__DLL__"
    # X32_DEFS="<key>:<MACRO>[,<key>:<MACRO>...]" -- opt-in diagnostic builds without a registry edit,
    # e.g. X32_DEFS=centralrepositorysrv:CENTREP_TRACE names every repository the server opens
    env_defs = [d.split(":", 1)[1] for d in os.environ.get("X32_DEFS", "").split(",")
                if ":" in d and d.split(":", 1)[0] == key]
    # X32_INCLUDE="<key>:<repo-relative header>[,...]" -- the same, force-included (-include) after the variant
    # header, e.g. X32_INCLUDE=mpmserver:toolchain/shims/mpm_rdebug/mpmlogger_rdebug.h = MPM's log on serial
    env_incs = [x for d in os.environ.get("X32_INCLUDE", "").split(",")
                if ":" in d and d.split(":", 1)[0] == key
                for x in ("-include", pathres(d.split(":", 1)[1]))]
    # incs_first: board copies of headers that must win over the mmp's own SYSTEMINCLUDE dirs (the STLport
    # compiler configuration, components.STLPORT_X32) -- searched before everything else
    # (-iquote too: a quoted #include searches the mmp's USERINCLUDE -iquote dirs before any -I -- Open C's "sysif.h")
    first = [x for p in comp.get("incs_first", []) for x in (f"-iquote{pathres(p)}", f"-I{pathres(p)}")]
    flags = (BASE_FLAGS + BASE_DEFS + [kind] + [f"-D{x}" for x in m.macros + comp.get("defs", []) + env_defs]
             + first + incs + BASE_INCS + user_sys
             # the variant .hrh BEFORE e32def.h (abld's order): it defines __LEAVE_EQUALS_THROW__,
             # which e32def.h then drops because we build without C++ exceptions
             + ["-include", MMP.variant_for(m.path)] + PRE + env_incs
             + portable_flags(comp.get("flags", []))
             + comp.get("x32_flags", []))
    # PHOREPOC_QT48=1: components_s4 copies x32_flags into flags too (ARM shares them) -> a repeated "-include X" pair; unguarded headers
    # (symcpp.h: std::nothrow_t) then redefine. Keep the first of each force-include.
    _seen, _dedup, _i = set(), [], 0
    while _i < len(flags):
        if flags[_i] == "-include" and _i + 1 < len(flags):
            if flags[_i + 1] not in _seen:
                _seen.add(flags[_i + 1])
                _dedup += flags[_i:_i + 2]
            _i += 2
            continue
        _dedup.append(flags[_i])
        _i += 1
    flags = _dedup
    rt_flags = BASE_FLAGS + BASE_DEFS + [kind] + BASE_INCS + PRE
    # one needs a typeinfo slot in its vtable -> on for the mw/app layers + opt-in (`rtti`). The
    # runtime is the x32 libsupc++ () + toolchain/dll_rt/rtti_rt.cpp (its strcmp).
    rtti = comp.get("rtti", MMP.variant_for(m.path) == MMP.S60_VARIANT_HRH)
    if rtti:
        flags = [f for f in flags if f != "-fno-rtti"] + ["-frtti"]

    real = comp.get("x32_real")
    drop = {d.lower() for d in ([] if real else comp.get("drop", [])) + comp.get("x32_drop", [])}
    srcs = [] if comp.get("nosrc") and not real else [s for s in m.sources if os.path.basename(s).lower() not in drop]
    srcs += [pathres(e) for e in comp.get("x32_extra", [] if real else comp.get("extra", []))]
    # the TStringTable + its _STLIT8s) + x.h. Missing until now, so e.g. XMLEntityReferences::Table
    # linked to xmlparser's unimpl stub and RStringPool::OpenL walked code bytes (mcsmenuserver).
    # The .h is generated next to the .cpp and put on the include path; epoc32/include is not touched.
    for st in m.stringtables:
        gen = os.path.join(work, "stringtable")
        os.makedirs(gen, exist_ok=True)
        local = os.path.join(gen, os.path.basename(st))
        stem = os.path.join(gen, os.path.splitext(os.path.basename(st))[0].lower())
        if not os.path.isfile(stem + ".cpp") or os.path.getmtime(stem + ".cpp") < os.path.getmtime(st):
            shutil.copyfile(st, local)
            r = subprocess.run(["perl", STRINGTABLE_PL, "./" + os.path.basename(local)], cwd=gen,
                               capture_output=True, text=True)
            if r.returncode or not os.path.isfile(stem + ".cpp"):
                print(f"  STRINGTABLE FAIL {os.path.basename(st)}: {(r.stdout + r.stderr).strip()[-300:]}")
                return 1
        srcs.append(stem + ".cpp")
        flags = flags + [f"-I{gen}"]
    arm_only = [s for s in srcs if s.lower().endswith(".cia")]
    if arm_only:
        print("  ! ARM naked-asm sources need an x32 replacement (x32_drop/x32_extra): "
              + ", ".join(os.path.basename(s) for s in arm_only))
        return 1

    uid2 = m.uids[0] if m.uids else 0
    uid3 = m.uids[1] if len(m.uids) > 1 else 0
    if m.targettype == "fsy" and not uid2:
        uid2 = 0x100039DF       # KFileSystemUidValue: the loader's REQ-UID check (abld's fsy rule)
    rt = [(os.path.join(XU, "xu_image.cpp"), []), (os.path.join(XU, "xu_cxxrt.cpp"), [])]
    # the image's own operator new/delete -- unless the component defines (and exports) its own: own_new
    # (Open C++'s libstdcpp.dll routes the global new/delete through its allocator)
    if not comp.get("own_new"):
        rt += [(os.path.join(EKA, "common", "gcchelp.cpp"), [])]
    if m.is_exe:
        entry_src = (os.path.join(XU, "xu_exe.cpp"), [])
    else:
        entry_src = (os.path.join(XU, "xu_dll.cpp"), [f"-DDLL_UID3=0x{uid3:08x}"])
        rt += [(os.path.join(EKA, "euser", "epoc", "up_dll_tls.cpp"), []),
               (os.path.join(EKA, "euser", "epoc", "up_dll_file.cpp"), [])]
    if rtti:
        rt += [(os.path.join(OS_ROOT, "toolchain", "dll_rt", "rtti_rt.cpp"), [])]

    jobs, objs = [], []
    for i, s in enumerate(srcs):
        obj = os.path.join(work, f"{i:03d}_{os.path.basename(s)}.o")
        extra = []
        if comp.get("cdriver") and s.lower().endswith(".c"):
            # called `std`), without the C++-only switches; C has no built-in wchar_t and the prefix
            # header typedefs __TText from it (cstdlib's stddef.h typedef switched off to match)
            # (`c_std`: another C dialect for the component's C sources, e.g. FFmpeg's C11)
            cstd = "-std=" + comp.get("c_std", "gnu99")
            cflags = [(cstd if f == "-std=gnu++03" else f) for f in flags
                      if f not in ("-frtti", "-fno-rtti", "-fno-access-control", "-fno-threadsafe-statics",
                                   "-fpermissive", "-fno-exceptions")]
            jobs.append((s, obj, ["-x", "c"] + cflags + ["-Dwchar_t=unsigned short", "-D__wchar_t_defined"]))
            objs.append(obj)
            continue
        # GCC knows .cpp/.CPP/.cc/.cxx/.C; any other spelling (contacts: CCntSession.Cpp) is taken as
        # LINKER INPUT -- nothing compiled, exit 0, and the link then misses the object 
        if s.endswith(".C") or os.path.splitext(s)[1] not in (".cpp", ".CPP", ".cc", ".cxx", ".c", ".c++", ".cp"):
            extra = ["-x", "c++"]
        jobs.append((s, obj, extra + flags))
        objs.append(obj)
    entry_o = os.path.join(work, "rt_entry.o")
    jobs.append((entry_src[0], entry_o, rt_flags + entry_src[1]))
    rt_objs = []
    for s, extra in rt:
        o = os.path.join(work, "rt_" + os.path.basename(s) + ".o")
        jobs.append((s, o, rt_flags + extra))
        rt_objs.append(o)
    if m.targettype == "lib":           # a static library: its own objects only, no entry/runtime
        jobs = jobs[:len(objs)]
    failed = []
    with cf.ThreadPoolExecutor(JOBS) as ex:
        for src, rc, out in ex.map(lambda j: compile_one(*j), jobs):
            if rc:
                failed.append((src, out))
    if failed:
        seen = {}
        for src, out in failed:
            errs = [l for l in out.splitlines() if "error" in l or "fatal" in l][:4]
            seen.setdefault("|".join(e.split(": ", 1)[-1] for e in errs[:1]), []).append((src, errs))
        for _sig, group in seen.items():
            src, errs = group[0]
            more = f"  (+{len(group) - 1} more TUs, same first error)" if len(group) > 1 else ""
            print(f"  COMPILE FAIL {os.path.relpath(src, BUILD)}{more}")
            for l in errs:
                print("      " + l[-220:])
        print(f"  {len(failed)}/{len(jobs)} TUs failed")
        if os.environ.get("X32_FULLERR"):
            # X32_FULLERR=1: the first failure's whole compiler output (include chains, notes)
            print(failed[0][1][:6000])
        return 1
    if m.targettype == "lib":
        # STATICLIBRARY: an ar archive the consumers' link pulls members from (static_archive())
        a = static_archive(key)
        if os.path.exists(a):
            os.remove(a)
        r = run(["ar", "rcs", a, *objs])
        if r.returncode:
            print(r.stderr[-2000:])
            return 1
        print(f"  wrote {os.path.relpath(a, BUILD)} ({len(objs)} objects)")
        return 0
    # the mmp's STATICLIBRARY list: registered `lib` components, built first (closure())
    archives = []
    for sl in m.staticlibs:
        if sl.lower() == "edllstub.lib":    # built in: the runtime links the real Dll::Tls/FileName (header)
            continue
        _sn = sl.rsplit(".", 1)[0]
        a = static_archive(LIB_ALIASES.get(_sn.lower(), _sn) if os.environ.get("PHOREPOC_QT48") == "1" else _sn)   # qtmain -> qt48_main
        if not os.path.exists(a):
            print(f"  ! STATICLIBRARY {sl}: {os.path.relpath(a, BUILD)} not built")
            return 1
        archives.append(a)
    # `x32_archives`: PREBUILT ar archives (repo-relative), linked like STATICLIBRARYs,
    # built outside the registry
    for a in comp.get("x32_archives", []):
        a = pathres(a)
        if not os.path.exists(a):
            print(f"  ! x32_archives: {os.path.relpath(a, BUILD)} not built")
            return 1
        archives.append(a)

    # --- exports ------------------------------------------------------------------------------
    defp = os.path.join(MMP.BASE, comp["def"]) if "def" in comp else m.def_path()
    if m.targettype == "plugin" and "def" not in comp and not m.deffile:
        defp = None
    defmap = parse_def(defp) if defp else None
    if m.targettype == "plugin" and not defmap:
        defmap = DefMap({1: "_Z24ImplementationGroupProxyRi"}, 1)
    if m.targettype == "ani" and not defmap:
        defmap = DefMap({1: "_Z15CreateCAnimDllLv"}, 1)
    if m.targettype == "fsy" and not defmap:
        # a file-system plug-in: no .def; EFILE does Lookup(1) -> CreateFileSystem (extern "C")
        defmap = DefMap({1: "CreateFileSystem"}, 1)
    if not m.is_exe and not defmap:
        print("  ! no .def found -- DLL would export nothing")
        return 1
    all_objs = objs + rt_objs
    # a STATICLIBRARY can define exports too (ImageConversion.mmp: CImageProcessorExtension lives in
    # imageprocessorperf.lib): counting them makes them `-u` roots, which pulls the members in 
    defined = nm("--defined-only", entry_o, *all_objs, *archives)
    served = {}
    if defmap:
        for o, n in defmap.by_ordinal.items():
            xn = x86_name(n)
            if xn in defined:
                served[o] = xn
            elif n in defined:
                served[o] = n
    entry = "_E32Startup" if m.is_exe else "_E32Dll"
    roots = sorted(set(served.values())) + [STUB, entry]
    ld = os.path.join(HERE, ("x32_exe_eh.ld" if comp.get("eh") else "x32_exe.ld") if m.is_exe else "x32_dll.ld")

    def link(extra_objs, out, ignore):
        rsp = os.path.join(work, "link.rsp")
        with open(rsp, "w") as fh:
            fh.write("\n".join(f"-u {n}" for n in roots) + "\n")
            # archives as a group: armlink rescans every library until nothing new resolves, GNU ld
            # scans each once in order -- WebCore's 15 STATICLIBRARYs reference each other in cycles
            grp = ["--start-group"] + archives + ["--end-group"] if len(archives) > 1 else archives
            fh.write("\n".join([entry_o] + all_objs + extra_objs + grp + ([SUPCXX] if rtti else [])) + "\n")
        # 260 MB of archives in seconds). X32_LD=bfd forces GNU ld.
        args = [LD, "-m", "elf32_x86_64", "-n", "-q", "--gc-sections", "--no-demangle",
                "-T", ld, f"@{rsp}", "-o", out]
        if ignore:
            args.insert(1, "--unresolved-symbols=ignore-all")
        r = run(args)
        if r.returncode:
            print(r.stderr[-3000:])
            raise SystemExit(f"{key}: link failed")

    t_phase = [time.monotonic()]
    def phase(label):
        now = time.monotonic()
        t_phase.append(now)
        times.append((label, now - t_phase[-2]))
    times = []
    elf1 = os.path.join(work, key + ".pass1.elf")
    link([], elf1, True)
    phase("ld pass 1")
    missing = sorted(nm("--undefined-only", elf1) - {""})
    phase("nm")
    # euser is always a provider: a few tiny mmps (lookuptable, the stub rasterizer) omit euser.lib
    # and would otherwise stub User::Panic
    libs = comp.get("libs", []) + m.libs
    if not any(l.lower().startswith("euser.") for l in libs):
        libs = libs + ["euser.lib"]
    resolve, dll_order, data_names, sidecars = provider_maps(libs)
    imports = {n: resolve[n] for n in missing if n in resolve and resolve[n][0] != rom}
    ti_names = [n for n in missing if n.startswith("_ZTI")]
    for n in missing:
        if n.startswith(("_ZTI", "_ZTS")):
            imports.pop(n, None)
    stub_names = [n for n in missing if n not in imports and not n.startswith(("_ZTI", "_ZTS"))]
    data_imps = {n: imports.pop(n) for n in list(imports)
                 if n in data_names or n.startswith(("_ZTV", "_ZTI", "_ZTS"))}

    veneer_s = devlink.make_veneers_x32(work, imports.keys(), key)
    veneer_o = veneer_s + ".o"
    stub_s = os.path.join(work, "unimpl_stubs.s")
    with open(stub_s, "w", newline="\n") as fh:
        for n in stub_names:
            fh.write(f'\t.section .text.unimpl.{n},"ax",@progbits\n\t.globl {n}\n'
                     f"\t.type {n},@function\n{n}:\n\tjmp {STUB}\n")
        for n in data_imps:         # link-time placeholder; the site IS the import slot
            fh.write(f'\t.section .rodata.dimp.{n},"a",@progbits\n\t.balign 4\n\t.globl {n}\n{n}:\n\t.long 0\n')
        if ti_names:
            # A leaf __class_type_info (vptr, name): our providers are mostly -fno-rtti, so the
            # real typeinfo does not exist. libsupc++ compares typeinfos by NAME, so a leaf with
            # the right name matches; the vptr has to be real, though -- dynamic_cast walks the
            # bases through it (a null one = KERN-EXEC 3 at 0x1c, Phonebook2's CAknViewAppUi).
            vptr = "_ZTVN10__cxxabiv117__class_type_infoE+8" if rtti else "0"
            fh.write('\t.section .rodata.synth_typeinfo,"a",@progbits\n\t.balign 4\n')
            for n in ti_names:
                fh.write(f"\t.globl {n}\n{n}:\n\t.long {vptr}\n\t.long 1f\n1:\t.asciz \"{n[4:]}\"\n\t.balign 4\n")
        fh.write('\t.section .note.GNU-stack,"",@progbits\n')
    stub_o = stub_s + ".o"
    for s, o in ((veneer_s, veneer_o), (stub_s, stub_o)):
        r = run(["gcc", "-mx32", "-c", s, "-o", o])
        if r.returncode:
            print(r.stderr[-2000:])
            return 1

    elf2 = os.path.join(work, key + ".elf")
    phase("veneers/stubs")
    link([veneer_o, stub_o], elf2, False)
    phase("ld pass 2")
    with open(elf2, "rb") as fh:
        ext = elf.extract(fh.read(), data_imports=frozenset(data_imps))
    phase("elf extract")
    if entry not in ext.symbols:
        raise SystemExit(f"{key}: entry {entry} missing")
    if not m.is_exe and (ext.data or ext.bss_size) and not (comp.get("allow_wsd") or m.allowdlldata):
        print(f"  ! DLL has writable static data: data={len(ext.data)} bss={ext.bss_size}")
        syms = run(["nm", "-S", "--size-sort", "-C", elf2]).stdout.splitlines()
        for l in [l for l in syms if l.split()[2:3] and l.split()[2] in "bBdD"][-8:]:
            print("      " + l)
        return 1

    stub_off = ext.symbols[STUB] - ext.code_base
    export_offsets, real = [], 0
    if defmap:
        for ordn in range(1, defmap.max_ordinal + 1):
            n = served.get(ordn)
            if n and n in ext.symbols:
                export_offsets.append(ext.symbols[n] - ext.code_base)
                real += 1
            else:
                export_offsets.append(stub_off)
    import_list = devlink.import_blocks(ext, imports, dll_order=dll_order, arch="x32")
    if data_imps:
        blocks = {d: list(e) for d, e in import_list}
        for n, (dll, ordn) in data_imps.items():
            for off, addend in ext.data_import_sites.get(n, []):
                sa = addend - 0x100000000 if addend >= 0x80000000 else addend
                if not -0x8000 <= sa <= 0x7FFF:
                    raise SystemExit(f"{key}: DATA import {n} addend {sa} outside a signed 16-bit field")
                # a NEGATIVE addend (Qt: &QVectorData::shared_null - 8) is two's complement in the slot's upper half:
                # rombuild sign-extends it; the F32 loader (RAM-loaded images) would not -- none such yet
                blocks.setdefault(dll, []).append((off, ordn, sa & 0xFFFF))
        import_list = [(d, sorted(blocks[d])) for d in dll_order if d in blocks]
    # %rip-relative references to imported statics: the ROM prelinker aims them (the .pcrel sidecar's pcimp lines)
    ext.pcimp = [(dll, ordn, off, addend) for n, (dll, ordn) in data_imps.items()
                 for off, addend in ext.data_import_pcrel.get(n, [])]
    import_list = [(d, e) for d, e in import_list if e]
    n_imp = sum(len(e) for _, e in import_list)

    # imports that bind an ordinal its provider serves with its unimpl stub = EUSRNIMP at run time
    on_stub = []
    for n, (dll, ordn) in list(imports.items()) + list(data_imps.items()):
        side = sidecars.get(dll)
        if not side or not os.path.exists(side):
            continue
        sm = json.load(open(side))
        ent = sm["ordinals"].get(str(ordn))
        if ent is None or ent[1] == sm["stub_offset"]:
            if n in ext.symbols or n in data_imps:
                on_stub.append(f"{n} @ {dll} {ordn}")

    print(f"  code {len(ext.code)} B  data {len(ext.data)}  bss {ext.bss_size}   "
          f"exports {real}/{defmap.max_ordinal if defmap else 0} real   imports {n_imp} ("
          + ", ".join(f"{d} {len(e)}" for d, e in import_list) + f")   {len(stub_names)} link stubs"
          f"   {len(on_stub)} imports on a provider stub")
    if stub_names:
        dm = run(["c++filt"], input="\n".join(stub_names[:None if os.environ.get("X32_ALL_STUBS") else 15])).stdout.splitlines()
        for l in dm:
            print("      stub: " + l)
    if on_stub:
        dm = run(["c++filt"], input="\n".join(on_stub[:15])).stdout.splitlines()
        for l in dm:
            print("      on provider stub: " + l)

    kw = {}
    if m.is_exe:
        # EPOCSTACKSIZE was sized for RVCT Thumb frames; x86-64 frames are bigger (Logs: 0x2710 declared,
        # overflowed in AknLayoutUtils, ). Twice the declared size, the default as a floor, 80 KB cap.
        stack = comp.get("stack") or min(max(2 * (m.stack or 0), DEFAULT_STACK), 0x14000)
        # heap_max: the software OpenVG  rasterises vector icons inside the calling thread's heap,
        # CSvgtFbsRasterizer::RenderMaskL). A maximum only -- the heap still commits on demand -- so every
        # EXE gets at least 8 MB; `heap_max` in the registry overrides (wserv: 16 MB).
        kw = dict(exe=True, stack_size=stack,
                  heap_min=m.heap[0] or 0x1000,
                  heap_max=comp.get("heap_max") or max(m.heap[1] or 0x100000, 0x800000))
    # `sis` : an image that ships as a SIS is LOADED FROM RAM, where no prelinker re-aims its %rip->static
    # displacements (the .pcrel sidecar) -- they go into the code relocations as KX32PcRelRelocType (0x4000), which
    # the board's F32 loader applies as += (data delta - code delta); rombuild skips them (the sidecar covers ROM).
    code_types = dict(ext.code_reloc_types)
    if comp.get("sis"):
        code_types.update({o: 0x4000 for o in ext.code_pcrel_data})
        # the REAL loader wants the export directory inside the code, each entry code-relocated
        kw["exports_in_code"] = True
    img = e32image.build_dll(
        ext.code, export_offsets, cpu=e32image.CPU_X86, uid2=uid2, uid3=uid3,
        entry_point=ext.symbols[entry] - ext.code_base, imports=import_list,
        code_relocs=sorted(code_types) if code_types else ext.code_relocs, code_reloc_types=code_types,
        code_base=ext.code_base, data=ext.data, data_base=ext.data_base,
        bss_size=ext.bss_size, data_relocs=ext.data_relocs,
        data_reloc_types=ext.data_reloc_types,
        # every image carries ALL capabilities (the bring-up default); `sis`  = the mmp's CAPABILITY: SWI
        # compares the file description's capabilities with the image's, and grants only user capabilities
        cap1=m.cap_bits() if comp.get("sis") else 0xFFFFFFFF,
        cap2=0 if comp.get("sis") else 0xFFFFFFFF, **kw)
    out = os.path.join(IMG_OUT, rom)
    with open(out, "wb") as fh:
        fh.write(img)
    e32image.write_pcrel_sidecar(out, ext)
    phase("image + sidecar")
    if t_phase[-1] - t_phase[0] > 30:
        print("  [time] " + ", ".join(f"{l} {d:.0f}s" for l, d in times))
    with open(out + ".exports.json", "w", newline="\n") as fh:
        json.dump({"code_base": ext.code_base, "stub_offset": stub_off, "uid3": uid3,
                   "count": len(export_offsets),
                   "ordinals": {str(o): [served.get(o), export_offsets[o - 1]]
                                for o in range(1, len(export_offsets) + 1)}}, fh, indent=0)
    with open(out + ".meta", "w") as fh:
        fh.write(f"stub=0x{ext.symbols[STUB]:X}")
    # the LIBRARYs whose imports became stubs (no provider yet) -- an image linked BEFORE its provider
    with open(out + ".stubs", "w", newline="\n") as fh:
        fh.write("\n".join(globals().get("STUBBED_LIBS", [])) + "\n")
    if comp.get("sis"):
        # a SIS image is LOADED FROM RAM: the real F32 loader's checks must pass, or it answers KErrCorrupt
        from loadercheck import check_loader
        bad = check_loader(img)
        if bad:
            raise SystemExit(f"{key}: the real loader would refuse {rom}: " + "; ".join(bad))
    print(f"  wrote {os.path.relpath(out, BUILD)} ({len(img)} B)  "
          + ("[loader-checked]" if comp.get("sis") else ""))
    return 0


def static_archive(key: str) -> str:
    a = os.path.join(WORK_ROOT, key.lower(), key.lower() + ".a")
    # S^4 builds: an S^3 static library (P.I.P.S.'s libcrt0 ...) comes from the S^3 tree, as its images do
    if S4 and not os.path.exists(a) and key.lower() not in __import__("components_s4").COMPONENTS:
        s3 = os.path.join(BUILD, "mmp-x32", key.lower(), key.lower() + ".a")
        if os.path.exists(s3):
            return s3
    return a


def closure(roots: list[str]) -> list[str]:
    """The roots + every registry component they reach through LIBRARY (and `libs`) lists,
    providers first. BUILTIN modules have their own builders and are not part of it."""
    order, seen = [], set()

    def visit(k):
        if k in seen or k not in C.COMPONENTS:
            return
        seen.add(k)
        comp = C.COMPONENTS[k]
        m = MMP.parse(os.path.join(MMP.BASE, mmp_of(comp)), MMP_DEFS)
        for lib in comp.get("libs", []) + m.libs + m.staticlibs:
            visit(lib.rsplit(".", 1)[0].lower())
        order.append(k)
    for r in roots:
        visit(r)
    return order


def main() -> int:
    keys = sys.argv[1:]
    if not keys:
        print(__doc__)
        return 2
    if keys[0] == "--closure":          # build the roots' whole registry dependency closure
        keys = closure(keys[1:])
        print("closure: " + " ".join(keys))
    elif keys[0] == "--all":            # every registry component, providers first 
        keys = closure(list(C.COMPONENTS))
        print(f"all: {len(keys)} components")
    if keys[:1] == ["--list-closure"]:
        print(" ".join(closure(keys[1:])))
        return 0
    if keys[:1] == ["--list-closure-roms"]:     # the ROM file names, for tools/make_phorepoc_rom.sh
        print(" ".join(C.rom_name(k, MMP.parse(os.path.join(MMP.BASE, mmp_of(C.COMPONENTS[k])),
                                               MMP_DEFS).targettype) for k in closure(keys[1:])))
        return 0
    rc = 0
    for k in keys:
        if k not in C.COMPONENTS:
            print(f"=== {k}: not in emitter/components.py")
            rc = 1
            continue
        try:
            rc |= build(k)
        except (SystemExit, Exception) as e:
            print(f"  FAILED: {str(e)[:1500]}")
            rc = 1
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
