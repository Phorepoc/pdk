# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_euser_dll -- shared helpers for building the user-side OS images (euser, efsrv, hal, efile ...) for ARM
from the unmodified 2010 source with the modern toolchain: compile (tools/toolchain/symcc), link (--gc-sections from
the .def exports), unimplemented-export stubs, the "fill-in" collision rule. build_euser_device.py is the euser image
itself; the other build_*.py scripts reuse these helpers.

    real source (.cpp / .cia) --symcc--> objects --ld -q @CODE_BASE--> .elf --elf.extract--> e32image.build_dll
"""
from __future__ import annotations

import os
import subprocess
import sys

from e32image import build_dll
from elf import extract
from euser_def import parse_def
from paths import PDK, OS_ROOT, SYMBIAN, BUILD
from refcheck import check_image, format_result

HERE = os.path.dirname(os.path.abspath(__file__))
CODE_BASE = 0x8000
SYMCC = os.path.join(PDK, "tools", "toolchain", "symcc")
GCC = "arm-none-eabi-gcc"
NM = "arm-none-eabi-nm"
OBJCOPY = "arm-none-eabi-objcopy"
# the Symbian 2010 kernelhwsrv package (euser, efsrv, hal, efile sources); our euser adapter sources are in OS_ROOT
KHS = os.path.join(SYMBIAN, "symbian-2010", "oss.FCL.sf.os.kernelhwsrv")
# uc_realx.cia exports the GCC-ABI soft-float helpers (__muldf3, __adddf3, the sf3
# float versions, the int/float converts and df2/sf2 compares) alongside its TRealX
# class methods. Those helpers are BROKEN under our toolchain (they raise a spurious
# __math_exception and return wrong bits — e.g. 1.5*1.5 != 2.25), while the TRealX
# methods (which the %f formatter and the Math library actually use) are correct. The
# .cia never calls its own helpers internally, so we localize them in the object and
# let libgcc supply the real, correct soft-float for all guest `double`/`float` work.
UC_REALX_SOFTFLOAT = [
    "__adddf3", "__subdf3", "__muldf3", "__divdf3",
    "__addsf3", "__subsf3", "__mulsf3", "__divsf3",
    "__floatsidf", "__floatsisf", "__extendsfdf2", "__truncdfsf2",
    "__fixdfsi", "__fixsfsi", "__negdf2", "__negsf2",
    "__eqdf2", "__nedf2", "__ltdf2", "__ledf2", "__gtdf2", "__gedf2", "__cmpdf3",
    "__eqsf2", "__nesf2", "__ltsf2", "__lesf2", "__gtsf2", "__gesf2", "__cmpsf3",
]
# The frozen euser export ordinals (EABI-mangled, matches our toolchain).
DEF_PATH = os.path.join(KHS, "kernel", "eka", "eabi", "euseru.def")
# euser_selftest is not a real euser symbol: the slot just past the real export table is reserved (it points at
# a self-test function) so the extra ordinals after it (TTrap::Trap/UnTrap, build_euser_device.EXTRA_EXPORTS)
# keep the numbers the importing images are built against.
SELFTEST_NAME = "euser_selftest"
STUB_NAME = "__unimpl_export"
B = os.path.join(KHS, "kernel", "eka")
ED = os.path.join(OS_ROOT, "toolchain", "euser_dll")


DEVICE_DIR = os.path.join(BUILD, "arm", "device")


def workdir(name: str) -> str:
    """Scratch directory of one ARM image build (objects, response files, intermediate ELFs): BUILD/arm/work/<name>."""
    d = os.path.join(BUILD, "arm", "work", name)
    os.makedirs(d, exist_ok=True)
    return d

# "Fill-in" TUs: large euser source files pulled in for a handful of exports but
# which redefine many symbols the seams/real TUs already own. After compiling,
# any global one of these objects defines that ANOTHER (earlier) object also
# defines is localized in the fill-in object, so the existing (proven) definition
# wins and the final link stays collision-free (same idea as UC_REALX_SOFTFLOAT,
# generalized to whole-symbol collisions). Order matters: fill-ins are processed
# in list order, so a symbol unique to the fill-ins keeps its first occurrence.
FILLIN_BASENAMES = ["us_exec.cpp", "us_ksvr.cpp",
                    # (client-server IPC, loader client, publish&subscribe, platsec,
                    # All are big "everything" TUs -> fill-ins: fresh symbols
                    # (CServer2/E32Loader/RProperty/...) stay global; anything an
                    # earlier proven TU already owns is localized (see
                    # _localize_fillin_collisions). Order = first occurrence wins.
                    "us_mes.cpp", "ub_svr.cpp", "us_property.cpp", "us_secure.cpp",
                    "us_rwlock.cpp", "us_decode.cpp", "up_utl.cpp", "up_lib.cpp",
                    # common/secure.cpp owns TSecurityPolicy/TCapabilitySet/PlatSec
                    # (a broad common TU -> fill-in so its overlaps yield to proven TUs).
                    "secure.cpp"]

# (source, is_cia) — compiled ARM (SYMCC_ARM=1) so the export is reachable by a
# plain blx from the ARM harness with no Thumb-bit dance.
SOURCES = [
    # real OS source: RArray + its ARM asm, the real RHybridHeap allocator, its
    # debug/walk methods, and mem primitives (which also define KHeapShrinkHysRatio).
    os.path.join(B, "common", "array.cpp"),
    os.path.join(B, "common", "arm", "carray.cia"),
    os.path.join(B, "common", "arm", "cmem.cia"),
    os.path.join(B, "common", "heap_hybrid.cpp"),
    os.path.join(B, "common", "debugfunction.cpp"),
    os.path.join(B, "common", "mem.cpp"),
    # real 8-bit descriptors (TDesC8/TDes8/TBuf8/TPtr8 + their ARM asm).
    os.path.join(B, "common", "des8.cpp"),
    os.path.join(B, "common", "arm", "cdes8.cia"),
    # real 16-bit (Unicode) descriptors + their ARM asm; CompareF/CompareC/FindF
    # route through the Unicode fold/collate subsystem below.
    os.path.join(B, "common", "des16.cpp"),
    os.path.join(B, "common", "arm", "cdes16.cia"),
    # real Unicode folding + collation: Mem::CompareF/CompareC + GetLocaleCharSet
    # (us_func.cpp) driving the real Unicode subsystem (fold/collate tables +
    # TUTF32Iterator/CompareFolded/TCollate). GC-sections prunes the rest.
    os.path.join(B, "euser", "us_func.cpp"),
    # real descriptor %-formatting: the format engine (TDes::AppendFormatList,
    # us_des.cpp) + the real lexer it uses to parse field width/precision
    # (TLex16, us_lex16.cpp). All integer/unsigned/hex/octal/char/descriptor/
    # pointer conversions + width/justify/fill/precision are real. The real-number
    # (%f/%e/%g) leaf is bounded in fmt_float_stub.cpp — see that file; wiring real
    # float means pulling soft-float TRealX + locale (RProperty), a separate step.
    os.path.join(B, "euser", "us_des.cpp"),
    os.path.join(B, "euser", "us_lex16.cpp"),
    os.path.join(B, "euser", "unicode", "Compare.cpp"),
    os.path.join(B, "euser", "unicode", "collate.cpp"),
    os.path.join(B, "euser", "unicode", "unicode.cpp"),
    os.path.join(B, "euser", "unicode", "unitable.cpp"),
    # real %f/%e/%g real-number formatting: the decimal conversion engine
    # (um_rtod.cpp: rtob/doFixed/doExponent/doGeneral + TRealFormat ctors +
    # TDes{8,16}::Num) over the real TRealX soft-float (uc_realx.cia arithmetic +
    # uc_realx.cpp's PanicOverUnderflowDividebyZero), Math::MultPow10X (um_pow10)
    # and Math::UMul64 (uc_i64). Locale (TRealFormat's decimal separator) is the
    # only seam -> locale_env.cpp (a SetDefaults-equivalent TLocale ctor, no
    # RProperty). GC-sections keeps only the reached subset.
    os.path.join(B, "euser", "maths", "um_rtod.cpp"),
    os.path.join(B, "euser", "maths", "um_pow10.cpp"),
    os.path.join(B, "euser", "epoc", "arm", "uc_realx.cia"),
    os.path.join(B, "euser", "epoc", "arm", "uc_realx.cpp"),
    os.path.join(B, "euser", "epoc", "arm", "uc_i64.cia"),
    # the rest of the Math library: pure computation over the real TRealX soft-float
    # already linked. Each adds real Math:: exports (trig / exp / ln / pow / sqrt /
    # frac / int / mod / rand / special). um_utl.cpp = shared helpers (Frac/Int/Poly).
    os.path.join(B, "euser", "maths", "um_utl.cpp"),
    os.path.join(B, "euser", "maths", "um_asin.cpp"),
    os.path.join(B, "euser", "maths", "um_atan.cpp"),
    os.path.join(B, "euser", "maths", "um_dtor.cpp"),
    os.path.join(B, "euser", "maths", "um_exp.cpp"),
    os.path.join(B, "euser", "maths", "um_frac.cpp"),
    os.path.join(B, "euser", "maths", "um_int.cpp"),
    os.path.join(B, "euser", "maths", "um_ln.cpp"),
    os.path.join(B, "euser", "maths", "um_log.cpp"),
    os.path.join(B, "euser", "maths", "um_mod.cpp"),
    os.path.join(B, "euser", "maths", "um_pow.cpp"),
    os.path.join(B, "euser", "maths", "um_rand.cpp"),
    os.path.join(B, "euser", "maths", "um_sin.cpp"),
    os.path.join(B, "euser", "maths", "um_spec.cpp"),
    os.path.join(B, "euser", "maths", "um_sqrt.cpp"),
    os.path.join(B, "euser", "maths", "um_tan.cpp"),
    # descriptor utilities: the 8-bit lexer (pairs with us_lex16) and filename
    # parsing (TParse/TParseBase/TParsePtr[C]).
    os.path.join(B, "euser", "us_lex8.cpp"),
    os.path.join(B, "euser", "us_parse.cpp"),
    # real hash-table container: RHashTableBase (open-addressing probe-sequence
    # incl. the default DefaultHash::Integer / DefaultIdentity::Integer. Env deps
    # (User::Alloc*/Free/ReAlloc, Mem::Swap, Panic) come from the existing shim +
    # real mem.cpp; the leaving variants (User::Leave) are GC-dropped.
    os.path.join(B, "euser", "us_htab.cpp"),
    # real cbase container framework (added together — they share deps):
    #   ub_buf.cpp   — CBufBase/CBufFlat (contiguous heap cell) / CBufSeg (segmented
    #                  TDblQue of segments); InsertL grows via User::ReAllocL.
    #   ub_array.cpp — CArrayFix*/CArrayVar*/CArrayPtr* over the CBuf backing.
    #   ub_obj.cpp   — CObject/CObjectCon/CObjectIx/CObjectConIx (refcounted handles).
    #   ub_utl.cpp   — CBase::~CBase/Delete/Extension_ + Panic(TBasePanic) (all three above).
    #   us_que.cpp   — TDblQue/TDblQueLink ops CBufSeg's segment list links against.
    #   gcchelp.cpp  — the global operator new/delete CObject::Close()/CArray dtors use.
    os.path.join(B, "euser", "cbase", "ub_buf.cpp"),
    os.path.join(B, "euser", "cbase", "ub_array.cpp"),
    os.path.join(B, "euser", "cbase", "ub_obj.cpp"),
    os.path.join(B, "euser", "cbase", "ub_utl.cpp"),
    #   ub_cln.cpp   — the cleanup stack (CCleanup/CTrapCleanup/CleanupStack/
    #                  TCleanupTrapHandler). Drives the trap harness (trap_env.cia)
    #                  via User::SetTrapHandler + GetTrapHandler; unblocks every *L
    #                  factory (CArrayVar*::ExpandL's TRAPD, CObjectConIx::CreateL).
    os.path.join(B, "euser", "cbase", "ub_cln.cpp"),
    #   ub_bma.cpp   — CBitMapAllocator (20 exports): pure bit-vector allocator
    #                  (Alloc/AllocAligned/AllocConsecutive/Free/IsFree/Avail).
    #   ub_circ.cpp  — CCirBufBase/CCirBuffer (12 exports): circular buffer.
    #   All self-contained algorithm over CBase + User::Alloc/Free + Mem:: — no
    #   async / IPC. us_regn.cpp = RRegion/TRegion geometry (31 exports), likewise.
    os.path.join(B, "euser", "cbase", "ub_bma.cpp"),
    os.path.join(B, "euser", "cbase", "ub_circ.cpp"),
    os.path.join(B, "euser", "us_regn.cpp"),
    #   ub_act.cpp   — the active-object framework (CActive / CActiveScheduler /
    #                  CActiveSchedulerWait / CAsyncCallBack / CAsyncOneShot / CIdle;
    #                  48 exports). Behaviour already validated host-side
    #                  (tests/euser-activeobjects). Linked + exported here; a LIVE
    #                  bench drive is deferred — it needs the scheduler-slot execs
    #                  (Exec::ActiveScheduler/SetActiveScheduler, currently link
    #                  stubs) plus the request-completion path (User::RequestComplete
    #                  / WaitForAnyRequest). Next selftest.
    os.path.join(B, "euser", "cbase", "ub_act.cpp"),
    os.path.join(B, "euser", "us_que.cpp"),
    os.path.join(B, "common", "gcchelp.cpp"),
    # real TTime/TDateTime value-class arithmetic (self-contained; the formatting/
    # locale + kernel-time paths gc-section out because the selftest never calls them).
    os.path.join(B, "euser", "us_time.cpp"),
    # our driver(s) + the kernel/env boundary shim (heap instance + env stubs).
    os.path.join(ED, "rarray_selftest.cpp"),
    os.path.join(ED, "desc_selftest.cpp"),
    os.path.join(ED, "desc16_selftest.cpp"),
    os.path.join(ED, "fmt_selftest.cpp"),
    os.path.join(ED, "hashmap_selftest.cpp"),
    os.path.join(ED, "math_selftest.cpp"),
    os.path.join(ED, "cbuf_selftest.cpp"),
    os.path.join(ED, "carray_selftest.cpp"),
    os.path.join(ED, "time_selftest.cpp"),
    os.path.join(ED, "cobject_selftest.cpp"),
    os.path.join(ED, "cobject_mc.cpp"),
    # trap_env.cia — the minimal TRAP/User::Leave/cleanup trap-frame harness
    # (setjmp/longjmp by hand + bench trap_handler execs). Owns the User::Leave
    # family now (moved out of euser_heap.cpp). cleanup_selftest.cpp exercises it.
    os.path.join(ED, "trap_env.cia"),
    os.path.join(ED, "cleanup_selftest.cpp"),
    os.path.join(ED, "misc_selftest.cpp"),   # CBitMapAllocator + CCirBuffer
    # sched_env.cpp — the kernel/env seam that lets the active-object framework
    # RUN (scheduler-slot execs, request-complete/wait execs, the C++ DoRunL the
    # ARM build hides). actsched_selftest.cpp drives it live.
    os.path.join(ED, "sched_env.cpp"),
    os.path.join(ED, "actsched_selftest.cpp"),
    # ub_tim.cpp — real CTimer/CPeriodic/CHeartbeat; timer_env.cpp = the RTimer
    # kernel-handle seam (bench timer execs); timer_selftest.cpp drives an async
    # timer whose completion arrives from the bench timing thread.
    os.path.join(B, "euser", "cbase", "ub_tim.cpp"),
    os.path.join(ED, "timer_env.cpp"),
    os.path.join(ED, "timer_selftest.cpp"),
    # asyncos_selftest.cpp — live drive of CAsyncOneShot/CAsyncCallBack (Setup ->
    # RThread::Duplicate + Add; Call -> SetActive + iThread.RequestComplete).
    os.path.join(ED, "asyncos_selftest.cpp"),
    # aswait_selftest.cpp — live drive of CActiveSchedulerWait (owned nested loop
    # broken by AsyncStop).
    os.path.join(ED, "aswait_selftest.cpp"),
    # cperiodic_selftest.cpp — live drive of CPeriodic (repeating timer; RunL
    # re-arms via After(iInterval)). Reuses the RTimer seam.
    os.path.join(ED, "cperiodic_selftest.cpp"),
    # RSemaphore / RMutex — the kernel synchronisation-object handle wrappers.
    # us_exec.cpp (Wait/Signal/Poll/IsHeld) + us_ksvr.cpp (CreateLocal +
    # RHandleBase::Close/SetReturnedHandle) are the two big "everything" euser
    # TUs; they redefine dozens of symbols the seams already own (heap, request,
    # RTimer, RChunk, TChar, TLocale ...), so they are FILL-IN sources: any global
    # they define that another object already defines is localized in their object
    # (see FILLIN_BASENAMES + compile_objs) so the proven definitions win and only
    # their fresh symbols (RSemaphore/RMutex + many more thin wrappers) go global.
    # sync_env.cpp = the bench Exec:: seam that makes the sync wrappers RUN;
    # sync_selftest.cpp drives them live.
    os.path.join(B, "euser", "us_exec.cpp"),
    os.path.join(B, "euser", "us_ksvr.cpp"),
    #   us_mes.cpp     — RSessionBase/RSubSessionBase::DoSendReceive (IPC client)
    #   cbase/ub_svr.cpp — CServer2/CSession2 (client-server SERVER framework)
    #   us_property.cpp  — RProperty (publish & subscribe)
    #   us_secure.cpp    — TSecurityPolicy/TCapabilitySet/PlatSec (platsec)
    #   us_rwlock.cpp    — RReadWriteLock
    #   epoc/up_utl.cpp  — UserSvr::* + __e32_atomic_* + RProcess/RThread SecureId
    #   epoc/up_lib.cpp  — RLibrary + E32Loader code-seg client wrappers
    os.path.join(B, "euser", "us_mes.cpp"),
    os.path.join(B, "euser", "cbase", "ub_svr.cpp"),
    os.path.join(B, "euser", "us_property.cpp"),
    os.path.join(B, "euser", "us_secure.cpp"),
    os.path.join(B, "euser", "us_rwlock.cpp"),
    os.path.join(B, "euser", "us_decode.cpp"),
    os.path.join(B, "euser", "epoc", "up_utl.cpp"),
    os.path.join(B, "euser", "epoc", "up_lib.cpp"),
    #   common/secure.cpp — TSecurityPolicy/TCapabilitySet/PlatSec (platsec value classes)
    #   common/arm/atomics.cia — __e32_atomic_* (up_utl et al. reference these)
    os.path.join(B, "common", "secure.cpp"),
    os.path.join(B, "common", "arm", "atomics.cia"),
    os.path.join(ED, "sync_env.cpp"),
    os.path.join(ED, "sync_selftest.cpp"),
    os.path.join(ED, "hash_env.cpp"),
    os.path.join(ED, "locale_env.cpp"),
    os.path.join(ED, "coll_selftest.cpp"),
    os.path.join(ED, "unicode_env.cpp"),
    os.path.join(ED, "desc_compat.cpp"),
    os.path.join(ED, "euser_heap.cpp"),
    # the placeholder every not-yet-from-source export ordinal points to.
    os.path.join(ED, "unimpl_export.cpp"),
]


def compile_objs(workdir: str, sources: list[str] = None,
                 fillin: list[str] = None, extra_flags=None) -> list[str]:
    """Compile every SOURCE to an ARM object; return the object paths.

    `sources`/`fillin` default to the bench euser lists; the device euser build
    passes its own (real-source TUs + dev_user_execs.cpp - bench HLE seams).
    `extra_flags(basename) -> list[str]` supplies per-source extra compiler args
    (e.g. `-D__GEN_USER_EXEC_CODE__` for the device exec layer).
    """
    sources = SOURCES if sources is None else sources
    fillin = FILLIN_BASENAMES if fillin is None else fillin
    env = dict(os.environ, SYMCC_ARM="1")
    objs = []
    for src in sources:
        base = os.path.basename(src)
        obj = os.path.join(workdir, base + ".o")
        # uc_realx.cia (TRealX soft-float) branches between its own functions with
        # PC-relative b/bl; -ffunction-sections puts each in a separate section, so
        # GAS can't resolve the cross-function branch ("symbol is in a different
        # section"). Keep this one TU in a single .text (the soft-float cluster is
        # reached as a unit anyway, so nothing is lost to GC).
        extra = (["-fno-function-sections", "-fno-data-sections"]
                 if base in ("uc_realx.cia", "atomics.cia") else [])
        # GCC 14 does not honor the spirit of __attribute__((naked)) for functions
        # returning an aggregate via sret: it brackets the asm body with
        # `mov r3,r0 ... mov r0,r3` return-pointer bookkeeping (kept even at -O2 --
        # DCE is off in naked fns). The PROLOGUE mov clobbers r3, which is a LIVE
        # ARGUMENT in every naked fn with sret + 3 register args:
        # TDesC8/16::Mid(pos,len) and TDes8/16::MidTPtr(pos,len). cdes16.cia's
        # range check then compares aLength==sret-pointer and ALWAYS panics
        # (found live at EFILE bring-up: USER 10 from TParseBase::Drive() on a
        # perfectly valid TParse). Fix: compile the des TUs with the machine-coded
        # guards OFF so the real C++ bodies all exist, and weaken everything in the
        # cdes objects so the C++ definitions win; the .cia keeps only what C++
        # does not define.
        if base in ("des8.cpp", "des16.cpp"):
            extra = extra + ["-D__MINIMUM_MACHINE_CODE__"]
        if extra_flags is not None:
            extra = extra + list(extra_flags(base))
        subprocess.run(["bash", SYMCC, src, "-O2", "-o", obj, *extra], check=True,
                       env=env, cwd=PDK)
        if base in ("cdes8.cia", "cdes16.cia"):
            subprocess.run([OBJCOPY, "--weaken", obj], check=True)
        if base == "uc_realx.cia":
            # Hide the broken GCC-ABI soft-float helpers so libgcc's win.
            subprocess.run([OBJCOPY,
                            *sum(([f"--localize-symbol={s}"] for s in UC_REALX_SOFTFLOAT), []),
                            obj], check=True)
        objs.append(obj)

    _localize_fillin_collisions(objs, fillin)
    return objs


def _localize_fillin_collisions(objs: list[str], fillin: list[str] = None) -> None:
    """Make the FILLIN_BASENAMES objects yield to every other object.

    For each fill-in object (in SOURCES order), localize any global symbol that a
    non-fill-in object — or an earlier fill-in — already defines globally, so the
    existing proven definition is the one the final link resolves against. A
    symbol unique to the fill-ins keeps its first occurrence global.
    """
    fillin = FILLIN_BASENAMES if fillin is None else fillin
    fillin_objs = [o for o in objs
                   if os.path.basename(o).rsplit(".o", 1)[0] in fillin]
    other_objs = [o for o in objs if o not in fillin_objs]

    claimed = _nm("--defined-only", *other_objs) if other_objs else set()
    for obj in fillin_objs:
        defined = _nm("--defined-only", obj)
        # _ZT*: typeinfo / typeinfo name / vtable / VTT. With -frtti they are COMDAT (weak) wherever the class has no key
        # function; a duplicate must stay global so the linker merges the groups. Localizing it would leave this object's
        # references bound to its own copy, whose section the linker then discards ("defined in discarded section").
        collisions = sorted(c for c in (defined & claimed) if not c.startswith("_ZT"))
        if collisions:
            subprocess.run([OBJCOPY,
                            *sum(([f"--localize-symbol={s}"] for s in collisions), []),
                            obj], check=True)
        claimed |= (defined - set(collisions))


def _nm(mode: str, *files: str) -> set[str]:
    """Symbol names from nm; `mode` selects --defined-only / --undefined-only."""
    args = list(files)
    if sum(len(f) + 1 for f in args) > 24000:
        # a very long object list goes through an @file
        import tempfile
        with tempfile.NamedTemporaryFile("w", suffix=".rsp", delete=False) as t:
            t.write("\n".join('"' + f.replace("\\", "/") + '"' for f in args))
        args = ["@" + t.name]
    out = subprocess.run([NM, mode, *args], check=True,
                         capture_output=True, text=True).stdout
    names: set[str] = set()
    for line in out.splitlines():
        parts = line.split()
        if mode == "--defined-only":
            # "<addr> <T|W|...> <name>" — keep GLOBAL (uppercase) code/data symbols.
            if len(parts) == 3 and parts[1] in ("T", "W", "R", "D", "B", "V"):
                names.add(parts[2])
        else:  # --undefined-only: "U <name>" (or "<addr> U <name>")
            names.add(parts[-1])
    return names


# Extra objects linked after the sources and before libgcc (the device euser: the C++ ABI runtime members).
EXTRA_LIBS: list[str] = []


def _libgcc() -> str:
    return subprocess.run([GCC, "-marm", "-march=armv5te", "-print-libgcc-file-name"],
                          check=True, capture_output=True, text=True).stdout.strip()


def _link(objs: list[str], roots: list[str], out_elf: str, workdir: str,
          ignore_unresolved: bool) -> None:
    ld = os.path.join(HERE, "spike.ld")   # .text@0x8000 + data/bss
    # roots go through a response file: 770+ `-u` flags make a very long command line. --gc-sections keeps only sections reachable from the roots.
    rsp = os.path.join(workdir, "roots.rsp")
    with open(rsp, "w") as fh:
        fh.write("\n".join(f"-Wl,-u,{n}" for n in roots))
    args = [GCC, "-nostdlib", "-Wl,-q", "-Wl,--gc-sections", "-Wl,--no-demangle",
            f"@{rsp}", f"-Wl,-T,{ld}", "-march=armv5te", "-marm", *objs, *EXTRA_LIBS, _libgcc(),
            "-o", out_elf]
    # opt-in extra linker args (LEAVE_TRACE: --wrap User::Leave, see build_euser_device)
    args[1:1] = os.environ.get("EUSER_LD_EXTRA", "").split()
    if ignore_unresolved:
        args.insert(1, "-Wl,--unresolved-symbols=ignore-all")
    subprocess.run(args, check=True)


def compile_and_link(workdir: str, sources: list[str] = None,
                     fillin: list[str] = None, extra_flags=None
                     ) -> tuple[bytes, list[str], set[str]]:
    """Build the ELF that backs the full euser export table.

    Roots every euser.def symbol our sources actually define (so --gc-sections
    keeps them at addresses we can export), plus euser_selftest and the unimpl
    stub. A first (unresolved-tolerant) link discovers the symbols the rooted code
    calls but that we don't build from source yet; those are aliased to the stub
    so the final link is clean and any such call fails loud instead of running
    into address 0. Returns (elf, exportable_names, stubbed_names).
    """
    objs = compile_objs(workdir, sources, fillin, extra_flags)

    defmap = parse_def(DEF_PATH)
    def_names = set(defmap.by_ordinal.values())
    defined = _nm("--defined-only", *objs)
    exportable = sorted(def_names & defined)
    roots = exportable + [SELFTEST_NAME, STUB_NAME]

    # Pass 1: tolerate unresolved to enumerate what the rooted closure still needs.
    elf1 = os.path.join(workdir, "euser0.pass1.elf")
    _link(objs, roots, elf1, workdir, ignore_unresolved=True)
    missing = sorted(_nm("--undefined-only", elf1) - {""})

    # A tiny stub TU: each missing symbol becomes a 1-instruction thunk to the
    # unimplemented-export handler, so referencing code links and, if ever run,
    # panics cleanly. (Data symbols get the same address; using them faults — the
    # honest "not from source yet" outcome.)
    stub_s = os.path.join(workdir, "unimpl_stubs.s")
    with open(stub_s, "w") as fh:
        fh.write("\t.text\n\t.arm\n")
        for i, name in enumerate(missing):
            fh.write(f"\t.globl {name}\n\t.type {name},%function\n"
                     f"{name}:\n\tb {STUB_NAME}\n")
    stub_o = os.path.join(workdir, "unimpl_stubs.s.o")
    subprocess.run([GCC, "-march=armv5te", "-marm", "-c", stub_s, "-o", stub_o],
                   check=True)

    # Pass 2: clean link (no unresolved tolerance) with the stubs in place.
    elf2 = os.path.join(workdir, "euser0.elf")
    _link(objs + [stub_o], roots, elf2, workdir, ignore_unresolved=False)
    with open(elf2, "rb") as fh:
        return fh.read(), exportable, set(missing)


def main() -> int:
    out_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "euser0.dll")
    work = workdir("euser0")

    defmap = parse_def(DEF_PATH)
    elf_blob, exportable, stubbed = compile_and_link(work)
    ext = extract(elf_blob)

    if ext.code_base != CODE_BASE:
        sys.exit(f"code_base mismatch: 0x{ext.code_base:X} != 0x{CODE_BASE:X}")
    for name in (SELFTEST_NAME, STUB_NAME):
        if name not in ext.symbols:
            sys.exit(f"required symbol {name!r} not in ELF")

    # Build the full export table: ordinal N -> address of euseru.def's symbol at N
    # if we build it from source, else the unimplemented-export stub. Offsets are
    # (VMA - code_base) so build_dll's code_base+offset reconstructs the real VMA
    # (works for code and data symbols alike).
    exportable_set = set(exportable)
    stub_off = ext.symbols[STUB_NAME] - ext.code_base
    export_offsets = []
    real = 0
    for ordn in range(1, defmap.max_ordinal + 1):
        name = defmap.name(ordn)
        if name in exportable_set and name in ext.symbols:
            export_offsets.append(ext.symbols[name] - ext.code_base)
            real += 1
        else:
            export_offsets.append(stub_off)
    # euser_selftest just past the real table (harness calls this ordinal).
    selftest_ord = defmap.max_ordinal + 1
    export_offsets.append(ext.symbols[SELFTEST_NAME] - ext.code_base)

    print(f"code: {len(ext.code)} B @0x{ext.code_base:X}   "
          f"data: {len(ext.data)} B   bss: {ext.bss_size} B")
    print(f"exports: {len(export_offsets)} ordinals  "
          f"({real}/{defmap.max_ordinal} real from source, "
          f"{defmap.max_ordinal - real} -> unimpl stub); "
          f"{len(stubbed)} link stubs; {SELFTEST_NAME} @ {selftest_ord}")
    print(f"code relocs: {len(ext.code_relocs)}   data relocs: {len(ext.data_relocs)}")

    img = build_dll(ext.code, export_offsets, uid3=0x20005001,
                    code_relocs=ext.code_relocs, code_base=ext.code_base,
                    data=ext.data, data_base=ext.data_base, bss_size=ext.bss_size,
                    data_relocs=ext.data_relocs,
                    code_reloc_types=ext.code_reloc_types,
                    data_reloc_types=ext.data_reloc_types)
    with open(out_path, "wb") as fh:
        fh.write(img)
    print(f"\nWrote {out_path} ({len(img)} bytes)")
    result = check_image(img)
    print(format_result(result))
    return 0 if result.ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
