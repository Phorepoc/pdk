# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""stub_audit -- every stub the images ship, and the ratchet that keeps new ones out.

    python3 tools/stub_audit.py report [--arch x32|arm] [--rom ARGS]
    python3 tools/stub_audit.py check  [--arch x32|arm] [--rom ARGS] [--core]   # CI: fails on a new stub (exit 1)
    python3 tools/stub_audit.py allow  [--arch x32|arm] [--rom ARGS]     # prints the allowlist for what is there now

No stub has a place in an image (docs/stub-audit.md). A "link stub" is a name an image calls that no provider exports:
the emitter (build_mmp.py / build_mmp_x32.py) links it to `__unimpl_export`, which panics the caller. Every image the
emitter writes carries a record, `<image>.linkstubs` (one name per line), next to it in BUILD/x32img or BUILD/arm/img.

The check compares the records with tools/stub-allow/<arch>.allow:
  * an (image, name) that is not in the allowlist fails the build -- a new stub;
  * an allowlist line that no record has any more fails too (delete it: the list only ever shrinks);
  * an allowlist line must name a reason of stub-allow/reasons.txt (each with its tracking line in docs/stub-audit.md);
  * an image without a record fails (an image the audit cannot see);
  * the C++ exception-handling runtime names (never called: nothing unwinds through them) are a counted ceiling per
    architecture, `#ceiling eh N` in the allowlist; the count may only go down.
--rom ARGS limits the audit to the images a ROM stages (Sys\\Bin\\X=<built file> lines of its args file).
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
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
    """({(image, name): reason id}, eh ceiling or None)."""
    entries, ceiling = {}, None
    path = os.path.join(ALLOW_DIR, arch + ".allow")
    if not os.path.exists(path):
        return entries, ceiling
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        if line.startswith("#ceiling eh "):
            ceiling = int(line.split()[2])
            continue
        if not line.strip() or line.startswith("#"):
            continue
        rid, image, name = line.split()
        entries[(image, name)] = rid
    return entries, ceiling


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
    print(f"#ceiling eh {eh}")
    for image, n in sorted(real):
        rid = next((r for r, (rx, _t, _k, _f) in reasons.items() if rx.search(n)), "R0")
        print(f"{rid} {image} {n}")
    return 0


def cmd_check(arch, rom_args, core=False):
    reasons = load_reasons()
    allow, ceiling = load_allow(arch)
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
    if ceiling is None:
        bad += 1
        print(f"stub_audit: stub-allow/{arch}.allow has no '#ceiling eh N' line")
    elif eh > ceiling:
        bad += 1
        print(f"stub_audit: {eh} exception-runtime link stubs, over the ceiling {ceiling}")
    elif eh < ceiling and not core:     # (a notice, not a failure: boards differ in what they add; the ceiling follows the best build)
        print(f"stub_audit: {eh} exception-runtime link stubs, under the ceiling {ceiling}: lower '#ceiling eh' to {eh}")
    print(f"stub_audit {arch}: {nimg} images, {len(real)} link stubs ({len(allow)} allowed), {eh} EH runtime -- "
          + ("FAILED" if bad else "ok"))
    return 1 if bad else 0


def main():
    args = sys.argv[1:]
    cmd = args.pop(0) if args and args[0] in ("report", "check", "allow") else "report"
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
    return {"report": cmd_report, "allow": cmd_allow}[cmd](arch, rom)


if __name__ == "__main__":
    sys.exit(main())
