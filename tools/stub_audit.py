# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""stub_audit -- every stub the images ship, and the ratchet that keeps new ones out.

    python3 tools/stub_audit.py report [--arch x32|arm] [--rom ARGS]
    python3 tools/stub_audit.py check  [--arch x32|arm] [--rom ARGS] [--core]   # CI: fails on a new stub (exit 1)
    python3 tools/stub_audit.py allow  [--arch x32|arm] [--rom ARGS]     # prints the allowlist for what is there now
    python3 tools/stub_audit.py dropped [--arch x32|arm]                 # the sources the registry leaves out of a build
    python3 tools/stub_audit.py package [--arch x32|arm] REPO [SINCE]    # package.yml: the images this job built (newer than SINCE)

No stub has a place in an image (docs/stub-audit.md). A "link stub" is a name an image calls that no provider exports:
the emitter (build_mmp.py / build_mmp_x32.py) links it to `__unimpl_export`, which panics the caller. Every image the
emitter writes carries a record, `<image>.linkstubs` (one name per line), next to it in BUILD/x32img or BUILD/arm/img.

The check compares the records with tools/stub-allow/<arch>.allow:
  * an (image, name) that is not in the allowlist fails the build -- a new stub;
  * an allowlist line that no record has any more fails too (delete it: the list only ever shrinks);
  * an allowlist line must name a reason of stub-allow/reasons.txt (each with its tracking line in docs/stub-audit.md);
  * an image without a record fails (an image the audit cannot see);
  * (kind 5) a source the registry's drop=/nosrc leaves out of a build needs a line in stub-allow/dropped.allow (an
    adaptation's own drops: <adaptation>/stub-allow/dropped.allow) with a
    reason of dropped-reasons.txt; a line no registry entry drops any more fails too;
  * the C++ exception-handling runtime names (never called: nothing unwinds through them) fail the build unless the
    architecture'"'"'s allowlist says `#eh allowed` (arm: until its images import them from drtaeabi, docs/stub-audit.md).
--rom ARGS limits the audit to the images a ROM stages (Sys\\Bin\\X=<built file> lines of its args file).
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
import paths  # noqa: E402
from paths import BUILD  # noqa: E402

ALLOW_DIR = os.path.join(HERE, "stub-allow")

# the C++ runtime names every image references for unwinding / RTTI matching; the emitter stubs them, nothing calls them
EH_RUNTIME = re.compile(r"^(__cxa_begin_cleanup|__cxa_type_match|__cxa_call_unexpected|__gnu_unwind_frame|"
                        r"__exidx_start|__exidx_end|__aeabi_unwind_cpp_pr[0-9]|_Unwind_\w+|__cxa_end_cleanup|"
                        r"__gxx_personality_v0|__cxa_get_globals)$")
STAND_IN = re.compile(r"(_null|null_|stub|dummy)", re.I)


def img_dir(arch):
    return os.path.join(BUILD, "x32img") if arch == "x32" else os.path.join(BUILD, "arm", "img")


# the images the image recipes build with their own builders (the core libraries, the kernel-side images): they sit
# outside the component build, in these folders once `phe image` has run, and carry a record all the same
EXTRA_DIRS = {"x32": ["x32core"], "arm": [os.path.join("arm", "device")]}


def rom_images(args_path):
    out = []
    for line in open(args_path, errors="replace"):
        line = line.strip()
        if "=" not in line or line.startswith("-"):
            continue
        rom, built = line.split("=", 1)
        if built.startswith("/build/"):         # the build folder as the toolchain container saw it
            built = os.path.join(BUILD, built[len("/build/"):])
        if rom.lower().startswith("sys\\bin\\"):
            out.append((rom[8:], built))
    return out


def images(arch, rom_args=None):
    """[(name, built file)] -- every image of the architecture's build, or those a ROM stages."""
    if rom_args:
        return rom_images(rom_args)
    d = img_dir(arch)
    names = sorted(f for f in os.listdir(d) if os.path.exists(os.path.join(d, f + ".meta"))) if os.path.isdir(d) else []
    out = [(n, os.path.join(d, n)) for n in names]
    for x in EXTRA_DIRS.get(arch, []):
        xd = os.path.join(BUILD, x)
        if os.path.isdir(xd):
            out += [(os.path.basename(x) + "/" + f, os.path.join(xd, f)) for f in sorted(os.listdir(xd)) if f.lower().endswith((".dll", ".exe"))]
    return out


def records(arch, rom_args=None):
    """({image: [names]}, [images without a record])."""
    recs, norec = {}, []
    for name, built in images(arch, rom_args):
        side = built + ".linkstubs"
        if not os.path.exists(side):
            norec.append(name)
            continue
        recs[name] = [n for n in open(side).read().split() if n]
    return recs, norec


def demangle(names):
    if not names:
        return []
    try:
        return subprocess.run(["c++filt"], input="\n".join(names), capture_output=True, text=True).stdout.splitlines()
    except OSError:
        return names


def load_reasons():
    """{id: (regex of symbols, text, tracking, flags)} from stub-allow/reasons.txt:  id | symbol regex | text | tracking [| flags]  (fields split on " | "; a regex may hold a bare |).
    flag `board`: some board's adaptation provides the name (that board's build no longer has the stub), so a line under
    this reason is never reported as unneeded."""
    out = {}
    path = os.path.join(ALLOW_DIR, "reasons.txt")
    if os.path.exists(path):
        for line in open(path, encoding="utf-8"):
            line = line.rstrip("\n")
            if not line.strip() or line.lstrip().startswith("#"):
                continue
            parts = [p.strip() for p in line.split(" | ", 4)]
            rid, rx, text, track = parts[:4]
            out[rid] = (re.compile(rx), text, track, parts[4] if len(parts) > 4 else "")
    return out


def load_allow(arch):
    """({(image, name): reason id}, whether the exception-runtime names are allowed)."""
    entries, eh_ok = {}, False
    path = os.path.join(ALLOW_DIR, arch + ".allow")
    if not os.path.exists(path):
        return entries, eh_ok
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        if line.startswith("#eh allowed"):
            eh_ok = True
            continue
        if not line.strip() or line.startswith("#"):
            continue
        rid, image, name = line.split()
        entries[(image, name)] = rid
    return entries, eh_ok


def current(arch, rom_args=None):
    """(real stubs {(image, name)}, eh name count, images without a record, image count)."""
    recs, norec = records(arch, rom_args)
    real, eh = set(), 0
    for image, names in recs.items():
        for n in names:
            if EH_RUNTIME.match(n):
                eh += 1
            else:
                real.add((image, n))
    return real, eh, norec, len(recs) + len(norec)


def registry_drops(arch):
    """{(component, source)}: what the registry leaves out of an `arch` build (drop= / x32_drop= / nosrc `*`)."""
    sys.path.insert(0, paths.REGISTRY)
    for r in paths.ADAPTATION_REGISTRIES:
        sys.path.insert(0, r)
    import components as C
    out = set()
    for key, comp in C.COMPONENTS.items():
        if arch == "x32" and comp.get("x32_real"):
            continue
        if comp.get("nosrc"):
            out.add((key, "*"))
        for d in comp.get("drop", []) + (comp.get("x32_drop", []) if arch == "x32" else []):
            if not comp.get("nosrc"):
                out.add((key, d.lower()))
    return out


def load_dropped():
    """({(component, source): reason}, {reason ids})."""
    entries, reasons = {}, set()
    # the pdk's list (core's drops), then each adaptation's own stub-allow/dropped.allow (the sources only its registry drops)
    for path in [os.path.join(ALLOW_DIR, "dropped.allow")] + [os.path.join(a, "stub-allow", "dropped.allow") for a in paths.ADAPTATIONS]:
        if not os.path.exists(path):
            continue
        for line in open(path, encoding="utf-8"):
            if line.strip() and not line.startswith("#"):
                rid, comp, src = line.split()
                entries[(comp, src)] = rid
    path = os.path.join(ALLOW_DIR, "dropped-reasons.txt")
    if os.path.exists(path):
        for line in open(path, encoding="utf-8"):
            if line.strip() and not line.startswith("#"):
                reasons.add(line.split(" | ")[0].strip())
    return entries, reasons


def check_dropped(arch, core=False):
    """kind 5. Returns the number of failure groups."""
    allow, reasons = load_dropped()
    try:
        now = registry_drops(arch)
        both = now | registry_drops("arm" if arch == "x32" else "x32")
    except Exception as e:          # the registry did not load: the audit cannot see the drops
        print(f"stub_audit: cannot read the registry's dropped sources: {e}")
        return 1
    bad = 0
    new = sorted(now - set(allow))
    if new:
        bad += 1
        print(f"stub_audit: {len(new)} source(s) the registry leaves out of the {arch} build have no line in stub-allow/dropped.allow:")
        for c, s in new[:60]:
            print(f"  {c} {s}")
        print("  Build the release's file, or say what replaces it and why (dropped-reasons.txt).")
    gone = [] if core else sorted(set(allow) - both)
    if gone:
        bad += 1
        print(f"stub_audit: {len(gone)} dropped.allow line(s) name a source no registry entry leaves out any more -- delete them:")
        for c, s in gone[:60]:
            print(f"  {allow[(c, s)]} {c} {s}")
    unknown = sorted({r for r in allow.values() if r not in reasons})
    if unknown:
        bad += 1
        print(f"stub_audit: dropped.allow lines with no reason in dropped-reasons.txt: {', '.join(unknown)}")
    print(f"stub_audit {arch}: {len(now)} dropped sources ({len(allow)} listed) -- " + ("FAILED" if bad else "ok"))
    return bad


def cmd_dropped(arch, rom_args):
    allow, _ = load_dropped()
    for c, s in sorted(registry_drops(arch)):
        print(f"{allow.get((c, s), 'D?')} {c} {s}")
    return 0


def cmd_report(arch, rom_args):
    recs, norec = records(arch, rom_args)
    total = eh = 0
    rows = []
    for image, names in recs.items():
        real = [n for n in names if not EH_RUNTIME.match(n)]
        eh += len(names) - len(real)
        total += len(real)
        rows.append((image, real))
    print(f"{arch}: {len(recs) + len(norec)} images, {total} link stubs (+{eh} EH runtime), {len(norec)} without a record")
    print("\n== stand-ins (by file name)")
    for image, _ in rows:
        if STAND_IN.search(image):
            print(f"  {image}")
    print("\n== link stubs")
    for image, real in sorted(rows, key=lambda r: -len(r[1])):
        if real:
            print(f"  {image}: {len(real)}")
            for n in demangle(real):
                print(f"      {n}")
    if norec:
        print("\n== no record")
        for image in norec:
            print(f"  {image}")
    return 0


def cmd_allow(arch, rom_args):
    reasons = load_reasons()
    real, eh, _, _ = current(arch, rom_args)
    print(f"# Link stubs allowed in the {arch} images: reason, image, name. The list only ever shrinks")
    print("# (tools/stub_audit.py check; reasons: stub-allow/reasons.txt, tracked in docs/stub-audit.md).")
    if eh:
        print("#eh allowed")
    for image, n in sorted(real):
        rid = next((r for r, (rx, _t, _k, _f) in reasons.items() if rx.search(n)), "R0")
        print(f"{rid} {image} {n}")
    return 0


def cmd_check(arch, rom_args, core=False):
    reasons = load_reasons()
    allow, eh_ok = load_allow(arch)
    real, eh, norec, nimg = current(arch, rom_args)
    bad = 0
    new = sorted(real - set(allow))
    if new:
        bad += 1
        print(f"stub_audit: {len(new)} NEW link stub(s) in the {arch} images (no stub has a place in an image):")
        for image, n in new[:60]:
            print(f"  {image}: {(demangle([n]) or [n])[0]}")
        if len(new) > 60:
            print(f"  ... and {len(new) - 60} more")
        print("  Give the name a real provider, or (with the user's decision) add it to tools/stub-allow with a reason.")
    gone = [] if core else sorted(e for e in set(allow) - real if "board" not in reasons.get(allow[e], (0, 0, 0, ""))[3])
    if gone:
        bad += 1
        print(f"stub_audit: {len(gone)} allowlist line(s) no image needs any more -- delete them from stub-allow/{arch}.allow:")
        for image, n in gone[:60]:
            print(f"  {allow[(image, n)]} {image} {n}")
    unknown = sorted({rid for rid in allow.values() if rid not in reasons})
    if unknown:
        bad += 1
        print(f"stub_audit: allowlist lines with no reason in stub-allow/reasons.txt: {', '.join(unknown)}")
    if norec:
        bad += 1
        print(f"stub_audit: {len(norec)} image(s) without a link-stub record (.linkstubs): {', '.join(norec[:20])}")
    if eh and not eh_ok:
        bad += 1
        print(f"stub_audit: {eh} exception-runtime link stubs in the {arch} images, and stub-allow/{arch}.allow has no '#eh allowed' line")
    print(f"stub_audit {arch}: {nimg} images, {len(real)} link stubs ({len(allow)} allowed), {eh} EH runtime -- "
          + ("FAILED" if bad else "ok"))
    bad += check_dropped(arch, core)
    return 1 if bad else 0


def cmd_package(arch, repo, since):
    """The package build's check (package.yml). Every image this job built -- a .linkstubs record newer than `since` (a file
    package.yml touches before the component build), anywhere under the build tree (a package's builder may write elsewhere, e.g. qt48/x32img) --
    may carry no link stub but those <repo>/stub-allow/<arch>.allow lists (the pdk's format and reasons). The
    exception-runtime names follow the pdk's `#eh allowed` for the architecture."""
    reasons = load_reasons()
    _, eh_ok = load_allow(arch)
    allow = {}
    path = os.path.join(repo, "stub-allow", arch + ".allow")
    if os.path.exists(path):
        for line in open(path, encoding="utf-8"):
            if line.strip() and not line.startswith("#"):
                rid, image, name = line.split()
                allow[(image, name)] = rid
    t0 = os.path.getmtime(since) if since and os.path.exists(since) else 0
    real, eh, nimg = set(), 0, 0
    for root, _dirs, files in os.walk(BUILD):
        for f in files:
            p = os.path.join(root, f)
            if not f.endswith(".linkstubs") or os.path.getmtime(p) <= t0:
                continue
            nimg += 1
            for n in open(p).read().split():
                if EH_RUNTIME.match(n):
                    eh += 1
                else:
                    real.add((f[:-len(".linkstubs")], n))
    bad = 0
    new = sorted(real - set(allow))
    if new:
        bad = 1
        print(f"stub_audit: {len(new)} link stub(s) in the package's {arch} images (a call to one panics EUSRNIMP):")
        for image, n in new[:60]:
            print(f"  {image}: {(demangle([n]) or [n])[0]}")
        print("  Link the provider (a lost LIBRARY line, a configure step), or list it in the package repository's "
              f"stub-allow/{arch}.allow with a reason of the pdk's stub-allow/reasons.txt.")
    unknown = sorted({rid for rid in allow.values() if rid not in reasons})
    if unknown:
        bad = 1
        print(f"stub_audit: allowlist lines with no reason in the pdk's stub-allow/reasons.txt: {', '.join(unknown)}")
    if eh and not eh_ok:
        bad = 1
        print(f"stub_audit: {eh} exception-runtime link stubs, and the pdk's stub-allow/{arch}.allow has no '#eh allowed' line")
    print(f"stub_audit package {arch}: {nimg} images built, {len(real)} link stubs ({len(allow)} allowed), {eh} EH runtime -- "
          + ("FAILED" if bad else "ok"))
    return bad


def main():
    args = sys.argv[1:]
    if args and args[0] == "package":
        args.pop(0)
        arch = os.environ.get("PHOREPOC_ARCH", "x32")
        if args and args[0] == "--arch":
            args.pop(0)
            arch = args.pop(0)
        if not args:
            sys.exit(__doc__)
        return cmd_package(arch, args[0], args[1] if len(args) > 1 else None)
    cmd = args.pop(0) if args and args[0] in ("report", "check", "allow", "dropped") else "report"
    arch = os.environ.get("PHOREPOC_ARCH", "x32")
    rom, core = None, False
    while args:
        a = args.pop(0)
        if a == "--arch":
            arch = args.pop(0)
        elif a == "--rom":
            rom = args.pop(0)
        elif a == "--core":
            core = True
        else:
            sys.exit(__doc__)
    if cmd == "check":
        return cmd_check(arch, rom, core)
    return {"report": cmd_report, "allow": cmd_allow, "dropped": cmd_dropped}[cmd](arch, rom)


if __name__ == "__main__":
    sys.exit(main())
