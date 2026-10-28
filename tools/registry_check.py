# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""registry_check -- every path the component registry names must exist: each component's .mmp, its added sources
(`extra`, `x32_extra`) and the include paths / forced includes in its `flags`. A path that only worked in another
checkout layout (a join of OS_ROOT with "baseline/...", a file the split left behind) shows up here before a build
fails on it.

    python3 tools/registry_check.py          (`ph component check`; exit 1 when a path is missing)
"""
from __future__ import annotations

import glob
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
import paths  # noqa: E402
import components as C  # noqa: E402

FLAG_PATHS = ("-I", "-iquote", "-idirafter", "-include", "-isystem")


def mmp_path(mmp: str) -> str:
    if os.path.isabs(mmp):
        return mmp
    if mmp.startswith("baseline/"):
        return paths.resolve(mmp)
    return os.path.normpath(os.path.join(paths.SYMBIAN, "symbian-2010", mmp))


def main() -> int:
    # x32-only settings name the x86-64 port's files: checked when that port is among the layers
    x32 = os.path.isdir(os.path.join(paths.OS_ROOT, "toolchain", "x32"))
    missing = []
    for key, comp in sorted(C.COMPONENTS.items()):
        if not isinstance(comp, dict):
            continue
        for field in ("mmp", "x32_mmp") if x32 else ("mmp",):
            if comp.get(field) and not os.path.isfile(mmp_path(comp[field])):
                missing.append((key, field, comp[field]))
        flags = list(comp.get("flags") or [])
        for i, f in enumerate(flags[:-1]):
            p = flags[i + 1]
            # the toolchain's own directories (GCC's include dir) are the container's business
            if f in FLAG_PATHS and os.path.isabs(p) and not p.startswith("/usr/") and not os.path.exists(p):
                missing.append((key, f, p))
        for field in ("extra", "x32_extra") if x32 else ("extra",):
            for e in comp.get(field) or []:
                p = paths.resolve(e)
                if not (glob.glob(p) if "*" in p else os.path.exists(p)):
                    missing.append((key, field, e))
    n = sum(1 for c in C.COMPONENTS.values() if isinstance(c, dict))
    print(f"registry_check: {n} components, {len(missing)} missing paths")
    for key, field, p in missing:
        print(f"  {key:32} {field:10} {p}")
    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
