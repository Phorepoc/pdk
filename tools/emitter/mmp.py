# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""mmp -- parse a Symbian .mmp project file the way the real build does.

The .mmp is run through the C preprocessor (platform_paths.hrh macros, the
product-variant .hrh feature flags, #include'd .mmh files, #ifdef WINS/EPOC32
arms), then the keyword statements are read. Only what our emitter needs:

    TARGET TARGETTYPE UID SOURCEPATH SOURCE USERINCLUDE SYSTEMINCLUDE LIBRARY
    STATICLIBRARY DEFFILE NOSTRICTDEF MACRO EPOCSTACKSIZE EPOCHEAPSIZE

START ... END blocks (RESOURCE / BITMAP / WINS / ARMCC) are skipped.
/epoc32/include paths map to BUILD/epoc32/include (tools/epoc32_export.py).
"""
from __future__ import annotations

import os
import re
import subprocess
from dataclasses import dataclass, field

from paths import SYMBIAN, BUILD, EPOC32_ROOT, OS_ROOT

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = os.path.join(SYMBIAN, "symbian-2010")
EPOC32 = EPOC32_ROOT
CEDAR = os.path.join(BASE, "oss.FCL.sf.os.deviceplatformrelease", "symbianosbld", "cedarutils")
VARIANT_HRH = os.path.join(CEDAR, "inc", "Symbian_OS_tb102sf.hrh")
CPP = "arm-none-eabi-cpp"
S60_VARIANT_HRH = os.path.join(EPOC32, "include", "variant", "s60_variant.hrh")


def variant_for(path: str) -> str:
    """mw/app-layer code builds under the S60 product variant (bldvariant.hrh: RD_*
    codeline flags, KFeatureId*); the OS layer under the plain Symbian OS variant."""
    # + system state management: the SSM (command lists, startup policy) lives in the OS-layer package but
    # names S60 features (KFeatureIdMediator, KFeatureIdOfflineMode ...), so it is preprocessed under the S60
    # variant too; so is HWRM's light target modifier (KFeatureIdCoverDisplay)
    p = path.replace("\\", "/")
    if (re.search(r"oss\.FCL\.sf\.(mw|app)\.", p) or "/sysstatemgmt/" in p or "/hwrmtargetmodifierplugin/" in p) \
            and os.path.isfile(S60_VARIANT_HRH):
        return S60_VARIANT_HRH
    return VARIANT_HRH


PLATFORM_DEFS = ["-DEPOC32", "-DMARM", "-DEABI", "-DGCCE", "-DMARM_ARMV5", "-DARMV5",
                 "-DSBSV2", "-D__GNUC__=3"]


@dataclass
class Mmp:
    path: str
    target: str = ""
    targettype: str = ""
    uids: list[int] = field(default_factory=list)
    sources: list[str] = field(default_factory=list)        # absolute
    userinc: list[str] = field(default_factory=list)        # absolute
    sysinc: list[str] = field(default_factory=list)         # absolute
    libs: list[str] = field(default_factory=list)           # lower, with .lib
    staticlibs: list[str] = field(default_factory=list)
    macros: list[str] = field(default_factory=list)
    deffile: str = ""
    linkas: str = ""
    nostrictdef: bool = False
    allowdlldata: bool = False                              # EPOCALLOWDLLDATA: the DLL may have writable statics
    stack: int = 0
    heap: tuple[int, int] = (0, 0)
    resources: list[str] = field(default_factory=list)      # absolute .rss paths
    # START RESOURCE blocks / RESOURCE lines: dict(rss, target, targetpath, header, uids)
    resource_blocks: list[dict] = field(default_factory=list)
    stringtables: list[str] = field(default_factory=list)   # absolute .st paths (START STRINGTABLE)
    targetpath: str = ""
    capability: list[str] = field(default_factory=list)     # CAPABILITY words, lower case ("all", "-tcb" ...)
    vendorid: int = 0                                       # VENDORID
    version: int = 0x000A0000                               # VERSION major.minor (the E32 module version)

    # TCapability order (e32capability.h): bit n = capability n
    CAPS = ("tcb", "commdd", "powermgmt", "multimediadd", "readdevicedata", "writedevicedata", "drm", "trustedui",
            "protserv", "diskadmin", "networkcontrol", "allfiles", "swevent", "networkservices", "localservices",
            "readuserdata", "writeuserdata", "location", "surroundingsdd", "userenvironment")

    def cap_bits(self) -> int:
        """The CAPABILITY statement as the E32Image's first capability word (all / none / -x / names)."""
        bits = 0
        for w in self.capability:
            if w == "all":
                bits = (1 << len(self.CAPS)) - 1
            elif w == "none":
                bits = 0
            elif w.startswith("-") and w[1:] in self.CAPS:
                bits &= ~(1 << self.CAPS.index(w[1:]))
            elif w in self.CAPS:
                bits |= 1 << self.CAPS.index(w)
        return bits

    @property
    def is_exe(self) -> bool:
        return self.targettype in ("exe", "exexp", "stdexe")

    def def_path(self) -> str | None:
        """Resolve the EABI .def per the abld rules (DEFFILE / default)."""
        hit = self._def_path(self.deffile)
        if hit is not None and not self.deffile and any(k in self.path.replace("\\", "/") for k in PACKAGE_MMP_DEFS):
            # variant-flag packages: the DEFFILE arm (ARMCC), when the mmp has one, decides, not the target-name fallback
            for ln in preprocess(self.path, ("-DARMCC",)).splitlines():
                t = ln.split()
                if len(t) > 1 and t[0].lower() == "deffile":
                    hit = self._def_path(t[1]) or hit
        if hit is None or re.search(r"(^|/)(wins|marm)\w*\.def$", hit.replace("\\", "/"), re.I):
            # `#if defined(ARMCC) deffile ../eabi/eabi.def #elif/#else <the emulator one>`: our cpp is the
            # GCCE line, so that arm is dropped. Read DEFFILE again as an ARMV5 (ARMCC) build would
            armcc = [ln.split()[1] for ln in preprocess(self.path, ("-DARMCC",)).splitlines()
                     if len(ln.split()) > 1 and ln.split()[0].lower() == "deffile"]
            if armcc and armcc[-1] != self.deffile:
                hit = self._def_path(armcc[-1]) or hit
        return hit

    def _def_path(self, deffile: str) -> str | None:
        mdir = os.path.dirname(self.path)
        stem = os.path.splitext(self.target)[0]
        cands = []
        if deffile:
            d = deffile.replace("\\", "/").replace("~", "eabi")
            # unconditional cpp takes the last DEFFILE arm (bwins/bmarm): we want EABI (and -DEABI turns a literal ../EABI/ into ../1/)
            d = re.sub(r"(^|/)(bwins|bwinscw|bmarm|1)(/|$)", r"\1eabi\3", d, flags=re.I)
            if d.endswith("/"):
                d += stem
            dn, bn = os.path.split(d)
            root, ext = os.path.splitext(bn)
            ext = ext or ".def"
            names = [root + ext] if self.nostrictdef else [root + "u" + ext, root + ext]
            dirs = [os.path.join(mdir, dn)] if dn else \
                   [os.path.join(mdir, "..", "eabi"), os.path.join(mdir, "..", "EABI"), mdir]
            if dn and dn.startswith("/"):
                dirs = [os.path.join(EPOC32, dn.lstrip("/").replace("epoc32/", "", 1))]
            cands = [os.path.join(x, n) for x in dirs for n in names]
        else:
            # no DEFFILE: the build names the .def after the target -- or after LINKAS when a
            # conditional TARGET differs from it (iaupdateapi_stub.dll LINKAS iaupdateapi.dll)
            stems = [stem] + ([os.path.splitext(self.linkas)[0]] if self.linkas else [])
            cands = [os.path.join(mdir, "..", "eabi", s + suf) for s in stems for suf in ("u.def", ".def")]
        for c in cands:
            hit = _isfile_nocase(c)
            if hit:
                return os.path.normpath(hit)
        return None


def _isfile_nocase(path: str) -> str | None:
    """The existing FILE `path` names when case is ignored (see nocase), or None."""
    hit = nocase(path)
    return hit if hit and os.path.isfile(hit) else None


def nocase(path: str) -> str | None:
    """The tree was authored on Windows (EABI/ vs eabi/, eabiU.def vs eabiu.def): on a case-sensitive host
    resolve each component of `path` case-insensitively. Returns the real file or directory, or None."""
    if os.path.exists(path):
        return path
    path = os.path.normpath(path)
    parts = path.replace("\\", "/").split("/")
    cur = "/" if path.startswith("/") else ""
    for p in parts:
        if not p:
            continue
        nxt = os.path.join(cur, p) if cur else p
        if not os.path.exists(nxt):
            try:
                hits = sorted(e for e in os.listdir(cur or ".") if e.lower() == p.lower())
            except OSError:
                return None
            if not hits:
                return None
            nxt = os.path.join(cur, hits[0]) if cur else hits[0]
        cur = nxt
    return cur if os.path.exists(cur) else None


def _num(tok: str) -> int:
    return int(tok, 16) if tok.lower().startswith("0x") else int(tok)


def _map_inc(p: str, mdir: str) -> str:
    r = _map_inc1(p, mdir)
    return (nocase(r) or r) if r else r


def _map_inc1(p: str, mdir: str) -> str:
    q = p.replace("\\", "/")
    if q.startswith("+"):
        q = "/epoc32" + q[1:]
    if re.match(r"^/?epoc32/", q, re.I):
        return os.path.normpath(os.path.join(EPOC32, re.sub(r"^/?epoc32/", "", q, flags=re.I)))
    if q.startswith("/"):
        # source-tree-rooted (/sf/mw/appinstall/...): the same sibling-package mapping as a relative path that
        # lands in sf/<layer>/<pkg> (the includer's own tree first); anything else resolves nothing
        if re.match(r"^/sf/", q):
            return map_sf_path(os.path.normpath(os.path.join(BASE, q.lstrip("/"))), mdir)
        return os.path.normpath(os.path.join(BASE, q.lstrip("/")))
    return map_sf_path(os.path.normpath(os.path.join(mdir, q)), mdir)


_SF_REL = re.compile(r"[\\/](os|mw|app|adapt|tools)[\\/]([^\\/]+)[\\/](.*)$")


def map_sf_path(path: str, mdir: str) -> str:
    """Nokia's tree is sf/<layer>/<package>/..., ours is <tree>/oss.FCL.sf.<layer>.<package>/...: a
    package reaching into a sibling by relative path (IMCM: ../../../../../../mw/messagingmw/...) lands
    outside the source tree. Map such a path onto the sibling checkout -- the includer's own tree first
    (an RCL_3 package pairs with its RCL_3 siblings), then the others.
    Our own mmps in OS_ROOT reach the source as <repo>/baseline/<tree>/... (the layout they were written in):
    that maps onto SYMBIAN, which a checkout of core keeps elsewhere."""
    rel = os.path.relpath(path, os.path.join(OS_ROOT, "baseline")).replace("\\", "/")
    if not rel.startswith("../"):
        path = os.path.normpath(os.path.join(SYMBIAN, rel))
    if nocase(path):
        return path
    m = _SF_REL.search(path)
    if not m:
        return path
    layer, pkg, rest = m.groups()
    root = SYMBIAN                                          # symbian-2010/, symbian-rcl3/
    rel = os.path.relpath(mdir, root).replace("\\", "/").split("/")[0]
    trees = [rel] + sorted(t for t in os.listdir(root) if t != rel and t.startswith('symbian-'))
    for t in trees:
        cand = os.path.join(root, t, f"oss.FCL.sf.{layer}.{pkg}", rest)
        # (or only its epoc32/tree twin: a package's `|` export dir is not in the source)
        twin = os.path.join(EPOC32, "tree", os.path.relpath(cand, BASE).replace("..", "_up").lower())
        if nocase(cand) or os.path.isdir(twin):
            return os.path.normpath(cand)
    return path


_SF_INCS: list = []


def sf_mirror_incs() -> list:
    """-I arguments that make relative includes written for Nokia's sf/<layer>/<pkg> tree resolve at ANY
    nesting depth (app.webuis bld.infs, and the help bld.inf they include, reach
    ../../../../../[../]mw/web/...). cpp retries a missing "relative/path" against each -I dir; a mirror
    root holds <layer>/<pkg> links to our checkouts (RCL_3 first) plus a chain d1/d2/.../d9, and -I
    <mirror>/d1/../dk for k = 1..9 lands every `k x ../` on the mirror root. The links are symlinks
    under BUILD/sf-mirror."""
    if _SF_INCS:
        return _SF_INCS
    root = os.path.join(BUILD, "sf-mirror")
    base = os.path.dirname(BASE)
    if not os.path.isdir(os.path.join(root, "d1")):
        chain = root
        for i in range(1, 10):
            chain = os.path.join(chain, f"d{i}")
        os.makedirs(chain, exist_ok=True)
        for tree in ("symbian-rcl3", "symbian-2010"):          # rcl3 first: an existing link wins
            tdir = os.path.join(base, tree)
            for pkg in (os.listdir(tdir) if os.path.isdir(tdir) else []):
                m = re.match(r"oss\.FCL\.sf\.(os|mw|app|adapt|tools)\.(.+)$", pkg)
                if not m:
                    continue
                link = os.path.join(root, m.group(1), m.group(2))
                if os.path.lexists(link):
                    continue
                os.makedirs(os.path.dirname(link), exist_ok=True)
                target = os.path.join(tdir, pkg)
                try:
                    os.symlink(target, link)
                except FileExistsError:
                    pass
    chain = root
    for i in range(1, 10):
        chain = os.path.join(chain, f"d{i}")
        _SF_INCS.extend(["-I", chain])
    return _SF_INCS


_MARKER = re.compile(r'^#\s+\d+\s+"(.*)"')
_PATH_KWS = ("userinclude", "systeminclude", "sourcepath")


def _path_statement_dirs(path: str, mdir: str, extra_defs=()) -> dict:
    """{keyword: [the directory of the file each USERINCLUDE / SYSTEMINCLUDE / SOURCEPATH came from, in
    order]} -- from cpp's line markers (makmake resolves a relative path in an #include'd .mmh
    against THAT file; WebKit's group/common.mmpi `USERINCLUDE ../../../WebCore` is right from group/, wrong
    from the including mmp two levels down). START ... END blocks are skipped, as parse() skips them."""
    out = {k: [] for k in _PATH_KWS}
    cur, skipping = mdir, False
    for raw in preprocess(path, extra_defs, markers=True).splitlines():
        mk = _MARKER.match(raw)
        if mk:
            f = mk.group(1).replace("\\\\", "\\")
            same = f.startswith("<") or f == "-" or \
                os.path.normcase(os.path.abspath(os.path.join(mdir, f))) == os.path.normcase(path)
            cur = mdir if same else os.path.dirname(os.path.abspath(os.path.join(mdir, f)))
            continue
        toks = raw.split()
        if not toks:
            continue
        kw = toks[0].lower()
        if kw == "start":
            skipping = True
        elif kw == "end":
            skipping = False
        elif kw in _PATH_KWS and not skipping:
            out[kw].append(cur)
    return out
_REL_INCLUDE = re.compile(r'^(\s*#\s*include\s*")([^"]+)(")', re.M)


def _package_unique(full: str, mdir: str) -> str | None:
    """A relative `#include "../../inc/x.h"` in an mmp whose target the release does not carry at that
    path (ccaapp.mmp: ccauids.h lives only in ccapp/ccaclient/inc): use the ONE file of that name in the
    same package, if there is exactly one. No match or several -> None (cpp reports it)."""
    root = SYMBIAN
    rel = os.path.relpath(mdir, root).replace("\\", "/").split("/")
    if len(rel) < 2:
        return None
    pkg = os.path.join(root, rel[0], rel[1])
    name = os.path.basename(full).lower()
    hits = []
    for dp, dns, fns in os.walk(pkg):
        dns[:] = [x for x in dns if x.lower() not in ("test", "tsrc", "internal", ".git")]
        hits += [os.path.join(dp, f) for f in fns if f.lower() == name]
        if len(hits) > 1:
            return None
    return os.path.normpath(hits[0]) if hits else None


_CACHE_DIR = os.path.join(BUILD, "mmpcache")
_CACHE_OFF = os.environ.get("MMP_NOCACHE") == "1"


def _stamp(f: str):
    try:
        st = os.stat(f)
        return [st.st_mtime_ns, st.st_size]
    except OSError:
        return None


def preprocess(path: str, extra_defs=(), markers: bool = False) -> str:
    """cpp over an mmp, cached on disk (a ROM build parses ~920 mmps twice; 1840 cpp processes cost ~5
    min uncached). An entry is keyed by the mmp, the defines, the marker mode and this module's own source,
    and is valid while every file cpp read (from the marker run's line markers) keeps its mtime + size.
    MMP_NOCACHE=1 bypasses it."""
    for _pk, _d in PACKAGE_MMP_DEFS.items():
        if _pk in path.replace("\\", "/") and not set(_d) <= set(extra_defs):
            extra_defs = tuple(extra_defs) + _d
    if _CACHE_OFF:
        return _preprocess(path, extra_defs, markers)
    import hashlib
    import json
    key = hashlib.sha1(json.dumps([os.path.abspath(path), list(extra_defs), markers,
                                   _stamp(os.path.abspath(__file__)), CPP]).encode()).hexdigest()
    cfile = os.path.join(_CACHE_DIR, key[:2], key + ".json")
    try:
        with open(cfile, encoding="utf-8") as f:
            ent = json.load(f)
        if all(_stamp(d) == s for d, s in ent["deps"]):
            return ent["out"]
    except (OSError, ValueError, KeyError, TypeError):
        pass
    out = _preprocess(path, extra_defs, markers)
    mk = out if markers else preprocess(path, extra_defs, True)     # parse() runs the marker pass anyway
    mdir = os.path.dirname(os.path.abspath(path))
    deps = {os.path.abspath(path)}
    for raw in mk.splitlines():
        m = _MARKER.match(raw)
        if m and not m.group(1).startswith("<") and m.group(1) != "-":
            deps.add(os.path.normpath(os.path.join(mdir, m.group(1).replace("\\\\", "\\"))))
    try:
        os.makedirs(os.path.dirname(cfile), exist_ok=True)
        tmp = cfile + f".{os.getpid()}.tmp"
        with open(tmp, "w", encoding="utf-8", newline="\n") as f:
            json.dump({"path": path, "out": out, "deps": [[d, _stamp(d)] for d in sorted(deps)]}, f)
        os.replace(tmp, cfile)
    except OSError:
        pass
    return out


def _preprocess(path: str, extra_defs=(), markers: bool = False) -> str:
    # `#include "../../../../../../mw/messagingmw/.../msgopt.mph"` (sf/ layout, see map_sf_path): feed cpp a
    # copy with such includes made absolute, from stdin in the mmp's own dir -- so every other
    # relative include resolves exactly as it did
    mdir = os.path.dirname(path)
    text = open(path, errors="replace").read()

    def fix(m):
        inc = m.group(2).replace("\\", "/")
        full = os.path.normpath(os.path.join(mdir, inc))
        mapped = map_sf_path(full, mdir) if not os.path.exists(full) else full
        if mapped == full and not os.path.exists(full) and "/" in inc:
            mapped = _package_unique(full, mdir) or full
        return m.group(1) + mapped.replace("\\", "/") + m.group(3) if mapped != full else m.group(0)
    fixed = _REL_INCLUDE.sub(fix, text)
    # `#include <vg\openvguids.hrh>` (sfopenvg): a Windows separator in a system include
    fixed = re.sub(r'^(\s*#\s*include\s*<)([^>]*\\[^>]*)(>)', lambda m: m.group(1) + m.group(2).replace("\\", "/") + m.group(3),
                   fixed, flags=re.M)
    src, stdin = (path, None) if fixed == text else ("-", fixed)
    r = subprocess.run([CPP, "-undef", "-nostdinc", "-x", "c", "-w", *([] if markers else ["-P"]), *PLATFORM_DEFS,
                        *extra_defs, "-I", CEDAR, "-I", os.path.join(EPOC32, "include"),
                        "-I", mdir, "-include", variant_for(path),
                        *sf_mirror_incs(), "-idirafter", os.path.join(BUILD, "casefix"),
                        "-include", os.path.join(CEDAR, "platform_paths.hrh"),
                        # CAP_SERVER, CAP_GENERAL_DLL ... (CAPABILITY CAP_SERVER NetworkControl): abld gets them from the platform
                        # variant; without them a CAPABILITY line dropped the macro and the image lost those capabilities
                        *(["-include", os.path.join(EPOC32, "include", "defaultcaps.hrh")]
                          if os.path.exists(os.path.join(EPOC32, "include", "defaultcaps.hrh")) else []), src],
                       input=stdin, cwd=mdir if stdin is not None else None,
                       capture_output=True, text=True, errors="replace")
    if r.returncode != 0 and not r.stdout:
        raise RuntimeError(f"cpp failed on {path}:\n{r.stderr[:2000]}")
    return r.stdout


# packages whose mmps are written for a product variant that has a flag ours lacks (the S^3 platform variant defines
# RD_MULTIPLE_DRIVE; Voice Recorder picks its DEFFILE and its VRUtils API by it). The same flag is a compile define via
# components `defs`; this makes the mmp's own #if/DEFFILE choice agree with it.
PACKAGE_MMP_DEFS = {"/oss.FCL.sf.app.voicerec/": ("-DRD_MULTIPLE_DRIVE",)}


def parse(path: str, extra_defs=()) -> Mmp:
    path = os.path.abspath(path)
    mdir = os.path.dirname(path)
    m = Mmp(path=path)
    srcpath = mdir
    skipping = False
    block = None
    # relative paths in an #include'd .mmh/.mmpi are relative to THAT file, as makmake resolves them
    # (WebKit's group/common.mmpi: `USERINCLUDE ../../../WebCore` is right from group/, wrong from the
    # including mmp two levels down). cpp's line markers say which file each statement came from.
    # The statements come from the -P output (it joins backslash-continued lines -- libc's stdio.mmpi SOURCE
    # lists); a second, marker run records which file each path statement came from, in order.
    ctx = _path_statement_dirs(path, mdir, extra_defs)
    seen_ctx = {}
    cur = mdir
    for raw in preprocess(path, extra_defs).splitlines():
        toks = raw.split()
        if not toks:
            continue
        kw, args = toks[0].lower(), toks[1:]
        if kw in _PATH_KWS and not skipping:
            i = seen_ctx.get(kw, 0)
            seen_ctx[kw] = i + 1
            cur = ctx[kw][i] if i < len(ctx.get(kw, [])) else mdir
        if skipping:
            if kw == "end":
                skipping = False
                block = None
            elif block is not None:
                if kw == "target" and args:
                    block["target"] = args[0]
                elif kw == "targetpath" and args:
                    block["targetpath"] = args[0]
                elif kw == "header":
                    block["header"] = True
                elif kw == "uid":
                    block["uids"] = [_num(a) for a in args]
            continue
        if kw == "start":
            skipping = True
            block = None
            if len(args) >= 2 and args[0].lower() == "resource":
                rss = os.path.join(srcpath, args[1].replace("\\", "/"))
                m.resources.append(rss)
                block = dict(rss=rss, target="", targetpath="", header=False, uids=[])
                m.resource_blocks.append(block)
            elif len(args) >= 2 and args[0].lower() == "stringtable":
                m.stringtables.append(os.path.join(srcpath, args[1].replace("\\", "/")))
        elif kw in ("resource", "systemresource"):
            for a in args:
                rss = os.path.join(srcpath, a.replace("\\", "/"))
                m.resources.append(rss)
                m.resource_blocks.append(dict(rss=rss, target="", header=True, uids=[],
                                              targetpath=m.targetpath or
                                              ("/system/data" if kw == "systemresource" else "")))
        elif kw == "targetpath" and args:
            m.targetpath = args[0]
        elif kw == "target" and args:
            m.target = args[0].lower()
        elif kw == "targettype" and args:
            m.targettype = args[0].lower()
        elif kw == "uid":
            m.uids = [_num(a) for a in args]
        elif kw == "sourcepath" and args:
            srcpath = _map_inc(args[0], cur)
        elif kw == "source":
            # `SOURCE /freetype2/src/base/ftinit.c` (FREETYPE.MMP): makmake takes a leading slash as relative to
            # the SOURCEPATH, not the filesystem root -- os.path.join would drop srcpath
            m.sources += [(nocase(p) or p) for p in
                          (os.path.join(srcpath, a.replace("\\", "/").lstrip("/")) for a in args)]
        elif kw == "userinclude":
            m.userinc += [_map_inc(a, cur) for a in args]
        elif kw == "systeminclude":
            m.sysinc += [_map_inc(a, cur) for a in args]
        elif kw == "library":
            # a library named twice (srchuiresviewplugin: charconv.lib) is ONE import block, as the
            # build tools make it -- two blocks for one DLL garbled the second's ordinals
            m.libs += [a.lower() for a in args if a.lower() not in m.libs]
        elif kw == "staticlibrary":
            m.staticlibs += [a.lower() for a in args]
        elif kw == "macro":
            # `MACRO "STACK_SCO_DATA"` (os.bt btcommon/config.cfg) is legal: the build strips the quotes.
            # `MACRO HB_BUILD_DIR="\"/path\""` (qmake-generated) defines a C STRING: keep its inner quotes
            for a in args:
                name, eq, val = a.partition("=")
                if eq and len(val) >= 4 and val.startswith('"\\"') and val.endswith('\\""'):
                    m.macros.append(f'{name}="{val[3:-3]}"')
                elif eq and len(val) >= 4 and val.startswith('""') and val.endswith('""'):
                    # Qt 4.8's qmake writes the same C string as `HB_LIB_DIR=""/path""` 
                    m.macros.append(f'{name}="{val[2:-2]}"')
                else:
                    m.macros.append(a.strip('"'))
        elif kw == "deffile" and args:
            m.deffile = args[0]
        elif kw == "linkas" and args:
            m.linkas = args[0]
        elif kw == "nostrictdef":
            m.nostrictdef = True
        elif kw == "epocallowdlldata":
            m.allowdlldata = True
        elif kw == "epocstacksize" and args:
            m.stack = _num(args[0])
        elif kw == "epocheapsize" and len(args) >= 2:
            m.heap = (_num(args[0]), _num(args[1]))
        elif kw == "capability":
            m.capability += [a.lower() for a in args]
        elif kw == "vendorid" and args:
            # VID_DEFAULT: defaultcaps.hrh's macro (Nokia's vendor ID), used unexpanded by some mmps
            try:
                m.vendorid = 0x101FB657 if args[0] == "VID_DEFAULT" else _num(args[0])
            except ValueError:
                pass
        elif kw == "version" and args:
            try:
                major, _, minor = args[0].partition(".")
                m.version = (int(major) << 16) | int(minor or 0)
            except ValueError:
                pass
    # mw/app layer: the S60 platform_paths MW_LAYER_SYSTEMINCLUDE also reaches the
    # OS-layer xml platform headers (our pinned Symbian platform_paths.hrh does not)
    if variant_for(path) == S60_VARIANT_HRH:
        for extra in ("platform/xml", "xml"):
            d = os.path.join(EPOC32, "include", *extra.split("/"))
            if d not in m.sysinc:
                m.sysinc.append(d)
    # in-tree exports (`|` destinations) live in the .epoc32/tree mirror (lower case, as epoc32_export writes it)
    for lst in (m.userinc, m.sysinc):
        for d in list(lst):
            # (a sibling tree such as symbian-rcl3 mirrors under tree/_up/)
            if d.lower().startswith(os.path.dirname(BASE).lower()):
                twin = os.path.join(EPOC32, "tree", os.path.relpath(d, BASE).replace("..", "_up").lower())
                if os.path.isdir(twin):
                    lst.insert(lst.index(d) + 1, twin)
    return m


if __name__ == "__main__":
    import sys
    mm = parse(sys.argv[1])
    print(mm)
    print("def:", mm.def_path())
