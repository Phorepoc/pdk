# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only

"""objcache -- is this object up to date? One rule for both builders (build_mmp.py: ARM, build_mmp_x32.py: x32).

An object is reused when everything its compilation read is what it was when the object was made, judged by CONTENT,
never by time:

  key     the compiler (hash of its cc1plus / cc1 and version line) and the command line, with the places this build
          happens to keep things in written as what they are (see `norm`)
  digest  the content of every file the compiler opened (the .d file g++ -MMD wrote: the source and each header),
          by real path

Both are stored next to the object (<obj>.key) when the compile succeeded and compared before the next one. A
modification time proves nothing here: a fresh checkout dates every file now, a restored build tree dates every
object when it was unpacked, and an export that rewrites a header with the same bytes would rebuild everything that
includes it. So a build tree made by one run (the published core layer, an earlier release) is reused by the next
whatever the clock says, and an object is never reused when a byte it was made from has changed.

What the rule does not see is what make does not see either: a NEW file that would now be found earlier on the include
path than the header the object was made from. The include directories under the OS tree are merged from several
repositories (paths.py `osview`), so the key lists the files a layer other than core adds to them.

Environment: OBJCACHE_EXPLAIN=1 stores the per-file list in the key file as well, and a rebuild then names the first
file that changed (otherwise only the kind of change is known).
"""
from __future__ import annotations

import hashlib
import os
import re
import subprocess
import threading
import time

_LOCK = threading.Lock()
_FILE_HASH: dict = {}       # path -> ((mtime_ns, size), sha1 of its content)
_REAL: dict = {}            # path -> realpath
_COMPILER: dict = {}        # compiler command -> id
_ADDED: dict = {}           # include dir -> text listing the files a layer other than core's adds to it
EXPLAIN = os.environ.get("OBJCACHE_EXPLAIN") == "1"

# how a build names things (build.go dockerArgs): core's os/ is /os, an adaptation i is /adapt/i with its own os/, and
# with adaptations the OS tree is the merged view BUILD/osview. All of them are "the OS tree": /os in the key.
_ADAPT_NAMES = [n for n in os.environ.get("PHOREPOC_ADAPTATION_NAMES", "").split(":") if n]
_ADAPT_OS = re.compile(r"/adapt/\d+/os(?=[/=]|$)")
_ADAPT = re.compile(r"/adapt/(\d+)(?=[/=]|$)")


def _build() -> str:
    return (os.environ.get("PHOREPOC_BUILD") or "/build").rstrip("/")


def norm(s: str) -> str:
    """A path written the way this build keeps it -> the way every build names it."""
    s = _ADAPT_OS.sub("/os", s)
    s = s.replace(_build() + "/osview", "/os")
    return _ADAPT.sub(lambda m: "/@" + (_ADAPT_NAMES[int(m.group(1))] if int(m.group(1)) < len(_ADAPT_NAMES) else m.group(1)), s)


def compiler_id(cmd0: str) -> str:
    """The compiler's own bytes: the driver's version line and the compiler proper (cc1plus, cc1). A new toolchain
    image changes the key; the same one never does."""
    with _LOCK:
        if cmd0 in _COMPILER:
            return _COMPILER[cmd0]
    h = hashlib.sha1()
    try:
        h.update(subprocess.run([cmd0, "--version"], capture_output=True, check=True).stdout)
        for prog in ("cc1plus", "cc1"):
            p = subprocess.run([cmd0, "-print-prog-name=" + prog], capture_output=True, check=True).stdout.decode().strip()
            if os.path.isabs(p) and os.path.isfile(p):
                h.update(file_hash(p).encode())
    except (OSError, subprocess.CalledProcessError):
        h.update(b"unknown compiler " + cmd0.encode())
    with _LOCK:
        _COMPILER[cmd0] = h.hexdigest()
    return _COMPILER[cmd0]


def file_hash(path: str) -> str:
    """sha1 of the content. Remembered for the life of the process by (mtime, size), so a header the build itself
    rewrites (a generated one) is read again; a file written in the last two seconds is never taken from the memory
    (a second write could leave both unchanged)."""
    try:
        st = os.stat(path)
    except OSError:
        return "-"
    stamp = (st.st_mtime_ns, st.st_size)
    hit = _FILE_HASH.get(path)
    if hit and hit[0] == stamp:
        return hit[1]
    try:
        with open(path, "rb") as fh:
            h = hashlib.sha1(fh.read()).hexdigest()
    except OSError:
        return "-"
    if time.time() - st.st_mtime > 2:
        _FILE_HASH[path] = (stamp, h)
    return h


def _realpath(path: str) -> str:
    r = _REAL.get(path)
    if r is None:
        r = _REAL[path] = norm(os.path.realpath(path))
    return r


def _added_files(dirs: list) -> str:
    """Files in these directories that a layer other than core's brought to the merged OS tree (symlinks into
    /adapt/<i>/os): a new header there could shadow one an object was made from."""
    out = []
    for d in dirs:
        d = os.path.normpath(d)
        if not (norm(d) + "/").startswith("/os/"):        # (only the merged OS tree has files of several layers)
            continue
        if d not in _ADDED:
            added = []
            if _ADAPT_NAMES and os.path.isdir(d):
                for root, _dirs, names in os.walk(d):
                    for n in names:
                        p = os.path.join(root, n)
                        if os.path.islink(p) and _ADAPT_OS.search(os.path.realpath(p)):
                            added.append(norm(p))
            _ADDED[d] = "\n".join(sorted(added))
        if _ADDED[d]:
            out.append(_ADDED[d])
    return "\n".join(out)


def make_key(compiler: str, cmd: list, extra: str = "") -> str:
    """cmd = the compile command line without its output paths. The mount points and the prefix maps (which map the
    checkout to the same `os/`, `build/` whatever it is) are written as what they mean."""
    maps, parts, incdirs = set(), [], []
    for i, a in enumerate(cmd):
        if a.startswith("-fmacro-prefix-map="):
            maps.add(a.rsplit("=", 1)[1])
            continue
        parts.append(norm(a))
        d = a[2:] if a.startswith("-I") and len(a) > 2 else (cmd[i + 1] if a in ("-I", "-iquote") and i + 1 < len(cmd) else
                                                              (a[7:] if a.startswith("-iquote") and len(a) > 7 else None))
        if d and os.path.isabs(d):
            incdirs.append(d)
    text = "\0".join(["objcache1", compiler_id(compiler), "maps:" + ",".join(sorted(maps)), *parts, extra,
                      _added_files(sorted(set(incdirs)))])
    return hashlib.sha1(text.encode("utf-8", "replace")).hexdigest()


def read_deps(obj: str):
    """The files <obj>.d lists (make syntax: backslash-newline continuations, backslash-space = a space in a path),
    None if there is no depfile."""
    try:
        text = open(obj + ".d", errors="replace").read()
    except OSError:
        return None
    text = text.replace("\\\r\n", " ").replace("\\\n", " ")
    body = text.split(": ", 1)[1] if ": " in text else ""
    return [d.strip().replace("\t", " ") for d in body.replace("\\ ", "\t").split(" ") if d.strip()]


def pin_depfile(obj: str) -> None:
    """Rewrite the depfile with real paths for what lives in /tmp: the case shim (case_shim.py) is made per container
    and links the headers that Windows spelled differently, so the next container would find those paths gone."""
    deps = read_deps(obj)
    if not deps or not any(d.startswith("/tmp/") for d in deps):
        return
    out = [os.path.realpath(d) if d.startswith("/tmp/") else d for d in deps]
    with open(obj + ".d", "w") as fh:
        fh.write(os.path.basename(obj) + ": " + " \\\n ".join(d.replace(" ", "\\ ") for d in out) + "\n")


def _entries(obj: str):
    deps = read_deps(obj)
    if deps is None:
        return None
    return sorted({(_realpath(d), file_hash(d)) for d in deps})


def _digest(entries) -> str:
    return hashlib.sha1("\n".join(f"{h} {p}" for p, h in entries).encode("utf-8", "replace")).hexdigest()


def record(obj: str, key: str) -> None:
    """After a successful compile."""
    pin_depfile(obj)
    ents = _entries(obj)
    if ents is None or any(h == "-" for _p, h in ents):
        try:
            os.remove(obj + ".key")             # (a file the compiler read cannot be read now: the object is never reused)
        except OSError:
            pass
        return
    with open(obj + ".key", "w") as fh:
        fh.write(key + "\n" + _digest(ents) + "\n")
        if EXPLAIN:
            fh.write("".join(f"{h} {p}\n" for p, h in ents))


def check(obj: str, key: str) -> str:
    """"" = up to date; else why not: no object | no key | compiler or command changed | a file it was made from changed
    (the first one, with OBJCACHE_EXPLAIN) | no depfile."""
    if not os.path.exists(obj):
        return "no object"
    try:
        lines = open(obj + ".key").read().split("\n")
    except OSError:
        return "no key"
    if len(lines) < 2 or lines[0] != key:
        return "compiler or command changed"
    ents = _entries(obj)
    if ents is None:
        return "no depfile"
    if any(h == "-" for _p, h in ents):
        return "a file it read is missing"      # (nothing to compare: never a hit)
    if lines[1] == _digest(ents):
        return ""
    old = dict(l.split(" ", 1)[::-1] for l in lines[2:] if " " in l)
    for p, h in ents:
        if p in old and old[p] != h:
            return "changed: " + p
    return "a file it was made from changed"


_STATS: dict = {}
_REGISTERED = []


def count(why: str) -> None:
    """Tally one object (why = check()'s answer; "" = reused) and print the tally when the process ends."""
    kind = why.split(":", 1)[0] if why else "up to date"
    with _LOCK:
        _STATS[kind] = _STATS.get(kind, 0) + 1
        if not _REGISTERED:
            import atexit
            _REGISTERED.append(1)
            atexit.register(_summary)
    if why and EXPLAIN:
        print(f"  rebuild {why}", flush=True)


def _summary() -> None:
    if _STATS:
        total = sum(_STATS.values())
        print("objcache: %d objects: %s" % (total, ", ".join(f"{n} {k}" for k, n in sorted(_STATS.items(), key=lambda kv: -kv[1]))),
              flush=True)
