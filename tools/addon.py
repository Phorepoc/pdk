# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

r"""addon -- an add-on archive: built components that are added to an image that was built without them.

    addon.py pack <component>... --arch arm|x32 [--name NAME] [--out NAME-ARCH-addon.tar.gz]
    addon.py info <archive>
    addon.py rom-args --arch arm|x32 --into DIR <archive>...      (the ROM recipe calls this: -O addon=<archive>,...)

pack takes the files a component carries in an image -- the built binary, its registration resource, the registry's
`rom_data` lines and the resource step's manifest, the same set tools/make_sis.py packages (make_sis.component_files) --
and lays them out in the archive by their image path:

    ADDON.txt                       the manifest (below)
    files/sys/bin/phestore.exe      <- Z:\sys\bin\phestore.exe
    files/resource/apps/...         <- Z:\resource\apps\...
    files/private/10003a3f/apps/... <- Z:\private\10003a3f\apps\...

ADDON.txt (text; the archive's members are sorted with fixed owner and times, so equal inputs give equal archives):

    format=1
    name=catalogue
    arch=arm                        the ABI the binaries were built for
    pdk=<commit>                    what they were built against (git HEAD of $PHOREPOC_PDK, $PHOREPOC_CORE,
    core=<commit>                   and each repository of $PHOREPOC_ADAPTATIONS as <repository>=<commit>)
    [files]
    sys/bin/phestore.exe=files/sys/bin/phestore.exe        <image path with / separators>=<archive path>

rom-args unpacks the archives under DIR/<name>/ and prints rombuild --data arguments (one per line, `--data` then
`<Z: path>=<host path>`), after refusing an archive of another architecture, with an unsafe path, or with a file listed
but missing. An archive built against another core commit than the image's is accepted with a warning: the binaries link
by ordinal against the image's DLLs (docs/phe: image-options, "addon").
"""
import argparse
import gzip
import hashlib
import io
import os
import subprocess
import sys
import tarfile

HERE = os.path.dirname(os.path.abspath(__file__))
FORMAT = "1"
MANIFEST = "ADDON.txt"


def commit(path):
    """git HEAD of a checkout, or 'unknown'."""
    try:
        return subprocess.run(["git", "-C", path, "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return "unknown"


def built_against():
    """[(repository, commit)] of what this build is made of: pdk, core, then each adaptation/layer."""
    out = []
    pdk = os.environ.get("PHOREPOC_PDK") or os.path.dirname(HERE)
    out.append(("pdk", commit(pdk)))
    if os.environ.get("PHOREPOC_CORE"):
        out.append(("core", commit(os.environ["PHOREPOC_CORE"])))
    for a in os.environ.get("PHOREPOC_ADAPTATIONS", "").split(os.pathsep):
        if a:
            out.append((os.path.basename(os.path.normpath(a)), commit(a)))
    return out


def image_path(win):
    """A Windows image path (sys\\bin\\x.exe) as the archive spells it: lower case, / separators."""
    return win.replace("\\", "/").lstrip("/").lower()


def check_member(path):
    parts = path.split("/")
    if not path or path.startswith("/") or ":" in path or "\\" in path or any(p in ("", ".", "..") for p in parts):
        sys.exit(f"addon: unsafe path {path!r}")


def parse_manifest(text):
    head, files, in_files = {}, [], False
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line == "[files]":
            in_files = True
        elif in_files:
            img, sep, arc = line.partition("=")
            if not sep:
                sys.exit(f"addon: manifest line is not <image path>=<archive path>: {line!r}")
            files.append((img, arc))
        else:
            k, _, v = line.partition("=")
            head[k] = v
    if head.get("format") != FORMAT:
        sys.exit(f"addon: manifest format {head.get('format')!r}, this tool reads {FORMAT!r}")
    for k in ("name", "arch"):
        if not head.get(k):
            sys.exit(f"addon: manifest has no {k}")
    return head, files


def read(archive):
    """(manifest head, [(image path, archive path)], tarfile) of an add-on archive."""
    tf = tarfile.open(archive, "r:*")
    try:
        text = tf.extractfile(MANIFEST).read().decode("utf-8")
    except (KeyError, AttributeError):
        sys.exit(f"addon: {archive} has no {MANIFEST}: not an add-on archive")
    head, files = parse_manifest(text)
    members = {m.name: m for m in tf.getmembers()}
    for img, arc in files:
        check_member(img)
        check_member(arc)
        if arc not in members or not members[arc].isfile():
            sys.exit(f"addon: {archive}: {arc} is listed but not in the archive")
    return head, files, tf


def add(tf, name, data):
    ti = tarfile.TarInfo(name)
    ti.size, ti.mode, ti.mtime = len(data), 0o644, 0
    tf.addfile(ti, io.BytesIO(data))


def pack(a):
    a.name = a.name or a.keys[0]
    a.out = a.out or os.path.join(os.environ.get("PHOREPOC_BUILD", "build"), "addon", f"{a.name}-{a.arch}-addon.tar.gz")
    sys.path.insert(0, HERE)
    import make_sis
    files, _, _ = make_sis.component_files(a.keys, a.arch == "arm", sis=False)
    entries = {}
    for data, target, _caps in files:
        img = image_path(target)
        check_member(img)
        if img in entries and entries[img] != data:
            sys.exit(f"addon: {img} comes from two components with different content")
        entries[img] = data
    write_archive(a.out, a.name, a.arch, entries)


def write_archive(out, name, arch, entries):
    """entries: {image path (lower case, / separators): bytes}."""
    lines = ["format=" + FORMAT, "name=" + name, "arch=" + arch]
    for repo, c in built_against():
        lines.append(f"{repo}={c}")
    lines.append("[files]")
    lines += [f"{img}=files/{img}" for img in sorted(entries)]
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    with open(out, "wb") as f, gzip.GzipFile(filename="", mode="wb", fileobj=f, mtime=0) as gz, tarfile.open(fileobj=gz, mode="w", format=tarfile.PAX_FORMAT) as tf:
        add(tf, MANIFEST, ("\n".join(lines) + "\n").encode())
        for img in sorted(entries):
            add(tf, "files/" + img, entries[img])
    print(f"{out}: {len(entries)} files, arch {arch}, sha256 {hashlib.sha256(open(out, 'rb').read()).hexdigest()[:16]}")
    for img in sorted(entries):
        print("   ", img)


def info(a):
    head, files, _ = read(a.archive)
    for k, v in head.items():
        print(f"{k}={v}")
    for img, arc in files:
        print(f"  {img}")


def rom_args(a):
    core_now = commit(os.environ["PHOREPOC_CORE"]) if os.environ.get("PHOREPOC_CORE") else None
    for archive in a.archives:
        head, files, tf = read(archive)
        if head["arch"] != a.arch:
            sys.exit(f"addon: {archive} is built for {head['arch']}, this image is {a.arch}")
        if core_now and head.get("core") not in (None, "unknown", core_now):
            print(f"addon: warning: {archive} was built against core {head['core'][:12]}, this image has {core_now[:12]}", file=sys.stderr)
        dest = os.path.join(a.into, head["name"])
        for img, arc in files:
            host = os.path.join(dest, *arc.split("/"))
            os.makedirs(os.path.dirname(host), exist_ok=True)
            with open(host, "wb") as f:
                f.write(tf.extractfile(arc).read())
            print("--data")
            print(img.replace("/", "\\") + "=" + host)
        print(f"addon: {head['name']} ({head['arch']}): {len(files)} files", file=sys.stderr)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("pack")
    p.add_argument("keys", nargs="+", help="components (the registry's keys)")
    p.add_argument("--arch", required=True, choices=["arm", "x32"])
    p.add_argument("--name", help="the add-on's name (default: the first component)")
    p.add_argument("--out", help="the archive (default: $PHOREPOC_BUILD/addon/<name>-<arch>-addon.tar.gz)")
    p.set_defaults(fn=pack)
    p = sub.add_parser("info")
    p.add_argument("archive")
    p.set_defaults(fn=info)
    p = sub.add_parser("rom-args")
    p.add_argument("--arch", required=True, choices=["arm", "x32"])
    p.add_argument("--into", required=True)
    p.add_argument("archives", nargs="+")
    p.set_defaults(fn=rom_args)
    a = ap.parse_args()
    a.fn(a)
    return 0


if __name__ == "__main__":
    sys.exit(main())
