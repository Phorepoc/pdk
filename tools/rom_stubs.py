# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""rom_stubs -- ROM stub SIS files for image components that are also released as installable packages.

    python3 tools/rom_stubs.py <rom-args-file> <out-dir>    # prints the --data lines for the ROM

A component of the image that also ships as a SIS (so it can be updated without a new image, e.g. the browser frontend)
names its package in the registry:

    rom_stub=dict(name="Phorepoc Browser", files=["sys\\\\bin\\\\phebrowser.exe", "resource\\\\apps\\\\phebrowser*"])

When the image carries that component (its sys\\bin image is in the ROM arguments), this writes the stub the way the
release's makesis -s stubs are: z:\\system\\install\\<key>_stub.sis, package UID = the component's UID3 (the SIS's own
default), version 0.0.0 (every release is newer), install block = the listed files as z: paths and no data. SWI then
registers the package as in ROM, and an SA package of the same UID with the ROM-upgrade flag (make_sis.py --rom-upgrade)
may eclipse exactly those files (installationplanner.cpp IsValidEclipsingUpgradeL, IsEclipsableL); uninstalling it
brings the image's copy back.
"""
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
import components as C  # noqa: E402
import mmp as MMP  # noqa: E402
import sis as SIS  # noqa: E402


def main() -> int:
    args_file, out = sys.argv[1], sys.argv[2]
    rom_args = open(args_file, errors="replace").read().lower()
    os.makedirs(out, exist_ok=True)
    for key, comp in sorted(C.COMPONENTS.items()):
        stub = comp.get("rom_stub")
        if not stub:
            continue
        m = MMP.parse(os.path.join(MMP.BASE, comp.get("x32_mmp", comp["mmp"])))
        rom = C.rom_name(key, m.targettype)
        if not re.search(r"^--(?:xip|data)\n\s*sys\\bin\\" + re.escape(rom.lower()) + "=", rom_args, re.M):
            continue
        uid = stub.get("uid") or m.uids[-1]   # UID3
        files = ["z:\\" + f.replace("/", "\\") for f in stub["files"]]
        path = os.path.join(out, key + "_stub.sis")
        open(path, "wb").write(SIS.build_rom_stub(pkg_uid=uid, vendor=stub.get("vendor", "Phorepoc"), name=stub["name"],
                                                  version=(0, 0, 0), files=files))
        print("--data")
        print(f"system\\install\\{key}_stub.sis={path}")
        print(f"rom_stubs: {key} -> {path} (uid {uid:#x}, {len(files)} files)", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
