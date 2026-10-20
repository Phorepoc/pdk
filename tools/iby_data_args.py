# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""iby_data_args -- stage the plain data files the source's ROM recipe (.iby) lists for every component we ship.

The release's buildrom took each component's .iby: its binaries AND its data (DBMS policies .spd, backup registrations,
EAP/engine .conf/.xml, rules...). Our ROM recipe stages binaries + compiled resources per component, so data files
would otherwise be found one boot failure at a time (wsficons.mif, 2000cf30.spd). This closes that class:

    python3 tools/iby_data_args.py <args file>      # appends nothing; prints --data lines for what <args file> lacks

For every .iby that ships >= 1 binary already in <args file>, each data entry absent from the ROM is looked up in the
component's own package (by file name; a unique match, or the one under a data/ dir) and staged -- only SOURCE files:
build outputs (.rsc/.mif/.mbm, binaries) come from the component builds, central repository files from
tools/crml2cenrep.py.
"""
from __future__ import annotations

import os
import re
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import SYMBIAN  # noqa: E402

TREES = [os.path.join(SYMBIAN, "symbian-2010"), os.path.join(SYMBIAN, "symbian-rcl3")]

BUILD_OUTPUTS = (".rsc", ".r01", ".mif", ".mbm", ".dll", ".exe", ".agt", ".prt", ".tsy", ".csy", ".ldd", ".pdd",
                 ".fep", ".fxt", ".plg")

# the S60 ROM-recipe directory macros (epoc32/rom/include data_caging_paths_for_iby.hrh)
DIRS = {
    "APP_BITMAP_DIR": r"resource\apps", "APP_RESOURCE_DIR": r"resource\apps", "BITMAP_DIR": r"resource\apps",
    "RESOURCE_FILES_DIR": r"resource", "SHARED_LIB_DIR": r"sys\bin", "PROGRAMS_DIR": r"sys\bin",
    "ECOM_RESOURCE_DIR": r"resource\plugins", "ECOM_BIN_DIR": r"sys\bin", "REG_RESOURCE_DIR": r"private\10003a3f\apps",
    "REG_RESOURCE_IMPORT_DIR": r"private\10003a3f\import\apps", "FONTS_DIR": r"resource\fonts",
    "HELP_FILE_DIR": r"resource\help", "SKINS_DIR": r"resource\skins", "CENREP_DIR": r"private\10202be9",
}

# not data to stage: SIS upgrade stubs, EKA1 paths, Qt/Orbit (their own packages stage them), test recipes
# (system\install\ is NOT noise: the ROM stub .sis files there tell the installer which packages are pre-installed)
NOISE = ("system\\libs\\", "resource\\qt\\", "epocroot", "dataz_")


def norm(p: str) -> str:
    p = p.strip().strip('"').replace("/", "\\").lower()
    p = re.sub(r"^\\+", "", p)
    p = re.sub(r"^z:\\", "", p)
    return re.sub(r"\\+", r"\\", p)


def expand(p: str) -> str:
    for k, v in DIRS.items():
        p = p.replace(k, v)
    return norm(p)


def our_rom(args_file: str) -> set[str]:
    """The ROM paths an args file already carries (`<rom path>=<host path>` lines)."""
    out = set()
    for line in open(args_file, encoding="utf-8", errors="replace"):
        line = line.strip()
        if "=" in line and not line.startswith("--"):
            out.add(norm(line.split("=", 1)[0]))
    return out


def walk(exts: tuple[str, ...]):
    for t in TREES:
        for root, dirs, files in os.walk(t):
            dirs[:] = [d for d in dirs if d not in (".git", "tsrc", "test", "tests", "internal_test")]
            for f in files:
                if f.lower().endswith(exts):
                    yield os.path.join(root, f)


def iby_entries(text: str) -> tuple[list[str], list[str]]:
    """(binaries, data) ROM paths one .iby puts in Z:"""
    bins, data = [], []
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    for raw in text.splitlines():
        line = raw.split("//")[0].strip()
        if not line or line.startswith(("#", "REM", "rem")):
            continue
        m = re.match(r"(?:file|data|rofs\d?_?data|dll|primary|secondary|device|extension|variant)\s*=\s*(\S+)\s+(\S+)", line, re.I)
        if m:
            dest = expand(m.group(2))
            (bins if dest.startswith("sys\\bin\\") else data).append(dest)
            continue
        m = re.match(r"(\w+)\s*\(([^)]*)\)", line)
        if not m:
            continue
        argv = [x.strip() for x in m.group(2).split(",")]
        mac, a, b = m.group(1).upper(), argv[0], (argv[1] if len(argv) > 1 else "")
        if mac == "ECOM_PLUGIN" or mac == "ECOM_PLUGIN_UDEB":
            bins.append(norm("sys\\bin\\" + a))
            b = re.sub(r"\.rss$", ".rsc", b, flags=re.I)     # some .iby name the source; buildrom ships the .rsc
            data.append(norm("resource\\plugins\\" + b) + "|" + norm("resource\\plugins\\" + os.path.splitext(a)[0] + ".rsc"))
        elif mac == "S60_APP_EXE":
            bins.append(norm(f"sys\\bin\\{a}.exe"))
        elif mac == "S60_APP_RESOURCE":
            data.append(norm(f"resource\\apps\\{a}.rsc"))
        elif mac == "S60_APP_AIF_RSC":
            data.append(norm(f"private\\10003a3f\\apps\\{a}_reg.rsc") + "|" + norm(f"private\\10003a3f\\import\\apps\\{a}_reg.rsc"))
        elif mac == "S60_APP_AIF_ICONS":
            data.append(norm(f"resource\\apps\\{a}_aif.mif") + "|" + norm(f"resource\\apps\\{a}_aif.mbm"))
        elif mac in ("S60_APP_BITMAP", "SCALABLE_IMAGE"):
            # SCALABLE_IMAGE(dir, dir, name): the name is the last argument
            data.append(norm(f"resource\\apps\\{argv[-1]}.mif") + "|" + norm(f"resource\\apps\\{argv[-1]}.mbm"))
    return bins, data


def package_root(path: str) -> str:
    """SYMBIAN/<tree>/oss.FCL.sf.<layer>.<pkg>"""
    parts = os.path.relpath(path, SYMBIAN).replace("\\", "/").split("/")
    return os.path.join(SYMBIAN, *parts[:2])


_pkg_files: dict[str, dict[str, list[str]]] = {}


def find_in_package(pkg: str, name: str) -> str | None:
    if pkg not in _pkg_files:
        files: dict[str, list[str]] = {}
        for root, dirs, fs in os.walk(pkg):
            dirs[:] = [d for d in dirs if d.lower() not in (".git", "tsrc", "test", "tests", "internal_test")]
            for f in fs:
                files.setdefault(f.lower(), []).append(os.path.join(root, f))
        _pkg_files[pkg] = files
    hits = _pkg_files[pkg].get(name.lower(), [])
    if len(hits) == 1:
        return hits[0]
    data = [h for h in hits if re.search(r"[\\/](data|rom|conf|config)[\\/]", h, re.I)]
    return data[0] if len(data) == 1 else None


def main() -> None:
    args_file = sys.argv[1]
    rom = our_rom(args_file)
    rom_bins = {p.split("\\")[-1] for p in rom if p.startswith("sys\\bin\\")}
    staged: dict[str, str] = {}
    unresolved = 0
    for iby in walk((".iby",)):
        if re.search(r"test", os.path.basename(iby), re.I):
            continue
        # a BOARD's recipe (baseport/<board>/...) is not ours: its drive map (estart.txt), media and HAL data belong to that
        # board -- syborg's estart.txt mapped x86's C: to an empty local drive (KErrCorrupt on C:)
        if "/baseport/" in iby.replace("\\", "/"):
            continue
        try:
            bins, data = iby_entries(open(iby, encoding="utf-8", errors="replace").read())
        except OSError:
            continue
        if not any(b.split("\\")[-1] in rom_bins for b in bins):
            continue
        for d in data:
            dest = d.split("|")[0]
            if (dest in rom or dest in staged or any(n in dest for n in NOISE) or dest.endswith(BUILD_OUTPUTS)
                    or dest.startswith("private\\10202be9\\") or dest.lower() == "sys\\data\\estart.txt"):
                continue
            src = find_in_package(package_root(iby), dest.split("\\")[-1])
            if src:
                staged[dest] = os.path.abspath(src).replace("\\", "/")
            else:
                unresolved += 1
    for dest, src in sorted(staged.items()):
        print("--data")
        print(f"{dest}={src}")
    print(f"# iby_data_args: {len(staged)} data files staged from the .iby recipes, {unresolved} not found as source "
          f"files", file=sys.stderr)


if __name__ == "__main__":
    main()
