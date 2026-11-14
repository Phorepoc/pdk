# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

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
import mmp as MMP  # noqa: E402
import sis as SIS  # noqa: E402
import e32image as E32  # noqa: E402

X32 = os.path.join(BUILD, "x32img")
ARM_IMG = os.path.join(BUILD, "arm", "img")
X32_S4 = os.path.join(BUILD, "qt48", "x32img")
try:
    S4_KEYS = set(__import__("components_s4").COMPONENTS) if os.environ.get("PHOREPOC_S4") == "1" else set()
except ImportError:
    S4_KEYS = set()


def component_files(keys, arm, sis=True):
    r"""The files an image carries for these components: ([(bytes, target, caps)], first component's UID3, its caps).
    Targets are Windows paths on the install drive (!:\sys\bin\x.exe); with sis=False the ROM paths themselves (sys\bin\x.exe,
    an application registration stays in private\10003a3f\apps). Shared with tools/addon.py: an add-on archive and a SIS
    place a component's files the same way."""
    import components as C  # the component registry (loads the toolchain tree): not needed by a --data-only package
    root = "!:\\" if sis else ""
    files = []
    uid3 = caps = None
    for key in keys:
        comp = C.COMPONENTS[key]
        m = MMP.parse(os.path.join(MMP.BASE, comp.get("x32_mmp", comp["mmp"])))
        if m.targettype.lower() == "lib":
            continue    # a static library (qtmain.lib) is built for the package's executables to link: nothing to install
        rom = C.rom_name(key, m.targettype)
        # the Symbian^4 set (components_s4, Qt 4.8) builds into its own root: BUILD/qt48/x32img
        d = X32_S4 if (not arm and key in S4_KEYS) else (ARM_IMG if arm else X32)
        if not arm and not comp.get("sis"):
            # without sis=True the image keeps its %rip->static displacements for the ROM prelinker only: loaded from RAM, its
            # first write to a static lands next to the code (build_mmp_x32.py KX32PcRelRelocType)
            sys.exit(f"make_sis: {key} has no sis=True in its registry entry: its x32 image cannot be loaded from RAM")
        img = open(os.path.join(d, rom), "rb").read()
        if not arm and os.environ.get("ALLCAPS") != "1" and len(img) >= 0x90:
            # x32 images are built with every capability; the ROM builder stamps the mmp's (x32_rom_args.py caps.tsv). An installed
            # image gets the same: its header and the SIS file description must both say the mmp's CAPABILITY, or SWI refuses the
            # file ("Executable capabilities do not match header capabilities", -30475)
            img = bytearray(img)
            struct.pack_into("<II", img, 0x88, m.cap_bits() & 0xFFFFFFFF, 0)
            E32.set_header_crc(img)
            img = bytes(img)
        img_caps = struct.unpack_from("<I", img, 0x88)[0]  # E32ImageHeaderV SSecurityInfo.iCaps, first word
        if uid3 is None:    # the first component with an image (a leading static library has none)
            uid3, caps = struct.unpack_from("<I", img, 8)[0], img_caps
        files.append((img, root + "sys\\bin\\" + rom, img_caps))
        if m.targettype == "plugin":
            # an ECOM plug-in is found through its registration resource (compile_registration: BUILD/plugins/<stem>.rsc), which
            # the ROM carries in \resource\plugins; installed, it goes to the same folder of the install drive
            reg = os.path.join(BUILD, "plugins", os.path.splitext(rom)[0] + ".rsc")
            if not os.path.isfile(reg):
                sys.exit(f"make_sis: {key} is an ECOM plug-in but {reg} (its registration) was not built: run the resource step first")
            files.append((open(reg, "rb").read(), root + "resource\\plugins\\" + os.path.basename(reg), None))
        lines = list(comp.get("rom_data", []))
        man = os.path.join(BUILD, "romfiles", rom + ".romfiles")
        if os.path.isfile(man):
            lines += open(man).read().split()
        for line in lines:
            target, _, host = line.partition("=")
            t = target.replace("/", "\\")
            if sis and t.lower().startswith("private\\10003a3f\\apps\\"):
                t = "private\\10003a3f\\import\\apps\\" + t.split("\\")[-1]
            hp = host if os.path.isabs(host) else (os.path.join(BUILD, host) if os.path.isfile(os.path.join(BUILD, host)) else resolve(host))
            files.append((open(hp, "rb").read(), root + t, None))
    return files, uid3, caps


def check_targets(files) -> None:
    """Refuse a target that is not a device path: '<drive or !>:/...' with no second drive in it. A Windows shell (Git Bash's
    MSYS argument conversion) rewrites '!:/resource/x' into '!;C:/Program Files/Git/resource/x', which the installer
    answers only with KErrSISInvalidTargetFile (-10146) at install time."""
    bad = [t for _, t, _ in files if len(t) < 3 or t[1:3] != ":" + chr(92) or ":" in t[2:] or not (t[0] == "!" or t[0].isalpha())]
    if bad:
        raise SystemExit("make_sis: not a device target path (a host path got into it; from Git Bash set MSYS2_ARG_CONV_EXCL='*'): "
                         + ", ".join(bad[:5]))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("keys", nargs="*", help="components; the first names the package (an app + its DLLs)")
    ap.add_argument("--data-only", action="store_true", help="no components: a package of --file data only (a theme); needs --uid and --name")
    ap.add_argument("--uid", type=lambda x: int(x, 0), default=None, help="package UID (default: the image's UID3)")
    ap.add_argument("--name", default=None)
    ap.add_argument("--out", default=None)
    # Phorepoc installs unsigned packages (no app signing); the signed default stays for targets that still check signatures
    ap.add_argument("--unsigned", action="store_true", help="no signature (Phorepoc needs none)")
    # ARM: the images tools/emitter/build_mmp.py wrote (BUILD/arm/img) instead of x32img
    ap.add_argument("--arm", action="store_true", help="ARM images from BUILD/arm/img (build_mmp.py)")
    ap.add_argument("--version", default="1.0.0", help="package version major.minor.build (default 1.0.0)")
    ap.add_argument("--vendor", default="Phorepoc", help="vendor name shown by the installer")
    ap.add_argument("--depends", action="append", default=[], metavar="UID,MAJ.MIN.BUILD,NAME",
                    help="the package needs an installed package of this uid, version >= x (e.g. 0x2001E61C,4.7.3,Qt)")
    ap.add_argument("--file", action="append", default=[], metavar="HOST=TARGET",
                    help="an extra data file (licence text, notices): host path = target path on the device (!:/resource/Qt/LICENSE.LGPL; / becomes a backslash)")
    ap.add_argument("--dir", action="append", default=[], metavar="HOSTDIR=TARGETDIR",
                    help="every file directly in a host directory, as --file HOSTDIR/f=TARGETDIR/f (a theme's images: too many for one command line)")
    ap.add_argument("--embed", action="append", default=[], metavar="UID,TYPE,MAJ.MIN.BUILD,NAME,HOST=TARGET[,HOST=TARGET...]",
                    help="an embedded package (makesis @\"x.sis\",(uid)), e.g. 0x10202BE9,sp,1.0.0,Orbit settings,a.txt=c:/private/10202be9/a.txt: a TYPE=SP "
                    "package of Central Repository files against its ROM stub (CenRepStub.sis)")
    ap.add_argument("--type", choices=["sa", "sp", "pu"], default="sa", help="SIS install type: sa (full), sp (patch), pu (partial upgrade of an installed package)")
    ap.add_argument("--run-on-install", action="append", default=[], metavar="TARGET",
                    help="an installed executable (e.g. !:/sys/bin/x.exe) that the installer also runs when the install has finished (makesis FR,RA); "
                    "the installer's policy AllowRunOnInstallUninstall must allow it")
    ap.add_argument("--rom-upgrade", action="store_true",
                    help="the ROM-upgrade flag (makesis RU): the package may replace the files of an image's copy that a ROM stub of the same "
                    "UID lists (Z:/system/install/<name>_stub.sis); a device without that copy installs it as usual")
    a = ap.parse_args()
    if a.data_only:
        if a.keys or a.uid is None or not a.name or not a.out:
            ap.error("--data-only takes no components and needs --uid, --name and --out")
        files, uid3, caps = [], None, 0
    elif not a.keys:
        ap.error("no components (--data-only for a package of data files only)")
    else:
        files, uid3, caps = component_files(a.keys, a.arm)
    for spec in a.dir:
        host, _, target = spec.partition("=")
        a.file += [os.path.join(host, f) + "=" + target.rstrip("/") + "/" + f for f in sorted(os.listdir(host)) if os.path.isfile(os.path.join(host, f))]
    for spec in a.file:
        host, _, target = spec.partition("=")
        files.append((open(host, "rb").read(), target.replace("/", chr(92)), None))
    check_targets(files)
    SIS.RUN_AFTER_INSTALL.update(t.replace("/", chr(92)) for t in a.run_on_install)
    uid = a.uid if a.uid is not None else uid3
    embedded = []
    for spec in a.embed:
        parts = spec.split(",")
        efiles = [(open(h, "rb").read(), t.replace("/", chr(92)), None) for h, _, t in (x.partition("=") for x in parts[4:])]
        check_targets(efiles)
        embedded.append(dict(files=efiles, app_uid=int(parts[0], 0), app_name=parts[3], version=tuple(int(x) for x in parts[2].split(".")),
                             install_type={"sa": 0, "sp": 1, "pu": 2}[parts[1]]))
    signer = None if a.unsigned else SIS.make_dev_signer(os.path.join(BUILD, "dev-cert"))
    # ARM: Symbian^3's product UID, else the installer warns "not compatible"
    pkg = SIS.build_package(files, app_uid=uid, vendor=a.vendor, app_name=a.name or a.keys[0], signer=signer, version=tuple(int(x) for x in a.version.split(".")),
                            platform_uid=0x20022E6D if a.arm else None,
                            dependencies=[(int(d.split(",")[0], 0), tuple(int(x) for x in d.split(",")[1].split(".")), d.split(",", 2)[2])
                                          for d in a.depends],
                            install_type={"sa": 0, "sp": 1, "pu": 2}[a.type], embedded=embedded,
                            install_flags=SIS.INSTALL_FLAG_ROM_UPGRADE if a.rom_upgrade else 0)
    out = a.out or os.path.join(BUILD, "sis", a.keys[0] + ".sis")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    open(out, "wb").write(pkg)
    print(f"{out}: {len(pkg)} bytes, uid {uid:#x}, caps {caps:#x}, {len(files)} files")
    for _, t, _ in files:
        print("   ", t)
    return 0


if __name__ == "__main__":
    sys.exit(main())
