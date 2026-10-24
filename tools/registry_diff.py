# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""registry_diff -- compare two component registries entry by entry and list every difference in the fields that change what
gets built: defs, flags, drop, extra, x32_flags, vfp, libs, noinc, rom, rom_as, allow_wsd, static_ctors, cdriver, rtti, stack.

A file-by-file source comparison cannot see a compile macro or a dropped/extra source in a registry entry; one such entry
(`wsstdplugin_nga` defs=SYMBIAN_ROTATION_MODE_CHANGES) kept a game from starting on a public image. Run this whenever two
trees are supposed to build the same thing:

    python3 tools/registry_diff.py A.py[,B.py...] C.py[,D.py...] [--all]

Each side is a comma-separated list of registry files (their `"key": dict(...)` entries are merged). Path spelling
(os.path.join, _resolve_src, baseline/, OS_ROOT, REPO, ...) is normalised away; with --all the pure spelling differences are
listed too. Output: one line per component and field, `key field A=... | B=...`; exit status 1 when anything differs.
"""
import re
import sys

FIELDS = ("defs", "flags", "drop", "extra", "x32_flags", "vfp", "libs", "noinc", "rom", "rom_as", "allow_wsd",
          "static_ctors", "cdriver", "rtti", "stack", "heap_max", "capabilities")


def load(spec):
    text = ""
    for p in spec.split(","):
        with open(p, encoding="utf8", errors="replace") as f:
            text += "\n" + f.read()
    return text


def entries(text):
    out = {}
    ms = list(re.finditer(r'^    "([\w.\-]+)": dict\(', text, re.M))
    for i, m in enumerate(ms):
        out[m.group(1)] = text[m.start():ms[i + 1].start() if i + 1 < len(ms) else len(text)]
    return out


def norm(v):
    v = re.sub(r"os\.path\.join\(\w+,\s*", "(", v)
    v = re.sub(r"_resolve_src\(", "(", v)
    v = re.sub(r"\b(REPO|OS_ROOT|PHOREPOC|SYMBIAN|BUILD)\b", "R", v)
    v = re.sub(r"\s+", "", v.replace('"', "").replace("'", ""))
    v = v.replace("_PUBSHIMS+/", "toolchain/shims/").replace("baseline/", "").replace(".epoc32/", "").replace("core/os/", "")
    v = v.replace("src/phorepoc-arm/a64/hwrm/", "src/hwrm/")
    return v


def feats(seg, keep_spelling):
    out = {}
    for kw in FIELDS:
        m = re.search(r"\b" + kw + r"=(\[[^\]]*\]|True|False|0x[0-9a-fA-F]+|\d+|\"[^\"]*\")", seg)
        if m:
            out[kw] = m.group(1) if keep_spelling else norm(m.group(1))
    return out


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    keep = "--all" in sys.argv
    if len(args) != 2:
        sys.exit(__doc__)
    a, b = entries(load(args[0])), entries(load(args[1]))
    n = 0
    for k in sorted(set(a) | set(b)):
        if k not in a or k not in b:
            print("%-28s only in %s" % (k, "B" if k in b else "A"))
            n += 1
            continue
        fa, fb = feats(a[k], keep), feats(b[k], keep)
        for kw in FIELDS:
            if fa.get(kw) != fb.get(kw):
                print("%-28s %-12s A=%s | B=%s" % (k, kw, (fa.get(kw) or "-")[:100], (fb.get(kw) or "-")[:100]))
                n += 1
    print("%d difference(s)" % n)
    sys.exit(1 if n else 0)


if __name__ == "__main__":
    main()
