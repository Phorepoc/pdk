# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""stub_audit -- every stub the CURRENT ROM ships, per image.

    python3 tools/stub_audit.py [rom args file]     # default: BUILD/rom/full.args (the last ROM built)

For each image the ROM stages (Sys\\Bin\\X=<built file>):
  link stubs   names the image calls that no provider exports (emitter sidecar <image>.linkstubs; images built
               before the sidecar existed are listed as "no record" -- rebuild them)
  stand-in     the image comes from a null / stub component (OURS *_null, or a release *stub*)
The exception-handling runtime names the emitter stubs in every C++ image (never called: no unwinding through
them) are counted apart. Provider-side unimplemented exports a ROM image IMPORTS are romgate.py's job.
"""
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD  # noqa: E402

# the C++ runtime names every image references for unwinding / RTTI matching; the emitter stubs them, nothing calls them
EH_RUNTIME = re.compile(r"^(__cxa_begin_cleanup|__cxa_type_match|__cxa_call_unexpected|__gnu_unwind_frame|"
                        r"__exidx_start|__exidx_end|__aeabi_unwind_cpp_pr[0-9]|_Unwind_\w+|__cxa_end_cleanup|"
                        r"__gxx_personality_v0|__cxa_get_globals)$")
STAND_IN = re.compile(r"(_null|null_|stub|dummy)", re.I)


def rom_images(args_path):
    out = []
    for line in open(args_path, errors="replace"):
        line = line.strip()
        if "=" not in line or line.startswith("-"):
            continue
        rom, built = line.split("=", 1)
        if rom.lower().startswith("sys\\bin\\"):
            out.append((rom[8:], built))
    return out


def demangle(names):
    if not names:
        return []
    try:
        return subprocess.run(["c++filt"], input="\n".join(names), capture_output=True, text=True).stdout.splitlines()
    except OSError:
        return names


def main() -> int:
    args_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(BUILD, "rom", "full.args")
    images = rom_images(args_path)
    total = eh_total = norec = 0
    rows = []
    for rom, built in images:
        side = built + ".linkstubs"
        if not os.path.exists(side):
            norec += 1
            rows.append((rom, built, None, 0))
            continue
        names = [n for n in open(side).read().split() if n]
        real = [n for n in names if not EH_RUNTIME.match(n)]
        eh_total += len(names) - len(real)
        total += len(real)
        rows.append((rom, built, real, len(names) - len(real)))
    print(f"ROM {args_path}: {len(images)} images, {total} link stubs (+{eh_total} EH runtime), {norec} without a record")
    print("\n== stand-ins (by built file name)")
    for rom, built, _, _ in rows:
        if STAND_IN.search(os.path.basename(built)) or STAND_IN.search(rom):
            print(f"  {rom}  <- {built}")
    print("\n== link stubs")
    for rom, built, real, eh in sorted(rows, key=lambda r: -(len(r[2]) if r[2] else 0)):
        if real:
            print(f"  {rom}: {len(real)}")
            for n in demangle(real):
                print(f"      {n}")
    if norec:
        print("\n== no record (built before the sidecar)")
        for rom, built, real, _ in rows:
            if real is None:
                print(f"  {rom}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
