# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""romgate -- static "who will hit an unimpl stub" report for the XIP bundle.

For every built ARM image (BUILD/arm/device for the core set, BUILD/arm/img for the registry components), walk its import table and flag each import
whose provider export slot is the provider's __unimpl_export stub (= the most
repeated address in that provider's export table). Names come from the .def
files. Catches the next EUSRNIMP panic BEFORE a boot does.

    python3 tools/romgate.py [--gui] [key ...]   # summary per image + the stubbed names
                                   # --gui: the minigui bundle
                                   # key ...: report ONLY these build_mmp components (implies
                                   # --gui; every built component is loaded as a provider)
"""
import collections, os, struct, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
sys.path.insert(0, HERE)
from paths import BUILD
import rombuild
import devlink
from euser_def import parse_def

DEV = os.path.join(BUILD, "arm", "device")      # the core set (build_*_device.py / build_*_exe.py)
EM = os.path.join(BUILD, "arm", "img")          # the registry components (build_mmp.py)
IMAGES = {  # rom name -> built file
    "euser.dll": "euser.dll", "efsrv.dll": "efsrv.dll", "hal.dll": "hal.dll",
    "efile.exe": "efile.exe", "estart.exe": "estart.exe", "elocal.fsy": "elocal.fsy",
    "ewsrv.exe": "ewsrv.exe", "edisp.dll": "edisp.dll", "ekdata.dll": "ekdata.dll",
    "econs.dll": "econs.dll", "eshell.exe": "eshell.exe",
}
GUI_DLLS = ["palette", "lookuptable", "linebreak", "estor", "bsul", "centralrepository",
            "bafl", "ecompatchdata", "ecom", "gdi", "scdv", "surfaceupdateclient", "sgresource", "fbsrasterizer",
            "fntstr", "fbscli", "bitgdi", "ektran", "ws32", "wsgraphicdrawer",
            "domaincli", "2001b70b"]
ONLY = [a for a in sys.argv[1:] if not a.startswith("-")]
if ONLY:
    sys.argv.append("--gui")
    import components as _C, mmp as _MMP
    for _k, _c in _C.COMPONENTS.items():
        _rom = _C.rom_name(_k, _MMP.parse(os.path.join(_MMP.BASE, _c["mmp"])).targettype)
        IMAGES.setdefault(_rom, _rom)
    ONLY = {_C.rom_name(k, _MMP.parse(os.path.join(_MMP.BASE, _C.COMPONENTS[k]["mmp"])).targettype) for k in ONLY}
if "--gui" in sys.argv:
    IMAGES.update({d + ".dll": d + ".dll" for d in GUI_DLLS})
    IMAGES.update({"ecomserver.exe": "ecomserver.exe", "fbserv.exe": "fbserv.exe", "ewsrv.exe": "wserv.exe",
                   "econs.dll": "econswserv.dll"})
    IMAGES.pop("edisp.dll", None)

DEFS = {"euser.dll": devlink.BE.DEF_PATH, "efsrv.dll": devlink.EFSRV_DEF,
        "hal.dll": devlink.HAL_DEF, "efile.exe": devlink.EFILE_DEF}


def main():
    imgs = {}
    for rom_name, fn in IMAGES.items():
        p = next((q for q in (os.path.join(DEV, fn), os.path.join(EM, fn)) if os.path.exists(q)), "")
        if p:
            imgs[rom_name] = rombuild.E32Xip(p, rom_name)
    stub = {}
    for n, im in imgs.items():
        meta = os.path.join(EM, IMAGES[n] + ".meta")
        if os.path.exists(meta):                 # build_mmp sidecar: exact stub address
            stub[n] = int(open(meta).read().split("=")[1], 16)
        elif len(im.exports) > 20:
            addr, cnt = collections.Counter(im.exports).most_common(1)[0]
            if cnt > 1:
                stub[n] = addr
    names = {n: parse_def(p).by_ordinal for n, p in DEFS.items() if os.path.exists(p)}
    if "--gui" in sys.argv:                      # .def names for the build_mmp components
        import components as C, mmp as MMP
        for k, c in C.COMPONENTS.items():
            m = MMP.parse(os.path.join(MMP.BASE, c["mmp"]))
            dp = os.path.join(MMP.BASE, c["def"]) if "def" in c else m.def_path()
            if dp:
                names[C.rom_name(k, m.targettype)] = parse_def(dp).by_ordinal
    total = 0
    for n, im in imgs.items():
        if ONLY and n not in ONLY:
            continue
        bad = []
        for dll, offs in im.imports:
            key = dll.split("{")[0].lower()
            tgt = imgs.get(key)
            if tgt is None or key not in stub:
                continue
            for off in offs:
                ordn = struct.unpack_from("<I", im.code, off)[0] & 0xFFFF
                if tgt.exports[ordn - 1] == stub[key]:
                    bad.append("%s@%d %s" % (key, ordn, names.get(key, {}).get(ordn, "?")))
        bad = sorted(set(bad))
        total += len(bad)
        print("%-12s %d stubbed imports" % (n, len(bad)))
        for b in bad:
            print("    " + b)
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
