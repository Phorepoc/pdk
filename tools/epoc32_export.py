# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""epoc32_export -- build a real epoc32/include tree from bld.inf PRJ_EXPORTS.

The 2010 source has no epoc32/include: every component EXPORTS its public /
platform headers there at build time (bld.inf PRJ_EXPORTS). Everything above
kernelhwsrv (estor, bafl, gdi, fbserv, wserv, ...) includes its neighbours via
that tree, so to compile them unmodified we do what `abld export` does:
preprocess each bld.inf (platform_paths.hrh macros), walk PRJ_EXPORTS, and copy
each header to its destination under $PHOREPOC_BUILD/epoc32/include.

    python3 tools/epoc32_export.py                       # the default OS-layer package set
    python3 tools/epoc32_export.py oss.FCL.sf.os.graphics [more packages...]
    python3 tools/epoc32_export.py --all            # every package of both source trees

Only destinations under /epoc32/include are honoured (no rom/ iby, data, zips).
"""
import filecmp, os, re, shutil, subprocess, sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "emitter"))
from paths import SYMBIAN, EPOC32_ROOT, OS_ROOT  # noqa: E402

BASE = os.path.join(SYMBIAN, "symbian-2010")
OUT = os.path.join(EPOC32_ROOT, "include")
CPP = os.environ.get("ARM_CPP", "arm-none-eabi-cpp")
PATHS_DIR = os.path.join(BASE, "oss.FCL.sf.os.deviceplatformrelease", "symbianosbld", "cedarutils")
DEFAULT_PKGS = [
    "oss.FCL.sf.os.deviceplatformrelease", "oss.FCL.sf.os.kernelhwsrv",
    "oss.FCL.sf.os.ossrv", "oss.FCL.sf.os.persistentdata", "oss.FCL.sf.os.textandloc",
    "oss.FCL.sf.os.graphics", "oss.FCL.sf.os.devicesrv", "oss.FCL.sf.os.security",
    "oss.FCL.sf.adapt.Qemu", "oss.FCL.sf.os.mmaudio", "oss.FCL.sf.os.mm", "oss.FCL.sf.os.mmimaging",
    "oss.FCL.sf.adapt.NokiaMMAudioReference",
    # client headers the IP layer includes
    "oss.FCL.sf.os.commsfw", "oss.FCL.sf.os.networkingsrv", "oss.FCL.sf.os.cellularsrv",
    # middleware layer: the application framework (cone/uikon/avkon, apparc, FEP)
    "../symbian-rcl3/oss.FCL.sf.mw.classicui", "../symbian-rcl3/oss.FCL.sf.mw.appsupport", "../symbian-rcl3/oss.FCL.sf.mw.inputmethods",
    "../symbian-rcl3/oss.FCL.sf.mw.uiresources",
]
# never part of a device build: the PC emulator's host graphics, and the API reference (a copy of an old SDK epoc32/include:
# casefix linked avkon.rsg there, without the RCL_3 resource ids)
HOST_ONLY_PKGS = {"oss.FCL.sf.adapt.SimulatorGraphics", "oss.API_REF.Public_API"}
# generated SDK headers no bld.inf exports, taken from the API reference's prebuilt copy (only these: the rest of it is
# an old SDK). featureuids.h: the feature UIDs header the SDK generated from the S60 feature list (NFeature::KFmTx ...);
# without it casefix linked the ROM tools' much older featureUIDs.h and hwrmserver failed.
PREBUILT = ["featureuids.h"]
SKIP_DIRS = {"test", "tsrc", "tests", "documentation", "examples", "internal"}
SECTION = re.compile(r"^\s*PRJ_(\w+)", re.I)


def all_packages():
    """the default set first, then every other package of both trees (symbian-2010, then symbian-rcl3: a later
    package replaces a same-named header of an earlier one, so the RCL_3 line wins as it does in the build).
    Not the PC emulator's host graphics (its EGL/eglplatform.h, found through casefix, broke sfopenvg's EGLint)."""
    base = os.path.dirname(BASE)
    rest = [os.path.join("..", t, d) for t in ("symbian-2010", "symbian-rcl3")
            for d in sorted(os.listdir(os.path.join(base, t))) if d not in HOST_ONLY_PKGS]
    # an RCL_3 package of the default set runs AGAIN in its place at the end: otherwise its symbian-2010 twin, later in
    # the list, replaced its headers (classicui's CAknItemActionMenu without MarkingMode -> avkon, eikcoctl ... failed)
    have = {os.path.normpath(os.path.join(BASE, p)) for p in DEFAULT_PKGS if "symbian-rcl3" not in p}
    order = DEFAULT_PKGS + [p for p in rest if os.path.normpath(os.path.join(BASE, p)) not in have]
    # ... and the other way round: a symbian-2010 package of the default set (the line the build compiles, e.g.
    # os.devicesrv) runs again after its RCL_3 twin, or that twin's older headers win (ssmstateawaresession.h without
    # CSsmStateAwareSession2 -> ssmcmn.dll failed, SSM ran on stubs). RCL_3-only headers stay.
    twins = {os.path.basename(p) for p in rest if "symbian-rcl3" in p}
    again = [p for p in DEFAULT_PKGS if "symbian-rcl3" not in p and os.path.basename(p) in twins
             and os.path.join("..", "symbian-rcl3", os.path.basename(p)) not in DEFAULT_PKGS]
    return order + again


def _variant(path):
    s60 = os.path.join(OUT, "variant", "s60_variant.hrh")
    if re.search(r"oss\.FCL\.sf\.(mw|app)\.", path.replace("\\", "/")) and os.path.isfile(s60):
        return s60
    return os.path.join(PATHS_DIR, "inc", "Symbian_OS_tb102sf.hrh")


def preprocess(path):
    # (app.webuis) is missing here and STOPS cpp -- the whole package exported nothing. Rewrite such includes
    # and give the markers back the real file name (exports() tracks the current dir from them).
    import mmp as _MMP
    d = os.path.dirname(os.path.abspath(path))
    text = open(path, errors="replace").read()

    def fix(m):
        full = os.path.normpath(os.path.join(d, m.group(2).replace("\\", "/")))
        if os.path.exists(full):
            return m.group(0)
        ci = _MMP.nocase(full)          # written for a case-insensitive file system (sf.mw.web: Browser_platform_variant.hrh)
        if ci:
            return m.group(1) + ci.replace("\\", "/") + m.group(3)
        mapped = _MMP.map_sf_path(full, d)
        return m.group(1) + mapped.replace("\\", "/") + m.group(3) if mapped != full else m.group(0)
    fixed = _MMP._REL_INCLUDE.sub(fix, text)
    src, stdin = (path, None) if fixed == text else ("-", fixed)
    r = subprocess.run([CPP, "-undef", "-nostdinc", "-x", "c", "-w",
                        "-DEPOC32", "-DMARM", "-DEABI", "-DMARM_ARMV5", "-DGCCE",
                        "-D__GNUC__=3", "-DSBSV2", *([] if os.environ.get("EPOC32_NEW_LOCATION") == "1" else ["-DSYMBIAN_OLD_EXPORT_LOCATION"]),
                        "-I", PATHS_DIR, "-I", OUT, "-I", os.path.dirname(path),
                        # the product-variant feature flags gate some exports
                        # (e.g. SYMBIAN_ENABLE_64_BIT_FILE_SERVER_API -> f32file64.h)
                        "-include", _variant(path),
                        *_MMP.sf_mirror_incs(), "-idirafter", os.path.join(os.path.dirname(EPOC32_ROOT), "casefix"),
                        "-include", os.path.join(PATHS_DIR, "platform_paths.hrh"), src],
                       input=stdin, cwd=d if stdin is not None else None,
                       capture_output=True, text=True, errors="replace")
    out = r.stdout
    if stdin is not None:
        out = out.replace('"<stdin>"', '"' + path.replace("\\", "\\\\") + '"')
    return out


def preprocessed_lines(bldinf):
    """the preprocessed bld.inf as lines, with a backslash-continued export ("../data/x.xml\\" + an indented
    "    /epoc32/data/z/..." line) joined back into one: the preprocessor drops the backslash but keeps the line break
    (cfwplugins/cenrepsourceplugin, 2026-10-10). Shared by exports() and tools/zdata_gen.py."""
    lines = preprocess(bldinf).splitlines()
    marker = re.compile(r'^#\s*\d+\s+"')
    joined, i = [], 0
    while i < len(lines):
        ln = lines[i]
        if ln.strip() and not ln.lstrip().startswith("#") and len(ln.split()) == 1:
            j = i + 1									# the preprocessor may also insert line markers after a splice
            while j < len(lines) and (not lines[j].strip() or marker.match(lines[j])):
                j += 1
            if j < len(lines) and lines[j][:1].isspace() and len(lines[j].split()) == 1 and "/" in lines[j]:
                joined.append(ln.strip() + " " + lines[j].strip())
                i = j + 1
                continue
        joined.append(ln)
        i += 1
    return joined


def _mmp():
    import mmp
    return mmp


def exports(bldinf):
    """yield (src_abs, dest_rel_to_epoc32_include) for one bld.inf."""
    cur_dir, section = os.path.dirname(bldinf), ""
    for ln in preprocessed_lines(bldinf):
        m = re.match(r'^#\s*\d+\s+"([^"]+)"', ln)
        if m:
            f = m.group(1).replace("\\\\", "\\")
            if not f.startswith("<"):
                cur_dir = os.path.dirname(os.path.abspath(f))
            continue
        m = SECTION.match(ln)
        if m:
            section = m.group(1).upper()
            continue
        if section not in ("EXPORTS", "TESTEXPORTS") or section == "TESTEXPORTS":
            continue
        toks = re.findall(r'"[^"]*"|\S+', ln)
        if not toks or toks[0].startswith(":"):
            continue
        toks = [t.strip('"') for t in toks]
        src = os.path.normpath(os.path.join(cur_dir, toks[0].replace("\\", "/")))
        src = _mmp().nocase(src) or src       # the source was written for a case-insensitive file system
        dest = toks[1].replace("\\", "/") if len(toks) > 1 else ""
        if not dest:
            rel = os.path.basename(src)
        elif dest.startswith("+"):
            d = dest[1:].lstrip("/").lower()
            if not d.startswith("include/"):
                continue
            rel = dest[1:].lstrip("/")[len("include/"):]
        elif dest.startswith("/") or re.match(r"^[a-zA-Z]:", dest):
            d = re.sub(r"^[a-zA-Z]:", "", dest).lstrip("/")
            if not d.lower().startswith("epoc32/include/"):
                continue
            rel = d[len("epoc32/include/"):]
        elif dest.startswith("|"):
            # export INTO the source tree (S60: `../inc/x.h |../../../inc/x.h`). The source
            # stays pristine: mirror under epoc32/tree/<path rel. to the 2010 root>;
            tgt = os.path.normpath(os.path.join(os.path.dirname(bldinf), dest[1:]))
            if not tgt.lower().startswith(os.path.dirname(BASE).lower()):
                continue
            # (a sibling tree such as ../symbian-rcl3 mirrors under tree/_up/)
            rel = "../tree/" + os.path.relpath(tgt, BASE).replace(os.sep, "/").replace("..", "_up")
        else:
            rel = dest                       # relative dest => under epoc32/include
        if rel.endswith("/") or not rel:
            rel = rel + os.path.basename(src)
        yield src, rel


MANIFEST = os.path.join(EPOC32_ROOT, "export_manifest.tsv")


def _load_manifest():
    """exported header (relative to .epoc32/include) -> the baseline file it was copied from
    (relative to baseline/). kept next to the tree."""
    m = {}
    if os.path.isfile(MANIFEST):
        for line in open(MANIFEST, encoding="utf-8"):
            f = line.rstrip("\n").split("\t")
            if len(f) == 2:
                m[f[0]] = f[1]
    return m


def _save_manifest(m):
    with open(MANIFEST, "w", encoding="utf-8", newline="\n") as fh:
        for k in sorted(m):
            fh.write(k + "\t" + m[k] + "\n")


_INF_INCLUDE = re.compile(r'^\s*#\s*include\s*"([^"]+\.inf)"', re.I | re.M)


def included_infs(top):
    """The bld.infs under `top` that another bld.inf #includes. Those are exported through their includer only, with
    its macros: commsbufs/group/bld.inf takes version1/mbufmgr/group/BLD.INF only without
    SYMBIAN_ZERO_COPY_NETWORKING, and on its own that legacy copy replaced mbuf.h, es_mbuf.h ... (tcpip6 failed)."""
    seen = set()
    for root, dirs, files in os.walk(top):
        dirs[:] = [d for d in dirs if d != ".git"]
        for f in files:
            if f.lower() != "bld.inf":
                continue
            try:
                text = open(os.path.join(root, f), errors="replace").read()
            except OSError:
                continue
            for inc in _INF_INCLUDE.findall(text):
                p = os.path.normpath(os.path.join(root, inc.replace("\\", "/")))
                p = _mmp().nocase(p) or p
                seen.add(os.path.normcase(os.path.normpath(p)).lower())
    return seen


def run(pkgs, scan_only=False):
    n_inf = n_new = n_same = n_missing = 0
    replaced = []
    manifest = _load_manifest()
    baseline = os.path.dirname(BASE)
    for pkg in pkgs:
        nested = included_infs(os.path.join(BASE, pkg))
        for root, dirs, files in os.walk(os.path.join(BASE, pkg)):
            # sorted: when two bld.infs export the same header the later one wins, so the walk order must
            # not depend on the file system (NTFS lists alphabetically, ext4 does not: weakcrypto's hash.h
            # then replaced weakcryptospi's and cryptospi/hash stopped compiling on Linux)
            dirs[:] = sorted((d for d in dirs if d.lower() not in SKIP_DIRS and not d.lower().endswith(("_test", "_tests")) and d != ".git"), key=str.lower)
            for f in sorted(files, key=str.lower):
                if f.lower() != "bld.inf" or os.path.normcase(os.path.normpath(os.path.join(root, f))).lower() in nested:
                    continue
                n_inf += 1
                for src, rel in exports(os.path.join(root, f)):
                    if not os.path.isfile(src):
                        n_missing += 1
                        continue
                    dst = os.path.join(OUT, rel.replace("/", os.sep).lower())
                    mkey = os.path.relpath(dst, OUT).replace(os.sep, "/")
                    msrc = os.path.relpath(os.path.normpath(src), baseline).replace(os.sep, "/")
                    if os.path.exists(dst) and filecmp.cmp(src, dst, shallow=False):
                        manifest[mkey] = msrc      # byte-identical = a true origin of what is in place
                    if os.path.exists(dst) and os.path.getsize(dst) == os.path.getsize(src) \
                            and os.path.getmtime(dst) >= os.path.getmtime(src):
                        n_same += 1
                        continue
                    if scan_only:
                        continue
                    if os.path.exists(dst) and not filecmp.cmp(src, dst, shallow=False):
                        # a DIFFERENT header of the same name was in place (another package, or the
                        # other branch's copy): components already built against it may no longer
                        replaced.append((mkey, manifest.get(mkey, "?"), msrc))
                    os.makedirs(os.path.dirname(dst), exist_ok=True)
                    shutil.copy2(src, dst)
                    manifest[mkey] = msrc
                    n_new += 1
    _save_manifest(manifest)
    if scan_only:
        print("manifest: %d exported headers have a known origin (%d bld.inf scanned)" % (len(manifest), n_inf))
        return
    # PINNED: several packages export the same name; the Symbian (cedarutils)
    # platform_paths.hrh must win over the S60 one (which needs variant/ headers).
    shutil.copy2(os.path.join(PATHS_DIR, "platform_paths.hrh"),
                 os.path.join(OUT, "platform_paths.hrh"))
    # Orbit's Symbian-facing device-dialog headers: mw.hb is a qmake build (no bld.inf
    # PRJ_EXPORTS) that installs them as <hb/hbcore/x.h>; classicui's eiksrvui uses them.
    hb = os.path.join(BASE, "oss.FCL.sf.mw.hb", "src", "hbcore", "devicedialogbase")
    if os.path.isdir(hb):
        os.makedirs(os.path.join(OUT, "hb", "hbcore"), exist_ok=True)
        for f in os.listdir(hb):
            if f.lower().endswith("symbian.h") or f.lower() == "hbsymbianvariant.h":
                shutil.copy2(os.path.join(hb, f), os.path.join(OUT, "hb", "hbcore", f.lower()))
    # Our public headers: os/inc/** of core and every layer (OS_ROOT is their merged view), e.g. the browser engine
    # seam <phorepoc/phewebengine.h> from the webengine repo -- engine repos compile against the exported tree
    our_inc = os.path.join(OS_ROOT, "inc")
    for root, dirs, files in os.walk(our_inc, followlinks=True):
        for f in files:
            rel = os.path.relpath(os.path.join(root, f), our_inc)
            dst = os.path.join(OUT, rel.lower())
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            src = os.path.join(root, f)
            if not (os.path.exists(dst) and filecmp.cmp(src, dst, shallow=False)):   # unchanged keeps its mtime
                shutil.copyfile(src, dst)
    # RCL_3 ErrorResGt.rss includes <errors/generic/faxerr.ra>, but the fax component that
    # exported it is not in the 2010 release; the only copy is the telephony test tree's.
    fax = os.path.join(BASE, "oss.FCL.sf.os.networkingsrv", "networkingtestandutils",
                       "networkingunittest", "tdummyetel", "faxerr.ra")
    if os.path.isfile(fax):
        os.makedirs(os.path.join(OUT, "errors", "generic"), exist_ok=True)
        shutil.copy2(fax, os.path.join(OUT, "errors", "generic", "faxerr.ra"))
    # The S60 product variant (mw/app layers): bldvariant.hrh pulls the RD_* codeline
    # flags + KFeatureId* ids and expects the OS variant at variant/Symbian_OS.hrh.
    os.makedirs(os.path.join(OUT, "variant"), exist_ok=True)
    shutil.copy2(os.path.join(PATHS_DIR, "inc", "Symbian_OS_tb102sf.hrh"),
                 os.path.join(OUT, "variant", "Symbian_OS.hrh"))
    s60_path = os.path.join(OUT, "variant", "s60_variant.hrh")
    s60_old = open(s60_path).read() if os.path.isfile(s60_path) else None
    with open(s60_path + ".new", "w") as fh:
        fh.write("// OURS (generated by tools/epoc32_export.py): the S60 product variant\n"
                 "#ifndef S60_VARIANT_HRH\n#define S60_VARIANT_HRH\n"
                 "#include <bldvariant.hrh>\n"
                 "// App registration: the LEGACY scheme (apparc itself scans the *_reg.rsc files\n"
                 "// under private/10003a3f/apps) instead of the USIF Software Component Registry\n"
                 "// (an SQL-backed server). Both are in the 2010 source; legacy needs no SCR.\n"
                 "#undef SYMBIAN_UNIVERSAL_INSTALL_FRAMEWORK\n#endif\n")
    if open(s60_path + ".new").read() == s60_old:
        os.remove(s60_path + ".new")
    else:
        os.replace(s60_path + ".new", s60_path)
    print("bld.inf: %d   exported: %d new (%d of them REPLACED a different header), %d up-to-date, %d missing-src -> %s"
          % (n_inf, n_new, len(replaced), n_same, n_missing, OUT))
    for mkey, old, new in replaced[:40]:
        print("  replaced %s: %s -> %s" % (mkey, old, new))
    if len(replaced) > 40:
        print("  ... %d more" % (len(replaced) - 40))


def prebuilt() -> None:
    ref = os.path.join(BASE, "oss.API_REF.Public_API", "epoc32", "include")
    for h in PREBUILT:
        dst = os.path.join(OUT, h)
        if not os.path.exists(dst) and os.path.isfile(os.path.join(ref, h)):
            shutil.copyfile(os.path.join(ref, h), dst)
            print(f"prebuilt: {h}")


if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    if sys.argv[1:2] == ["--manifest-all"]:
        # reconstruct export_manifest.tsv for the include tree AS IT IS: scan every package on disk
        # (both lines), copy nothing; a header is attributed to an export whose bytes are identical
        base = os.path.dirname(BASE)
        run([os.path.join("..", t, d) for t in ("symbian-2010", "symbian-rcl3")
             for d in sorted(os.listdir(os.path.join(base, t)))], scan_only=True)
    else:
        if sys.argv[1:2] == ["--all"]:
            # Two passes around casefix: a bld.inf that includes a header under another case (bldvariant.hrh's
            # "ProductVariant.hrh", sf.mw.web's browser_platform_variant.hrh) stops cpp on a case-sensitive file
            # system and exports nothing until casefix has linked that spelling.
            import casefix
            pkgs = all_packages()
            run(pkgs)
            casefix.main(pkgs)
            run(pkgs)
            import cdl_headers                     # the CDL client headers no bld.inf exports (MifHeader ...)
            cdl_headers.main()
            prebuilt()
        else:
            run(sys.argv[1:] or DEFAULT_PKGS)
