# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

r"""build_mmp -- build ANY registered component for ARM from its REAL 2010 .mmp, unmodified.

For a component key in components.py:

  1. parse the .mmp the way abld does (mmp.py: cpp + platform_paths.hrh +
     the product-variant .hrh) -> sources, include paths, macros, UIDs, LIBRARYs,
     the frozen EABI .def
  2. compile every SOURCE with tools/toolchain/symcc (parallel, cached by flags+mtime)
     against the exported epoc32/include tree (tools/epoc32_export.py)
  3. pass-1 link -> undefined set; every name with an ordinal in a LIBRARY's .def
     becomes a REAL import (devlink veneer); the rest fall to the loud unimpl stub
  4. final link -> elf.py -> E32Image with the REAL export ordinal table
     (unbuilt ordinals -> the unimpl stub)

    python3 build_mmp.py estor [bafl gdi ...]      python3 build_mmp.py --all
    python3 build_mmp.py --list                    python3 build_mmp.py --rsc <key>  (resources only)

Where the outputs land (BUILD = $PHOREPOC_BUILD; the x32 builder, rombuild and x32_rom_args read the same
resource / romfiles / registration locations):

    BUILD/arm/img/<rom name>            the ARM E32Image (+ <rom name>.meta: the unimpl-stub address for romgate)
    BUILD/arm/mmp-build/<key>/          objects, link response files, <key>.elf, <key>.pass1.elf; a `lib` target's
                                        archive <key>.a
    BUILD/rsc/<targetpath>/<stem>.rsc   compiled resources (components with `rsc_out` use BUILD/<rsc_out>/ instead)
    BUILD/epoc32/include/<stem>.rsg     the resource header (HEADER), plus generated stringtable headers
    BUILD/romfiles/<rom name>.romfiles  ROM manifest: `<rom path>=<host path>` lines, the host path RELATIVE to BUILD
                                        (a resource file, e.g. `resource\apps\x.rsc=rsc/resource/apps/x.rsc`)
    BUILD/plugins/<rom stem>.rsc        an ECOM plug-in's registration resource (z:\resource\plugins\)
"""
from __future__ import annotations

import concurrent.futures as cf
import glob
import hashlib
import os
import shutil
import subprocess
import sys
import threading

import build_euser_dll as BE
import build_euser_device as BD
import components as C
import devlink
import mmp as MMP
from e32image import build_dll
from elf import extract
from euser_def import parse_def
from paths import PDK, OS_ROOT, SYMBIAN, BUILD, prefix_maps, resolve as rpath
from refcheck import check_image, format_result

HERE = os.path.dirname(os.path.abspath(__file__))
ARM_ROOT = os.path.join(BUILD, "arm")
IMG_DIR = os.path.join(ARM_ROOT, "img")
OUT_ROOT = os.path.join(ARM_ROOT, "mmp-build")
ROMFILES_DIR = os.path.join(BUILD, "romfiles")
PLUGINS_DIR = os.path.join(BUILD, "plugins")
CASEFIX = os.path.join(BUILD, "casefix")      # tools/casefix.py: symlinks for every #include spelling (case-sensitive host)
STUB = BE.STUB_NAME
RT = os.path.join(OS_ROOT, "toolchain", "dll_rt")
GCCHELP = os.path.join(BE.B, "common", "gcchelp.cpp")
EE = os.path.join(OS_ROOT, "toolchain", "efile_exe")
STRINGTABLE_PL = os.path.join(MMP.BASE, "oss.FCL.sf.os.ossrv", "lowlevellibsandfws", "apputils",
                              "stringtools", "stringtable.pl")
JOBS = int(os.environ.get("MMP_JOBS", "8"))
# EXEs without EPOCSTACKSIZE: the Symbian default is 0x2000, sized for RVCT Thumb frames. Our GCC
# ARM-mode -O2 frames are larger (the stock Calculator, with no EPOCSTACKSIZE, overflowed 8K during
# UI construction). 0x5000 = what S60 apps that do set it typically use.
DEFAULT_STACK = 0x5000
CXXFILT = "arm-none-eabi-c++filt"
AR = "arm-none-eabi-ar"


def _rel_build(p: str) -> str:
    """A path under BUILD, relative to it with forward slashes (the form .romfiles lines carry)."""
    return os.path.relpath(p, BUILD).replace("\\", "/")


def _toolchain_rev() -> str:
    """Compiler-front-end revision: symcc, the prefix header and the include overlay
    change codegen for EVERY TU, so they are part of each object's cache key."""
    tc = os.path.join(PDK, "tools", "toolchain")
    files = [os.path.join(tc, "symcc"), os.path.join(tc, "symbian_gcc_prefix.h")]
    files += sorted(glob.glob(os.path.join(OS_ROOT, "toolchain", "overlay", "*")))
    h = hashlib.sha1()
    for f in files:
        h.update(open(f, "rb").read())
    return h.hexdigest()


TOOLCHAIN_REV = None


def read_deps(obj):
    """Paths from <obj>.d (make syntax: backslash-newline continuations, backslash-space = a space
    inside a path)."""
    try:
        text = open(obj + ".d", errors="replace").read()
    except OSError:
        return None
    text = text.replace("\\\r\n", " ").replace("\\\n", " ")
    body = text.split(": ", 1)[1] if ": " in text else ""
    return [d.strip().replace("\t", " ") for d in body.replace("\\ ", "\t").split(" ") if d.strip()]


def _deps_newer(obj):
    """True if a recorded dependency is newer than the object (no .d yet = unknown = not newer:
    objects built before -MMD keep their old behaviour until their next rebuild)."""
    deps = read_deps(obj)
    if not deps:
        return False
    t = os.path.getmtime(obj)
    for d in deps:
        try:
            if os.path.getmtime(d) > t:
                return True
        except OSError:
            return True          # a header it used is gone
    return False


_SYMCC_CMDS = {}
_SYMCC_LOCK = threading.Lock()


def _symcc_cmd(src, flags, env):
    """symcc's compiler command for this source kind + environment (it depends on the extension and SYMCC_*/SYMBIAN_GXX)."""
    kind = os.path.splitext(src)[1].lower() == ".cia"
    key = (kind, env.get("SYMCC_ARM"), env.get("SYMBIAN_GXX"), env.get("SYMCC_PREINC"), env.get("SYMCC_NKERN"))
    with _SYMCC_LOCK:
        if key not in _SYMCC_CMDS:
            out = subprocess.run(["bash", BE.SYMCC, "x.cia" if kind else "x.cpp"], capture_output=True,
                                 env=dict(env, SYMCC_PRINT="1"), cwd=PDK, check=True).stdout
            _SYMCC_CMDS[key] = [a for a in out.decode().split("\0") if a]
        return _SYMCC_CMDS[key]


def _compile_one(src, obj, flags, env):
    global TOOLCHAIN_REV
    if TOOLCHAIN_REV is None:
        TOOLCHAIN_REV = _toolchain_rev()
    key = hashlib.sha1((TOOLCHAIN_REV + " ".join(flags) + src).encode()).hexdigest()
    stamp = obj + ".key"
    if (os.path.exists(obj) and os.path.exists(stamp) and open(stamp).read() == key
            and os.path.getmtime(obj) >= os.path.getmtime(src) and not _deps_newer(obj)):
        return src, 0, ""
    if "-DSYMCC_C_DRIVER" in flags:
        # a C source of a `cdriver` component: g++ compiles even a .c as C++, so hand symcc the
        # gcc driver (its SYMBIAN_GXX override)
        env = dict(env, SYMBIAN_GXX=BE.GCC)
    # -MMD: <obj>.d = every header this TU read. Feeds _deps_newer (a header-tree change, e.g. a
    # branch move, recompiles its dependents) and tools/closure_export.py (the source deliverable).
    # g++ directly with symcc's command line (SYMCC_PRINT, cached per flag set): no shell start per TU
    if os.environ.get("MMP_DUMPCMD") == os.path.basename(src):      # debug: the full compile command of one TU
        with open(os.path.join(ARM_ROOT, "mmp-cmd.txt"), "w") as fh:
            fh.write("\n".join([*_symcc_cmd(src, flags, env), src, "-O2", "-w", *flags, "-o", obj]))
    r = subprocess.run([*_symcc_cmd(src, flags, env), src, "-O2", "-w", *flags, "-MMD", "-MF", obj + ".d", "-o", obj],
                       capture_output=True, text=True, errors="replace", env=env, cwd=PDK)
    if r.returncode == 0:
        open(stamp, "w").write(key)
    return src, r.returncode, (r.stdout + r.stderr)


_SUPCXX_TRIMMED = None      # build(): this component's libsupc++ with `supcxx_drop` members removed


def _libsupcxx() -> str:
    if _SUPCXX_TRIMMED:
        return _SUPCXX_TRIMMED
    return subprocess.run([BE.GCC, "-marm", "-march=armv5te", "-print-file-name=libsupc++.a"],
                          check=True, capture_output=True, text=True).stdout.strip()


# `device` images (RAM-loaded images for a stock Symbian loader): the target's drtaeabi.dll carries the EHABI unwinder,
# the __aeabi personalities and the whole C++ ABI runtime (__cxa_throw / begin_catch / end_catch ...) -- what its own
# apps import. What it lacks is GCC's personality routine, which g++ names in every catch / cleanup frame: that one object comes from GCC's libsupc++
# (EHABI makes the pairing legal: the unwinder calls it through the frame's index entry, it talks to the unwinder
# only through the _Unwind_VRS_* / __gnu_unwind_frame interface and to the runtime through __cxa_*).
DEVICE_RUNTIME_LIBS = ["drtaeabi.lib", "dfpaeabi.lib", "scppnwdl.lib"]
# the personality + its frame decoder (pr-support: __gnu_unwind_frame, _Unwind_GetLanguageSpecificData ... written
# against the _Unwind_VRS_* API only). Both go into pass 1 so what THEY need is imported from drtaeabi too -- left to
# the final link, libgcc would pull its own unwinder (unwind-arm.o) for _Unwind_VRS_Pop: two unwinders in one
# process. std::terminate = a panic (toolchain/dll_rt/device_rt.cpp), not libsupc++'s handler machinery.
DEVICE_MEMBERS = [("supcxx", "eh_personality.o"), ("gcc", "pr-support.o")]


def static_archive(key: str) -> str:
    return os.path.join(OUT_ROOT, key.lower(), key.lower() + ".a")


def _device_rt_objs(work: str) -> list[str]:
    ar = AR
    out = os.path.join(work, "device_rt")
    os.makedirs(out, exist_ok=True)
    for lib, member in DEVICE_MEMBERS:
        subprocess.run([ar, "x", _libsupcxx() if lib == "supcxx" else BE._libgcc(), member], cwd=out, check=True)
    return [os.path.join(out, member) for _, member in DEVICE_MEMBERS]


DATA_EXPORTS: set[str] = set()      # every DATA-flagged export seen in a provider .def


# LIBRARY names the release uses but never builds -> the DLL that provides what their users import (as
# build_mmp_x32.py). aknlayout.lib: its users' only import is CdlEngine::GetFunction (cdlengine.dll).
LIB_ALIASES = {"aknlayout": "cdlengine", "mpm": "mpmclient"}
if os.environ.get("PHOREPOC_QT48") == "1":      # Qt 4.8.7: qtcore.lib -> the qt48_core component ...
    LIB_ALIASES.update({m: "qt48_" + m[2:] for m in ("qtcore", "qtgui", "qtnetwork", "qtsvg", "qtsql", "qtxml", "qtscript", "qtdeclarative", "qtmultimediakit")})
    LIB_ALIASES["qtmain"] = "qt48_main"     # the STATICLIBRARY every Qt program links (E32Main -> main())


def _sf_layout(comp, m) -> list[str]:
    """`sf_layout` (as build_mmp_x32.py): the component quotes a SIBLING package by a relative path written for the
    original sf/os/<package> layout (networkingsrv: `#include "../../../../commsfw/..."`). Mirror it: the component's
    own source dirs as REAL directories under .mmp-build/_sf/os/<pkg>, every other oss.FCL.sf.os.* package beside
    them as a symlink, each own dir on -iquote."""
    if not comp.get("sf_layout"):
        return []
    rel = os.path.relpath(m.path, MMP.BASE).replace("\\", "/")
    own = rel.split("/")[0]
    osroot = os.path.join(OUT_ROOT, "_sf", "os")      # (absolute symlinks / real dirs under BUILD, never /tmp)
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


def gcc_mangled(name):
    """RVCT mangles an extern "C" function type with a Y (Itanium <function-type> ::= F [Y] ...); GCC never writes it.
    A frozen .def name with "FY" is therefore the GCC symbol with "F" (Qt's qHBNewFace(void*, HB_GetFontTableFunc):
    QtCore never exported it and QtGui's call hit the unimpl stub -- EUSRNIMP in QFontEngine::harfbuzzFace)."""
    return name.replace("FY", "F") if name.startswith("_Z") else name


def provider_maps(libs, device=False):
    """name -> (rom dll, ordinal), honouring the mmp LIBRARY list; euser wins.
    device: a stock euser has no TTrap spare ordinals (they are OUR euser's; there those
    ordinals are other functions) -- only the release .def."""
    resolve = {}
    order = []
    for lib in libs:
        stem = lib.rsplit(".", 1)[0]
        stem = LIB_ALIASES.get(stem.lower(), stem)
        if any(o[0] == stem for o in order):
            continue        # an alias of a LIBRARY already listed (aknlayout + cdlengine): a 2nd entry shifts the import table
        if stem in C.BUILTIN:
            rom, defp = C.BUILTIN[stem]
        elif stem in C.COMPONENTS:
            m = MMP.parse(os.path.join(MMP.BASE, C.COMPONENTS[stem]["mmp"]))
            rom = C.rom_name(stem, m.targettype)
            defp = (os.path.join(MMP.BASE, C.COMPONENTS[stem]["def"])
                    if "def" in C.COMPONENTS[stem] else m.def_path())
        else:
            print(f"  ! LIBRARY {lib}: no provider registered (its imports -> stubs)")
            continue
        if not defp:
            print(f"  ! LIBRARY {lib}: no .def found")
            continue
        order.append((stem, rom, defp))
    for stem, rom, defp in sorted(order, key=lambda t: t[0] == "euser"):   # euser last = wins
        d = parse_def(defp)
        for ordn, name in d.by_ordinal.items():
            if name not in d.absent:                # an ABSENT slot provides nothing
                resolve[name] = (rom, ordn)
                if gcc_mangled(name) != name:       # the importer's GCC-mangled spelling of the same function
                    resolve.setdefault(gcc_mangled(name), (rom, ordn))
        DATA_EXPORTS.update(d.data_names)
        if stem == "euser" and not device:
            for name in BD.EXTRA_EXPORTS:
                resolve[name] = (rom, BD.extra_export_ordinal(name, d.max_ordinal))
    return resolve, [rom for _, rom, _ in order]


def _is_registration(m, block) -> bool:
    # the registration is `<target stem>.rss`, or (SYMBIAN_SECURE_ECOM idiom) `<dll uid3>.rss`
    # with `TARGET <target stem>.rsc` inside the block
    stem = os.path.splitext(m.target)[0].lower()
    if m.targettype != "plugin":
        return False
    by_target = [b for b in m.resource_blocks if os.path.splitext(b.get("target") or "")[0].lower() == stem]
    if len(by_target) > 1:
        # two blocks with one TARGET (wrtharvester: 2001FE2C.rss and wrtharvester.rss, both `TARGET
        # wrtharvester.rsc`): the one with a TARGETPATH / HEADER is an ordinary resource
        by_target = [b for b in by_target if not b.get("targetpath") and not b.get("header")] or by_target
    if by_target:
        # an explicit `TARGET <stem>.rsc` names THE registration; a same-named .rss beside it is an ordinary
        # resource (WidgetInstallerUI: 1028281F.rss -> WidgetInstallerUI.rsc is the registration, and
        # WidgetInstallerUI.rss has a HEADER -- it was compiled over the registration, no .rsg)
        return block in by_target
    return stem == os.path.splitext(os.path.basename(block["rss"]))[0].lower()


def build_resources(key: str, m, rom: str) -> int:
    """START RESOURCE / RESOURCE -> the REAL rcomp (tools/rsc.py): .rsg into
    BUILD/epoc32/include, .rsc into BUILD/rsc/<targetpath>/, and a ROM manifest
    BUILD/romfiles/<rom>.romfiles (`<rom path>=<host path rel. to BUILD>` lines) the ROM script adds."""
    # (a plug-in's ECOM registration <target stem>.rss is handled with the image itself)
    blocks = [b for b in m.resource_blocks if not _is_registration(m, b)]
    if not blocks:
        return 0
    sys.path.insert(0, os.path.join(PDK, "tools"))
    import rsc
    lines, bad = [], 0
    for b in blocks:
        stem = os.path.splitext(b["target"] or os.path.basename(b["rss"]))[0].lower()
        tp = (b["targetpath"] or "resource/apps").replace("\\", "/").strip("/")
        # rsc_out: a separate output root (under BUILD) for a second build of the same resources (e.g. the
        # Symbian^4 SSM lists beside the S^3 ones from RCL_3 -- same file names, different content)
        out = os.path.join(BUILD, C.COMPONENTS[key].get("rsc_out", "rsc"), *tp.split("/"), stem + ".rsc")
        rsg = os.path.join(MMP.EPOC32, "include", stem + ".rsg") if b["header"] else None
        # rss_delta: {<rss basename, lower case>: <OS_ROOT-relative path>} -- a board copy of one resource source
        # (e.g. a product list); the original's directory stays on the include path
        rss = b["rss"]
        delta = C.COMPONENTS[key].get("rss_delta", {}).get(os.path.basename(rss).lower())
        extra_inc = []
        if delta:
            extra_inc = [os.path.dirname(rss)]
            rss = rpath(delta)
            print(f"  resource delta: {os.path.basename(b['rss'])} <- {delta}")
        # rsc_incs FIRST: they override stale epoc32 exports (RCL_3 phoneappcommands.hrh vs master's)
        ok, log = rsc.compile_rss(rss, out, rsg, incs=[rpath(x) for x in C.COMPONENTS[key].get("rsc_incs", [])] + extra_inc + m.userinc + m.sysinc + [CASEFIX], uids=b["uids"],
                                   defs=C.COMPONENTS[key].get("rdefs", ()), variant_of=b["rss"])
        if ok:
            lines.append(tp.replace("/", "\\") + "\\" + stem + ".rsc=" + _rel_build(out))
            # no TARGETPATH: the .iby decides where it lands. A plug-in's extra resources conventionally go
            # next to its registration (ECOM_RESOURCE_DIR: the notes search plug-in opens
            # \resource\plugins\notespluginsresource.rsc) -- stage it there as well
            if not b["targetpath"] and m.targettype == "plugin":
                lines.append("resource\\plugins\\" + stem + ".rsc=" + _rel_build(out))
        else:
            bad += 1
            print(f"  RSC FAIL {os.path.basename(b['rss'])}: " + " | ".join(log.strip().splitlines()[-3:])[:400])
    man = os.path.join(ROMFILES_DIR, rom + ".romfiles")
    os.makedirs(ROMFILES_DIR, exist_ok=True)
    with open(man, "w", newline="\n") as fh:
        fh.write("\n".join(lines) + "\n")
    print(f"  resources: {len(lines)}/{len(blocks)} compiled (rcomp)")
    return bad


def compile_registration(key: str, m, rom: str) -> None:
    """A plug-in's ECOM registration resource -> BUILD/plugins/<rom stem>.rsc (z:/resource/plugins/).
    Arch-neutral: build_mmp_x32.py's ROMs (x32_rom_args.py) use the same file."""
    if m.targettype == "plugin":
        sys.path.insert(0, os.path.join(PDK, "tools"))
        import ecom_rsc
        import rsc as RSC
        for rss in [b["rss"] for b in m.resource_blocks if _is_registration(m, b)]:
            rsc = os.path.join(PLUGINS_DIR, os.path.splitext(rom)[0] + ".rsc")
            os.makedirs(os.path.dirname(rsc), exist_ok=True)
            # the REAL rcomp when built (handles any registration, e.g. CDL's opaque
            # data); tools/ecom_rsc.py (REGISTRY_INFO only) as the no-host-tools fallback
            ok = False
            if os.path.isfile(RSC.RCOMP):
                ok, log = RSC.compile_rss(rss, rsc, None, incs=[rpath(x) for x in C.COMPONENTS[key].get("rsc_incs", [])] + m.userinc + m.sysinc + [CASEFIX])
                if ok:
                    print(f"  registration: {os.path.basename(rss)} -> "
                          f"{_rel_build(rsc)} (rcomp, {os.path.getsize(rsc)} B)")
                else:
                    print("  ! rcomp failed on registration: " + log.strip()[-300:])
            if not ok:
                reg, n_impl, size = ecom_rsc.compile_rss(rss, rsc)
                print(f"  registration: {os.path.basename(rss)} -> {_rel_build(rsc)} "
                      f"({n_impl} implementations, {size} B)")


def export_stringtables(m) -> None:
    """START STRINGTABLE x.st / EXPORTPATH /epoc32/include (http.mmp: HttpStringConstants.st): the build
    GENERATES x.h with the real stringtable.pl and exports it -- other components #include it
    (drmhelper -> <http.h> -> <httpstringconstants.h>). The .cpp half is compiled by the image build."""
    if not m.stringtables:
        return
    import tempfile
    pl = os.path.join(MMP.BASE, "oss.FCL.sf.os.ossrv", "lowlevellibsandfws", "apputils", "stringtools",
                      "stringtable.pl")
    for st in m.stringtables:
        with tempfile.TemporaryDirectory() as gen:
            local = os.path.join(gen, os.path.basename(st))
            shutil.copyfile(st, local)
            r = subprocess.run(["perl", pl, "./" + os.path.basename(local)], cwd=gen,
                               capture_output=True, text=True)
            hdr = os.path.join(gen, os.path.splitext(os.path.basename(st))[0].lower() + ".h")
            hdrs = [f for f in os.listdir(gen) if f.lower().endswith(".h")]
            if r.returncode or not hdrs:
                print(f"  ! stringtable {os.path.basename(st)}: {r.stderr.strip()[-200:]}")
                continue
            for h in hdrs:
                shutil.copyfile(os.path.join(gen, h), os.path.join(MMP.EPOC32, "include", h.lower()))
            print(f"  stringtable: {os.path.basename(st)} -> epoc32/include/{', '.join(h.lower() for h in hdrs)}")


def build(key: str, rsc_only: bool = False) -> int:
    comp = C.COMPONENTS[key]
    m = MMP.parse(os.path.join(MMP.BASE, comp["mmp"]))
    rom = C.rom_name(key, m.targettype)
    if rsc_only or comp.get("rsc_only"):
        # resources are arch-neutral; a component that is real only on x32 (x32_real + x32_mmp, e.g. the
        # RCL_3 drmhelper where ARM keeps a null shim) takes its resources from the real mmp
        if comp.get("x32_real") and comp.get("x32_mmp"):
            m = MMP.parse(os.path.join(MMP.BASE, comp["x32_mmp"]))
        r = build_resources(key, m, rom)
        compile_registration(key, m, rom)
        export_stringtables(m)
        return r
    build_resources(key, m, rom)
    work = os.path.join(OUT_ROOT, key)
    os.makedirs(work, exist_ok=True)
    # `supcxx_drop` (drtaeabi): fnmatch patterns of libsupc++ members that must not come in -- operator new/delete are
    # scppnwdl.dll's (an import), not libsupc++'s malloc-based ones
    global _SUPCXX_TRIMMED
    _SUPCXX_TRIMMED = None
    if comp.get("supcxx_drop"):
        import fnmatch
        full = subprocess.run([BE.GCC, "-marm", "-march=armv5te", "-print-file-name=libsupc++.a"], check=True,
                              capture_output=True, text=True).stdout.strip()
        ar = AR
        trimmed = os.path.join(work, "libsupc++_trim.a")
        shutil.copyfile(full, trimmed)
        members = subprocess.run([ar, "t", trimmed], check=True, capture_output=True, text=True).stdout.split()
        drop = [x for x in members if any(fnmatch.fnmatch(x, pat) for pat in comp["supcxx_drop"])]
        if drop:
            subprocess.run([ar, "d", trimmed, *drop], check=True)
        _SUPCXX_TRIMMED = trimmed
    print(f"=== {key}: {m.target} ({m.targettype}) -> {rom}   [{len(m.sources)} sources]")

    # USERINCLUDE wins quoted lookups (abld order): -iquote precedes every -I,
    # incl. symcc's own kernel dirs (estor's "U32STD.H" vs the kernel <u32std.h>).
    # incs_first (as build_mmp_x32.py): board copies of headers that must win over the mmp's own dirs (the STLport compiler
    # configuration, components.STLPORT_X32) -- searched before everything else, -iquote too
    # (opt-in, `arm_incs_first`: the registry's incs_first were written for x32 and stay x32-only)
    first = [x for p in (comp.get("incs_first", []) if comp.get("arm_incs_first") else [])
             for x in ("-iquote" + rpath(p), "-I" + rpath(p))]
    incs = first + [x for p in m.userinc for x in ("-iquote", p)] + _sf_layout(comp, m)
    # `noinc`: drop include dirs containing any of these substrings (e.g. P.I.P.S. "stdapis"
    # when the component is pointed at estlib's libc headers instead)
    noinc = comp.get("noinc", [])
    incs += [f"-I{p}" for p in m.userinc + m.sysinc
             if not any(n in p.replace("\\", "/").lower() for n in noinc)]
    # -fno-threadsafe-statics: RVCT/GCCE-era Symbian has no __cxa_guard_* runtime
    flags = (["-fno-access-control", "-fno-threadsafe-statics", "-include", MMP.variant_for(m.path),
              # e32def.h sets this for the Symbian GCC-ARM config (__GCC32__, which
              # our prefix deliberately leaves off): class-typed header consts become
              # macros instead of per-TU dynamic-init globals (= DLL WSD, estor).
              "-D__NO_CLASS_CONSTS__",
              "-D__DLL__" if not m.is_exe else "-D__EXE__"]
             + prefix_maps()      # __FILE__ (KPanicFileName ...) independent of the checkout and mount points
             + [f"-D{x}" for x in m.macros + comp.get("defs", [])]
             # X32_DEFS="<key>:<MACRO>[,...]" -- opt-in diagnostic defines, as build_mmp_x32.py
             + [f"-D{d.split(':', 1)[1]}" for d in os.environ.get("X32_DEFS", "").split(",")
                if ":" in d and d.split(":", 1)[0] == key]
             + incs + ["-idirafter", CASEFIX, "-idirafter", SYMBIAN]   # case-insensitive #include fallback (tools/casefix.py)
             + comp.get("flags", [])
             # 2010-era C++ (`nullptr` as an identifier: etelmm, cntmodel), as build_mmp_x32.py compiles everything
             + ["-std=gnu++03"]
             # RVCT-only __declspec(notshared/...) under __EABI__ (cntmodel/cntsrv NONSHARED): no meaning to GCC,
             # whose import/export is already blanked (IMPORT_C/EXPORT_C in the prefix header)
             + ["-D__declspec(x)="]
             # _FOFF = __builtin_offsetof, a constant expression (as the x32 port_prelude.h): the prefix's
             # pointer-arithmetic form is for euser's runtime-index HBufC allocation, never built here
             # ... except the components marked PORT_FOFF_DYNAMIC (a run-time index: estor, edbms, eikcore)
             + ([] if "-DPORT_FOFF_DYNAMIC" in comp.get("x32_flags", [])
                else ["-include", os.path.join(OS_ROOT, "toolchain", "shims", "foff_builtin.h")])
             # vfp=True + ARM_VFP=1: hardware floating point, soft-float ABI (links with the rest of the ROM);
             # the kernel must switch VFP state (__CPU_HAS_VFP: the SMP kernel)
             + (["-mfpu=vfp", "-mfloat-abi=softfp"] if comp.get("vfp") and os.environ.get("ARM_VFP") == "1" else []))
    # The C-library <stdarg.h> (stdapis = P.I.P.S., libc = estlib) re-#defines va_start/va_arg
    # as `char*` stack walking -- wrong for the ARM EABI (first varargs live in r0-r3). The
    # real RVCT/GCCE builds found the COMPILER's stdarg.h first; mirror that: pre-set the two
    # header guards so the prefix header's __builtin_va_list definitions stand.
    if any(p.replace("\\", "/").lower().rstrip("/").endswith(("/stdapis", "include/libc"))
           for p in m.userinc + m.sysinc) or comp.get("noinc"):
        flags += ["-D_STDARG_H", "-D_STDARG_E_H", "-D__e32_va_list=va_list"]
    # RTTI: S60 (mw/app layer) code uses dynamic_cast, and any object that can reach one
    # needs a typeinfo slot in its vtable -> on for the mw/app layers + opt-in (`rtti`).
    rtti = comp.get("rtti", MMP.variant_for(m.path) == MMP.S60_VARIANT_HRH)
    if rtti:
        flags.append("-frtti")
    # `device`: a stock-loader image leaves by C++ throw (XLeaveException) and TRAPs by catch -- the release's own
    # e32cmn.h model under these two macros (symcc's -fno-exceptions comes first; this later one wins;
    # the toolchain/overlay setjmp TRAP steps aside under __LEAVE_EQUALS_THROW__)
    device = comp.get("device", False)
    # `eh_dll` (compsupp: drtaeabi): a runtime DLL of our ROM that itself uses C++ exceptions (cppsemantics.cpp's
    # try/catch in __cxa_end_catch / __cxa_call_unexpected) -- the device image's exception machinery (tables kept, the
    # descriptor, GCC's personality) without the stock-loader image format of `device`
    ehdll = comp.get("eh_dll", False)
    if device or ehdll:
        flags += ["-fexceptions", "-D__SUPPORT_CPP_EXCEPTIONS__", "-D__LEAVE_EQUALS_THROW__"]
    env = dict(os.environ, SYMCC_ARM="1")
    # x32_real without x32_mmp (c32): the REAL component from the same mmp, as the x32 build does
    real = comp.get("x32_real") and not comp.get("x32_mmp")
    drop = {d.lower() for d in ([] if real else comp.get("drop", []))}   # (lower-case, as build_mmp_x32)
    srcs = [] if comp.get("nosrc") and not real else [s for s in m.sources if os.path.basename(s).lower() not in drop]
    srcs += [rpath(e) for e in ([] if real else comp.get("extra", []))]
    # `own_newdel`: the component IS the operator new/delete provider (scppnwdl.dll) -- gcchelp.cpp's would collide
    # `own_crt`: the component carries its own __cxa_pure_virtual / sized delete leaves (drtaeabi)
    rt = [*([] if comp.get("own_newdel") else [GCCHELP]), *([] if comp.get("own_crt") else [os.path.join(RT, "dll_crt.cpp")]),
          os.path.join(BE.ED, "unimpl_export.cpp")]
    if not m.is_exe:
        # edllstub.lib (every DLL links it): the REAL Dll::Tls/SetTls/FreeTls + Dll::FileName
        rt += [os.path.join(BE.B, "euser", "epoc", "up_dll_tls.cpp"),
               os.path.join(BE.B, "euser", "epoc", "up_dll_file.cpp")]
    if rtti and not comp.get("own_rtti_rt"):       # (own_rtti_rt: the component defines __cxa_bad_cast/typeid itself)
        rt += [os.path.join(RT, "rtti_rt.cpp")]
    if comp.get("device") and not ehdll:
        rt += [os.path.join(RT, "device_rt.cpp")]
    if device or ehdll:
        rt += [os.path.join(RT, "device_excdesc.s")]
    if m.is_exe:
        rt += [os.path.join(RT, "exe_dso_handle.cpp")]
        rt += [os.path.join(BE.B, "euser", "epoc", "arm", "uc_exe.cia"),
               os.path.join(EE, "dev_exe_startup.cpp")]

    # START STRINGTABLE x.st: the REAL generator (apputils/stringtools/stringtable.pl) -> x.cpp
    # (one more SOURCE) + x.h, exported to epoc32/include exactly as abld does (clients use it)
    for st in m.stringtables:
        gen = os.path.join(work, "stringtable")
        os.makedirs(gen, exist_ok=True)
        local = os.path.join(gen, os.path.basename(st))
        # (the tool lower-cases its output names, and only parses an argument that has a directory part)
        stem = os.path.join(gen, os.path.splitext(os.path.basename(st))[0].lower())
        if not os.path.isfile(stem + ".cpp") or os.path.getmtime(stem + ".cpp") < os.path.getmtime(st):
            shutil.copyfile(st, local)
            r = subprocess.run(["perl", STRINGTABLE_PL, "./" + os.path.basename(local)], cwd=gen,
                               capture_output=True, text=True)
            if r.returncode or not os.path.isfile(stem + ".cpp"):
                print(f"  STRINGTABLE FAIL {os.path.basename(st)}: {(r.stdout + r.stderr).strip()[-300:]}")
                return 1
            shutil.copyfile(stem + ".h", os.path.join(MMP.EPOC32, "include", os.path.basename(stem) + ".h"))
        srcs.append(stem + ".cpp")
        flags = flags + [f"-I{gen}"]
    jobs, objs = [], []
    for i, s in enumerate(srcs):
        obj = os.path.join(work, f"{i:03d}_{os.path.basename(s)}.o")
        if s.endswith(".CIA"):
            # symcc keys its ARM naked-asm mode on a lower-case .cia; an upper-case .CIA reaches g++
            # as an unknown extension = "linker input", silently compiles NOTHING (cstdlib JMP_MARM)
            wrap = os.path.join(work, f"{i:03d}_{os.path.basename(s)[:-4].lower()}_ascia.cia")
            text = '#include "%s"\n' % s.replace("\\", "/")
            if not os.path.isfile(wrap) or open(wrap).read() != text:
                with open(wrap, "w") as fh:
                    fh.write(text)
            s = wrap
        if s.endswith(".C"):
            # g++ takes an UPPER-case .C for C++ (abld, on a case-blind host, built it as C:
            # cstdlib's FINDFP.C has a function called `std`). Compile a lower-case .c that
            # just #includes it -- quoted includes still resolve next to the real file.
            wrap = os.path.join(work, f"{i:03d}_{os.path.basename(s)[:-2].lower()}_asc.c")
            text = '#include "%s"\n' % s.replace("\\", "/")
            if not os.path.isfile(wrap) or open(wrap).read() != text:
                with open(wrap, "w") as fh:
                    fh.write(text)
            s = wrap
        if comp.get("cdriver") and s.lower().endswith(".c"):
            jobs.append((s, obj, [f for f in flags if f not in ("-frtti", "-fno-access-control",
                                                                "-fno-threadsafe-statics", "-std=gnu++03")]
                         # C has no built-in wchar_t and the (C++-minded) prefix header typedefs __TText from
                         # it; cstdlib's own stddef.h typedef is switched off to match (-fshort-wchar)
                         + ["-DSYMCC_C_DRIVER", "-Dwchar_t=unsigned short", "-D__wchar_t_defined"]))
            objs.append(obj)
            continue
        jobs.append((s, obj, flags))
        objs.append(obj)
    for s in rt:
        obj = os.path.join(work, "rt_" + os.path.basename(s) + ".o")
        # a `device` EXE's thread entry sets up the runtime's per-thread exception state (dev_exe_startup.cpp:
        # TCppRTExceptionsGlobals) -- only under the device's leave-by-throw model
        eh = (["-fexceptions", "-D__SUPPORT_CPP_EXCEPTIONS__", "-D__LEAVE_EQUALS_THROW__"]
              if device and os.path.basename(s) == "dev_exe_startup.cpp" else [])
        jobs.append((s, obj, eh))
        objs.append(obj)
    failed = []
    with cf.ThreadPoolExecutor(JOBS) as ex:
        for src, rc, out in ex.map(lambda j: _compile_one(*j, env), jobs):
            if rc != 0:
                failed.append((src, out))
    if failed:
        seen = {}
        for src, out in failed:
            errs = [l for l in out.splitlines() if "error" in l or "fatal" in l or (os.environ.get("MMP_INCCHAIN") and "included from" in l)][:int(os.environ.get("MMP_ERRLINES", "4"))]
            sig = "|".join(e.split(": ", 1)[-1] for e in errs[:1])
            seen.setdefault(sig, []).append((src, errs))
        for sig, group in seen.items():           # one report per distinct first error
            src, errs = group[0]
            more = f"  (+{len(group) - 1} more TUs, same first error)" if len(group) > 1 else ""
            print(f"  COMPILE FAIL {os.path.basename(src)}{more}")
            for l in errs:
                print("      " + l[-200:])
        print(f"  {len(failed)}/{len(jobs)} TUs failed")
        return 1

    if m.targettype == "lib":
        # STATICLIBRARY: an ar archive of its own objects (no runtime), which a
        # consumer's link takes from its mmp's STATICLIBRARY list (static_archive())
        a = static_archive(key)
        if os.path.exists(a):
            os.remove(a)
        own = [o for o in objs if not os.path.basename(o).startswith("rt_")]
        subprocess.run([AR, "rcs", a, *own], check=True)
        print(f"  wrote {_rel_build(a)} ({len(own)} objects)")
        return 0

    if not m.is_exe:
        entry_o = os.path.join(work, "dll_entry.s.o")
        subprocess.run([BE.GCC, "-march=armv5te", "-marm", "-c",
                        f"-DDLL_UID3=0x{(m.uids[1] if len(m.uids) > 1 else 0):08x}",
                        # a DLL allowed writable data (EPOCALLOWDLLDATA / allow_wsd) gets its static
                        # constructors run too -- dscstore's KSql*Length constants are initialised by one
                        *(["-DDLL_STATIC_CTORS"] if comp.get("static_ctors") or comp.get("allow_wsd") or m.allowdlldata
                          else []),
                        os.path.join(RT, "dll_entry.S"), "-o", entry_o], check=True)
        objs.insert(0, entry_o)

    defp = (os.path.join(MMP.BASE, comp["def"]) if "def" in comp else m.def_path())
    if m.targettype == "plugin" and "def" not in comp and not m.deffile:
        # no DEFFILE statement: the default-named .def next door can be a stale pre-ECOM
        # leftover (AvKon/EABI/AKNINITU.DEF freezes CAknDllInitializer::NewL) -- the
        # toolchain never reads it for a plug-in, whose export table is fixed.
        defp = None
    defmap = parse_def(defp) if defp else None
    if m.targettype == "plugin" and not defmap:
        # ECOM plug-in: abld freezes exactly one export, the proxy table accessor
        from euser_def import DefMap
        defmap = DefMap({1: "_Z24ImplementationGroupProxyRi"}, 1)
    if m.targettype == "ani" and not defmap:
        # WSERV animation plug-in: one frozen export, the CAnimDll factory
        from euser_def import DefMap
        defmap = DefMap({1: "_Z15CreateCAnimDllLv"}, 1)
    if m.targettype == "fsy" and not defmap:
        # file system plug-in: no .def, ONE export (ordinal 1) = extern "C" CreateFileSystem (EFILE's Lookup(1);
        # sl_main.cpp) -- as build_elocal_fsy.py
        from euser_def import DefMap
        defmap = DefMap({1: "CreateFileSystem"}, 1)
    if not m.is_exe and not defmap:
        print("  ! no .def found -- DLL would export nothing"); return 1
    defined = BE._nm("--defined-only", *objs)
    # exports a prebuilt archive provides (Mesa's EGL/GLES entry points) -- the link roots
    # them with -u, so their members come in
    for a in comp.get("arm_archives", []):
        if os.path.isfile(rpath(a)):
            defined |= BE._nm("--defined-only", rpath(a))
    # `gcc_exports` (compsupp: dfpaeabi/drtaeabi): the generic exports come from GCC's own libgcc / libsupc++ where
    # the release linked RVCT's -- rooted with -u like an object's, so the archive member comes in
    if comp.get("gcc_exports"):
        defined |= BE._nm("--defined-only", *([_libsupcxx()] if comp.get("rtti", True) else []), BE._libgcc())
    exportable = sorted(set(defmap.by_ordinal.values()) & defined) if defmap else []
    # .def names in RVCT's spelling of an extern "C" function type: alias them to GCC's symbol (--defsym below)
    y_alias = {n: gcc_mangled(n) for n in (defmap.by_ordinal.values() if defmap else [])
               if n not in defined and gcc_mangled(n) != n and gcc_mangled(n) in defined}
    exportable = sorted(set(exportable) | set(y_alias))
    entry = "_E32Startup" if m.is_exe else "_E32Dll"
    roots = [n for n in exportable if n not in y_alias] + sorted(set(y_alias.values())) + [STUB, entry]
    ld = os.path.join(HERE, ("efile" if m.is_exe else "mmp_dll") + ("_device" if device or ehdll else "") + ".ld")

    def link(all_objs, out, ignore, archives=None):
        rsp = os.path.join(work, "link.rsp")
        with open(rsp, "w") as fh:
            fh.write("\n".join(f"-Wl,-u,{n}" for n in roots) + "\n")
            fh.write("".join(f"-Wl,--defsym,{a}={g}\n" for a, g in y_alias.items()))
            fh.write("\n".join('"' + o.replace("\\", "/") + '"' for o in all_objs))
        if archives is None:
            archives = [*([_libsupcxx()] if rtti else []), BE._libgcc()]
        # `arm_archives` (device): prebuilt static archives of this component, in
        # BOTH passes -- pass 1 has to see what their members import. One group: they reference each other.
        arm_ar = [rpath(a) for a in comp.get("arm_archives", [])]
        # the mmp's STATICLIBRARY list: registered `lib` components (build them first)
        for sl in m.staticlibs:
            sl = LIB_ALIASES.get(sl.rsplit(".", 1)[0], sl.rsplit(".", 1)[0]) + ".lib"   # qtmain.lib -> qt48_main (PHOREPOC_QT48)
            a = static_archive(sl.rsplit(".", 1)[0])
            if device and os.path.isfile(static_archive(sl.rsplit(".", 1)[0] + "_dev")):
                a = static_archive(sl.rsplit(".", 1)[0] + "_dev")   # its EH (Leave = throw) twin: `<lib>_dev` component
            if os.path.isfile(a):
                arm_ar.append(a)
            elif sl.rsplit(".", 1)[0] in C.COMPONENTS:
                print(f"  ! STATICLIBRARY {sl}: {_rel_build(a)} not built")
        if arm_ar:
            archives = ["-Wl,--start-group", *arm_ar, "-Wl,--end-group", *archives]
        args = [BE.GCC, "-nostdlib", "-Wl,-q", "-Wl,--gc-sections", "-Wl,--no-demangle",
                f"-Wl,-T,{ld}", "-march=armv5te", "-marm", f"@{rsp}", *archives, "-o", out]
        if device or ehdll:
            args.insert(1, f"-Wl,-Map={out}.map")   # which archive member came in, and for whom
            # keep the personality in BOTH passes: an image without a catch of its own would otherwise lose it to
            # --gc-sections in pass 1 (so its runtime names are never imported from drtaeabi), yet the final link
            # still needs it and then drags in libsupc++'s whole throw/alloc side (malloc, getenv ...)
            args.insert(1, "-Wl,-u,__gxx_personality_v0")
            args.insert(1, "-Wl,-u,Symbian_CPP_Exception_Descriptor")   # toolchain/dll_rt/device_excdesc.s
            args.insert(1, "-Wl,--target2=rel")   # what the linked-in GCC personality decodes
        if ignore:
            args.insert(1, "-Wl,--unresolved-symbols=ignore-all")
        if device:
            # an archive member pulled for a GCC-only helper may also define a runtime symbol the image already
            # IMPORTS from drtaeabi (its veneer comes first on the line): the veneer wins
            args.insert(1, "-Wl,--allow-multiple-definition")
        # an image past BL's +-16 MB gets linker long-branch veneers: by default `ldr ip,[pc]` + an
        # ABSOLUTE target word that --emit-relocs does not report -> unrelocated at run time. PC-relative veneers need no fixup.
        args.insert(1, "-Wl,--pic-veneer")
        r = subprocess.run(args, capture_output=True, text=True, errors="replace")
        if r.returncode:
            print("\n".join(l for l in r.stderr.splitlines() if "warning:" not in l)[-3000:]); raise SystemExit(f"{key}: link failed")

    elf1 = os.path.join(work, key + ".pass1.elf")
    if device or ehdll:
        objs = objs + _device_rt_objs(work)
    if device and not ehdll:
        link(objs, elf1, True, archives=[])       # no archives: every runtime name stays visible as missing
    else:
        link(objs, elf1, True)
    missing = sorted(BE._nm("--undefined-only", elf1) - {""})
    # `libs`: extra providers the mmp does not name (e.g. estlib standing in for P.I.P.S. libc/libm)
    resolve, dll_order = provider_maps(comp.get("libs", []) + m.libs + (DEVICE_RUNTIME_LIBS if device else []),
                                       device)
    imports = {n: resolve[n] for n in missing if n in resolve and resolve[n][0] != rom}
    # typeinfo (_ZTI/_ZTS) is never imported: RVCT-era .defs rarely freeze it and a
    # provider built without RTTI would export garbage. Each unresolved _ZTI<X> is
    # synthesized locally as a ROOT class (__class_type_info + its name): a
    # dynamic_cast whose target lies below the DLL boundary works (types compare by
    # name), one that would have to walk past it fails cleanly with NULL.
    # import_typeinfo (a `device` app deriving from the target's framework classes): the target's
    # DLLs are RVCT builds WITH RTTI, and their code dynamic_casts the app's objects (the AppUi) -- a synthesized
    # root typeinfo makes those casts NULL. Such a component imports every _ZTI the providers export instead.
    keep_ti = {n for n in imports if n.startswith("_ZTI")} if comp.get("import_typeinfo") else set()
    ti_names = [n for n in missing if n.startswith("_ZTI") and n not in keep_ti]
    for n in missing:
        if n.startswith(("_ZTI", "_ZTS")) and n not in keep_ti:
            imports.pop(n, None)
    stub_names = [n for n in missing if n not in imports and not n.startswith(("_ZTI", "_ZTS"))]
    if device:
        # what the target does not export but GCC's own archives define (the unwinder-side helpers of the personality,
        # the soft-float/int helpers RVCT spelt differently) is linked in from them at the final link, not stubbed
        from_archives = BE._nm("--defined-only", _libsupcxx(), BE._libgcc())
        stub_names = [n for n in stub_names if n not in from_archives]
    # DATA imports (vtables / patchable consts): no veneer -- each ABS32 reference
    # site becomes the import slot itself (elf.extract data_import_sites).
    data_imps = {n: imports.pop(n) for n in list(imports)
                 if n in DATA_EXPORTS or n.startswith(("_ZTV", "_ZTI", "_ZTS"))}
    veneer_o = devlink.make_veneers(work, imports, key)
    stub_s = os.path.join(work, "unimpl_stubs.s")
    with open(stub_s, "w") as fh:
        fh.write("\t.text\n\t.arm\n")
        for n in stub_names:
            fh.write(f"\t.globl {n}\n\t.type {n},%function\n{n}:\n\tb {STUB}\n")
        for n in data_imps:       # link-time placeholder; never read at runtime
            fh.write(f"\t.globl {n}\n{n}:\n\t.word 0\n")
        if ti_names:
            fh.write("\t.section .rodata.synth_typeinfo,\"a\"\n\t.align 2\n")
            for n in ti_names:
                fh.write(f"\t.globl {n}\n{n}:\n"
                         f"\t.word _ZTVN10__cxxabiv117__class_type_infoE+8\n\t.word 1f\n"
                         f"1:\t.asciz \"{n[4:]}\"\n\t.align 2\n")
    stub_o = stub_s + ".o"
    subprocess.run([BE.GCC, "-march=armv5te", "-marm", "-c", stub_s, "-o", stub_o], check=True)

    elf2 = os.path.join(work, key + ".elf")
    link(objs + [veneer_o, stub_o], elf2, False,
         archives=[_libsupcxx(), BE._libgcc()] if device else None)
    with open(elf2, "rb") as fh:
        ext = extract(fh.read(), data_imports=frozenset(data_imps))
    if entry not in ext.symbols:
        raise SystemExit(f"{key}: entry {entry} missing")
    ia = ext.symbols.get("__init_array_end", 0) - ext.symbols.get("__init_array_start", 0)
    if not m.is_exe and ia and (comp.get("static_ctors") or comp.get("allow_wsd") or m.allowdlldata):
        print(f"  {ia // 4} static constructors, run by _E32Dll on process attach")
    elif not m.is_exe and ia:
        print(f"  ! DLL has {ia // 4} static constructors (NOT run)")
    if not m.is_exe and (ext.data or ext.bss_size) and not (comp.get("allow_wsd") or m.allowdlldata):   # as x32
        print(f"  ! DLL has writable static data: data={len(ext.data)} bss={ext.bss_size}")
        syms = subprocess.run([BE.NM, "-S", "--size-sort", "-C", elf2], capture_output=True,
                              text=True).stdout.splitlines()
        for l in [l for l in syms if l.split()[2:3] and l.split()[2] in "bBdD"][-8:]:
            print("      " + l)
        return 1

    export_offsets, real = [], 0
    if defmap:
        stub_off = ext.symbols[STUB] - ext.code_base
        ex_set = set(exportable)
        for ordn in range(1, defmap.max_ordinal + 1):
            n = defmap.name(ordn)
            if n in ex_set and n in ext.symbols:
                export_offsets.append(ext.symbols[n] - ext.code_base); real += 1
            else:
                export_offsets.append(stub_off)
    import_list = devlink.import_blocks(ext, imports, dll_order=dll_order)
    if data_imps:
        blocks = {d: list(e) for d, e in import_list}
        n_sites = 0
        for n, (dll, ordn) in data_imps.items():
            for off, addend in ext.data_import_sites.get(n, []):
                if addend > 0xFFFF:
                    raise SystemExit(f"{key}: DATA import {n} addend {addend:#x} > 16 bits")
                blocks.setdefault(dll, []).append((off, ordn, addend)); n_sites += 1
        import_list = [(d, sorted(blocks[d])) for d in dll_order if d in blocks]
        print(f"  {len(data_imps)} DATA imports -> {n_sites} direct slots")
    n_imp = sum(len(e) for _, e in import_list)
    print(f"  code {len(ext.code)} B  data {len(ext.data)}  bss {ext.bss_size}   "
          f"exports {real}/{defmap.max_ordinal if defmap else 0} real   imports {n_imp} ("
          + ", ".join(f"{d} {len(e)}" for d, e in import_list) + f")   {len(stub_names)} link stubs")
    if stub_names:
        dm = subprocess.run([CXXFILT], input="\n".join(stub_names[:12]), capture_output=True,
                            text=True).stdout.splitlines()
        for l in dm:
            print("      stub: " + l)

    uid2 = m.uids[0] if m.uids else 0
    uid3 = m.uids[1] if len(m.uids) > 1 else 0
    if m.targettype == "fsy" and not uid2:
        # the toolchain's implied UID2 for a file system plug-in (KFileSystemUidValue): efat32.mmp says `UID 0 ..`
        # and EFILE loads an .fsy by this UID -- with 0 the drive is never mounted
        uid2 = 0x100039DF
    # the mmp's VERSION (libc 1.1, Qt 4.2055 = 4.8.7 ...): SDK apps import versioned names (libc{00010001}, QtCore{00040703})
    # and the loader requires the SAME major
    kw = dict(module_version=m.version)
    if ehdll:
        kw.update(exception_descriptor=(ext.symbols["Symbian_CPP_Exception_Descriptor"] - ext.code_base) | 1)
    if device:
        # the stock loader checks these: a DLL's capabilities must cover the process that loads it
        kw.update(cap1=m.cap_bits(), vendor_id=m.vendorid, secure_id=uid3, imp_elf=True, exports_in_code=True,
                  # where the unwinder finds this image's tables (device_excdesc.s): code offset | 1
                  exception_descriptor=(ext.symbols["Symbian_CPP_Exception_Descriptor"] - ext.code_base) | 1)
    if m.is_exe:
        kw.update(exe=True, stack_size=comp.get("stack") or m.stack or DEFAULT_STACK,
                  heap_min=m.heap[0] or 0x1000, heap_max=comp.get("arm_heap_max") or m.heap[1] or 0x100000)
    img = build_dll(ext.code, export_offsets, uid2=uid2, uid3=uid3,
                    entry_point=ext.symbols[entry] - ext.code_base,
                    imports=import_list, code_relocs=ext.code_relocs, code_base=ext.code_base,
                    data=ext.data, data_base=ext.data_base, bss_size=ext.bss_size,
                    data_relocs=ext.data_relocs, code_reloc_types=ext.code_reloc_types,
                    data_reloc_types=ext.data_reloc_types, **kw)
    os.makedirs(IMG_DIR, exist_ok=True)
    out_path = os.path.join(IMG_DIR, rom)
    with open(out_path, "wb") as fh:
        fh.write(img)
    # sidecar for tools/romgate.py: where this image's unimpl stub lives
    with open(out_path + ".meta", "w") as fh:
        fh.write(f"stub=0x{ext.symbols[STUB]:X}")
    compile_registration(key, m, rom)
    if device:
        # for a stock loader: its F32 loader's checks, and the exception descriptor its unwinder needs
        from loadercheck import check_loader
        bad = check_loader(img, need_exception_descriptor=True)
        if bad:
            raise SystemExit(f"{key}: the loader would refuse {rom}: " + "; ".join(bad))
    result = check_image(img)
    print(f"  wrote {_rel_build(out_path)} ({len(img)} B)  " + ("[ACCEPTED]" if result.ok else format_result(result)))
    return 0 if result.ok else 1


def main() -> int:
    args = sys.argv[1:]
    if not args or args == ["--list"]:
        for k, c in C.COMPONENTS.items():
            print(f"{k:20s} {c['mmp']}")
        return 0
    rsc_only = "--rsc" in args          # resources (.rsc/.rsg) only -- run before a
    args = [a for a in args if a != "--rsc"]    # bulk build so every <x.rsg> exists
    keys = list(C.COMPONENTS) if args == ["--all"] else args
    rc = 0
    for k in keys:
        try:
            rc |= build(k, rsc_only)
        except (Exception, SystemExit) as e:   # noqa: BLE001 -- one bad mmp (or a `link failed` SystemExit) must
            print(f"=== {k}: FAILED: {(str(e).splitlines() or ['?'])[0][:200]}")   # not stop a batch
            rc = 1
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
