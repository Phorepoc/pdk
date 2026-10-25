# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""case_shim -- a component's own headers under the other spellings casefix knows (both builders use it).

On Windows a component's own header wins under any spelling ("UiEvent.h" = stmgesturefw's inc/uievent.h). On Linux
that name misses the component's folders and casefix (-idirafter, last) answers it with another package's header
(WebCore's UIEvent.h). So each header of the source's own folder and the component's include folders (not epoc32 or
casefix) is linked into a shim dir under every other spelling casefix knows, unless that spelling exists exactly on
the include path, where it would have been found anyway. The shim goes on -I ahead of casefix.
"""
from __future__ import annotations

import os

_CASEFIX_NAMES: dict = {}
_SHIMS: dict = {}


def include_dirs(flags: list[str], base: str) -> list[str]:
    """-I/-iquote dirs of a flag list, in either form ("-I", "x" or "-Ix"), made absolute against base."""
    out = []
    for i, f in enumerate(flags):
        if f in ("-I", "-iquote") and i + 1 < len(flags):
            d = flags[i + 1]
        elif f.startswith("-iquote") and len(f) > 7:
            d = f[7:]
        elif f.startswith("-I") and len(f) > 2:
            d = f[2:]
        else:
            continue
        out.append(os.path.normpath(os.path.join(base, d)))
    return out


def make(src_dir: str, dirs: list[str], shim: str, casefix: str, epoc: str) -> bool:
    """Fill shim for one include-path configuration (cached per shim). True if the shim has any link."""
    if shim in _SHIMS:
        return _SHIMS[shim]
    if not _CASEFIX_NAMES and os.path.isdir(casefix):
        for n in os.listdir(casefix):
            _CASEFIX_NAMES.setdefault(n.lower(), []).append(n)
    path = [os.path.normpath(d) for d in [src_dir, *dirs]]
    own = [d for d in path if not d.startswith((epoc, casefix)) and os.path.isdir(d)]
    made = False
    for d in own:
        for name in sorted(os.listdir(d), key=str.lower):
            for other in _CASEFIX_NAMES.get(name.lower(), ()):
                if other == name or os.path.lexists(os.path.join(shim, other)):
                    continue
                if any(os.path.exists(os.path.join(p, other)) for p in path):
                    continue
                os.makedirs(shim, exist_ok=True)
                try:
                    os.symlink(os.path.join(d, name), os.path.join(shim, other))
                except FileExistsError:         # (another TU of the same component, in parallel)
                    pass
                made = True
    _SHIMS[shim] = made or os.path.isdir(shim)
    return _SHIMS[shim]
