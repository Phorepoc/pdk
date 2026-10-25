# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""paths -- where everything lives, for every tool in tools/emitter (and tools/*.py).

    PDK       the pdk checkout (this repository)
    CORE      the core checkout: the OS and its registry (registry/components*.py, rom_heap.tsv, mif_dirs.txt).
              PHOREPOC_CORE, default <pdk>/../core.  <core>/registry is put on sys.path (import components)
    OS_ROOT   the Phorepoc OS tree: toolchain/ (shims, overlay, kinc, generated, dll_rt ...), src/ (our components),
              the board and port directories.  PHOREPOC_OS_ROOT, default <core>/os.
    ADAPTATIONS   the adaptation checkouts built with it (PHOREPOC_ADAPTATIONS, os.pathsep-separated): each brings an
              os/ tree laid out like core's and a registry/ with its own components. With any, OS_ROOT is a merged
              view BUILD/osview (core's os/ + every adaptation's os/, as symlinks; a path in two layers is an
              error) and their registry dirs are on sys.path (registry/components.py merges them: ADAPTATION_REGISTRIES).
    SYMBIAN   the Symbian source: symbian-2010/<package>/ and symbian-rcl3/<package>/ (fetch.sh puts them in
              <core>/symbian).  PHOREPOC_SYMBIAN
    BUILD     all generated files.  PHOREPOC_BUILD, default <core>/build
    EPOC32_ROOT   BUILD/epoc32: the exported include tree (tools/epoc32_export.py)

Registry paths are relative; resolve() turns one into an absolute path:
    baseline/<x>   -> SYMBIAN/<x>      (the registry still says baseline/symbian-2010/...; also accepts symbian-2010/...)
    thirdparty/<x>, .<x>/ -> BUILD/<x> (prebuilt third-party archives, generated icons ...)
    anything else  -> OS_ROOT/<x>
"""
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
PDK = os.path.dirname(os.path.dirname(HERE))
CORE = os.path.abspath(os.environ.get("PHOREPOC_CORE") or os.path.join(PDK, "..", "core"))
REGISTRY = os.path.join(CORE, "registry")
if REGISTRY not in sys.path:
    sys.path.insert(0, REGISTRY)


def _dir(env: str, default: str) -> str:
    return os.path.abspath(os.environ.get(env) or default)


OS_ROOT = _dir("PHOREPOC_OS_ROOT", os.path.join(CORE, "os"))
SYMBIAN = _dir("PHOREPOC_SYMBIAN", os.path.join(CORE, "symbian"))
BUILD = _dir("PHOREPOC_BUILD", os.path.join(CORE, "build"))
EPOC32_ROOT = os.path.join(BUILD, "epoc32")
ADAPTATIONS = [os.path.abspath(a) for a in os.environ.get("PHOREPOC_ADAPTATIONS", "").split(os.pathsep) if a]
ADAPTATION_REGISTRIES = [os.path.join(a, "registry") for a in ADAPTATIONS if os.path.isdir(os.path.join(a, "registry"))]
for _r in ADAPTATION_REGISTRIES:
    if _r not in sys.path:
        sys.path.append(_r)


def _osview(layers: list) -> str:
    """BUILD/osview: one tree of symlinks to the files of every layer (core's os/, then each adaptation's os/).
    Rebuilt only when the file lists change; built aside and swapped in, so parallel tools never see half a tree."""
    import hashlib
    files = {}
    for layer in layers:
        for root, dirs, names in os.walk(layer):
            dirs.sort(key=str.lower); names.sort(key=str.lower)   # NTFS order on every file system (see epoc32_export.py)
            dirs[:] = [d for d in dirs if d != ".git"]
            for n in names:
                p = os.path.join(root, n)
                rel = os.path.relpath(p, layer)
                if rel in files:
                    raise SystemExit(f"paths: {rel} is in two layers: {files[rel]} and {p}")
                files[rel] = p
    view = os.path.join(BUILD, "osview")
    stamp = hashlib.sha1("\n".join(f"{k}\t{v}" for k, v in sorted(files.items())).encode()).hexdigest()
    try:
        if open(os.path.join(view, ".osview")).read() == stamp:
            return view
    except OSError:
        pass
    tmp = f"{view}.{os.getpid()}"
    for rel, p in files.items():
        os.makedirs(os.path.dirname(os.path.join(tmp, rel)), exist_ok=True)
        os.symlink(p, os.path.join(tmp, rel))
    with open(os.path.join(tmp, ".osview"), "w") as f:
        f.write(stamp)
    old = f"{view}.old.{os.getpid()}"
    if os.path.isdir(view):
        os.rename(view, old)
    os.rename(tmp, view)
    if os.path.isdir(old):
        import shutil
        shutil.rmtree(old, ignore_errors=True)
    return view


if ADAPTATIONS:
    OS_ROOT = _osview([OS_ROOT] + [os.path.join(a, "os") for a in ADAPTATIONS if os.path.isdir(os.path.join(a, "os"))])


def prefix_maps() -> list:
    """-fmacro-prefix-map flags: __FILE__ (panic file names, asserts) the same whatever the checkout or mount points,
    so builds from different machines compare byte for byte. GCC tries the LAST map first: the general ones go
    first (BUILD before the osview inside it)."""
    layers = [os.path.join(a, "os") for a in ADAPTATIONS] + [_dir("PHOREPOC_OS_ROOT", os.path.join(CORE, "os")), OS_ROOT]
    maps = [(SYMBIAN, ""), (BUILD, "build/")] + [(d, "os/") for d in layers]
    return [f"-fmacro-prefix-map={d}/={to}" for d, to in maps]


def resolve(rel: str) -> str:
    if os.path.isabs(rel):
        return rel
    rel = rel.replace("\\", "/")
    if rel.startswith("baseline/"):
        return os.path.join(SYMBIAN, rel[len("baseline/"):])
    if rel.startswith(("thirdparty/", ".")):
        return os.path.join(BUILD, rel.lstrip("."))
    return os.path.join(OS_ROOT, rel)


if __name__ == "__main__":
    print(OS_ROOT)      # shell scripts: OS_ROOT="$(python3 tools/emitter/paths.py)" (the merged view with adaptations)
