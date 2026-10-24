# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""make_sis -- package a registered component as an installable, self-signed SIS.

    python3 tools/make_sis.py <key> [<key> ...] [--uid 0x...] [--name "Caption"] [--out x.sis] [--unsigned] [--arm]

Several keys make one package (an app and its DLLs); the first names it (UID, caption, output file).

The same files the ROM would carry for that component, re-rooted on the install drive (`!:`):
its x32 image -> !:\\sys\\bin\\, its staged resources (`BUILD/romfiles/<rom>.romfiles`) and `rom_data` lines, and an
application registration file moved from \\private\\10003a3f\\apps (ROM) to \\private\\10003a3f\\import\\apps (the
path SWI takes registrations from). Signed with the developer key (`tools/emitter/sis.py --sign BUILD/dev-cert`).
The image's capability word goes into the file description (SWI checks it against the E32Image; a self-signed
package may only carry user-grantable capabilities -- swipolicy.ini). Build the ROM WITHOUT the component
(`ROM_EXCLUDE=<key>`), or the ROM file eclipse-blocks the install.
"""
import argparse
import os
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD, resolve  # noqa: E402
import components as C  # noqa: E402
import mmp as MMP  # noqa: E402
import sis as SIS  # noqa: E402

X32 = os.path.join(BUILD, "x32img")
ARM_IMG = os.path.join(BUILD, "arm", "img")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("keys", nargs="+", help="components; the first names the package (an app + its DLLs)")
    ap.add_argument("--uid", type=lambda x: int(x, 0), default=None, help="package UID (default: the image's UID3)")
    ap.add_argument("--name", default=None)
    ap.add_argument("--out", default=None)
    # Phorepoc installs unsigned packages (no app signing); the signed default stays for targets that still check signatures
    ap.add_argument("--unsigned", action="store_true", help="no signature (Phorepoc needs none)")
    # ARM: the images tools/emitter/build_mmp.py wrote (BUILD/arm/img) instead of x32img
    ap.add_argument("--arm", action="store_true", help="ARM images from BUILD/arm/img (build_mmp.py)")
    ap.add_argument("--version", default="1.0.0", help="package version major.minor.build (default 1.0.0)")
    ap.add_argument("--vendor", default="Phorepoc", help="vendor name shown by the installer")
    ap.add_argument("--file", action="append", default=[], metavar="HOST=TARGET",
                    help="an extra data file (licence text, notices): host path = target path on the device (!:/resource/Qt/LICENSE.LGPL; / becomes a backslash)")
    a = ap.parse_args()
    files = []
    for key in a.keys:
        comp = C.COMPONENTS[key]
        m = MMP.parse(os.path.join(MMP.BASE, comp.get("x32_mmp", comp["mmp"])))
        rom = C.rom_name(key, m.targettype)
        img = open(os.path.join(ARM_IMG if a.arm else X32, rom), "rb").read()
        img_caps = struct.unpack_from("<I", img, 0x88)[0]  # E32ImageHeaderV SSecurityInfo.iCaps, first word
        if key == a.keys[0]:
            uid3, caps = struct.unpack_from("<I", img, 8)[0], img_caps
        files.append((img, "!:\\sys\\bin\\" + rom, img_caps))
        lines = list(comp.get("rom_data", []))
        man = os.path.join(BUILD, "romfiles", rom + ".romfiles")
        if os.path.isfile(man):
            lines += open(man).read().split()
        for line in lines:
            target, _, host = line.partition("=")
            t = target.replace("/", "\\")
            if t.lower().startswith("private\\10003a3f\\apps\\"):
                t = "private\\10003a3f\\import\\apps\\" + t.split("\\")[-1]
            hp = host if os.path.isabs(host) else (os.path.join(BUILD, host) if os.path.isfile(os.path.join(BUILD, host)) else resolve(host))
            files.append((open(hp, "rb").read(), "!:\\" + t, None))
    for spec in a.file:
        host, _, target = spec.partition("=")
        files.append((open(host, "rb").read(), target.replace("/", chr(92)), None))
    uid = a.uid if a.uid is not None else uid3
    signer = None if a.unsigned else SIS.make_dev_signer(os.path.join(BUILD, "dev-cert"))
    # ARM: Symbian^3's product UID, else the installer warns "not compatible"
    pkg = SIS.build_package(files, app_uid=uid, vendor=a.vendor, app_name=a.name or a.keys[0], signer=signer, version=tuple(int(x) for x in a.version.split(".")),
                            platform_uid=0x20022E6D if a.arm else None)
    out = a.out or os.path.join(BUILD, "sis", a.keys[0] + ".sis")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    open(out, "wb").write(pkg)
    print(f"{out}: {len(pkg)} bytes, uid {uid:#x}, caps {caps:#x}, {len(files)} files")
    for _, t, _ in files:
        print("   ", t)
    return 0


if __name__ == "__main__":
    sys.exit(main())
