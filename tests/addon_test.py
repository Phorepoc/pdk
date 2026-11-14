# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# Checks the add-on archive (tools/addon.py): the layout and manifest of an archive, that equal inputs give equal bytes, and
# that the ROM recipe's consumer (rom-args) unpacks it to rombuild --data arguments and refuses an archive of another
# architecture, an unsafe path or a listed file that is missing.
# Run: python3 pdk/tests/addon_test.py
import importlib.util
import io
import os
import subprocess
import sys
import tarfile
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
TOOL = os.path.join(HERE, "..", "tools", "addon.py")
spec = importlib.util.spec_from_file_location("addon", TOOL)
ad = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ad)

tmp = tempfile.mkdtemp()
os.environ.pop("PHOREPOC_CORE", None)
os.environ["PHOREPOC_ADAPTATIONS"] = ""

files = {"sys/bin/demo.exe": b"EXE" * 100, "resource/apps/demo_icon.mbm": b"MBM", "private/10003a3f/apps/demo_reg.rsc": b"RSC"}
a1, a2 = os.path.join(tmp, "a1.tar.gz"), os.path.join(tmp, "a2.tar.gz")
ad.write_archive(a1, "demo", "arm", files)
ad.write_archive(a2, "demo", "arm", dict(reversed(list(files.items()))))
assert open(a1, "rb").read() == open(a2, "rb").read(), "the archive is not reproducible"

head, listed, tf = ad.read(a1)
assert head["name"] == "demo" and head["arch"] == "arm" and head["format"] == "1" and "pdk" in head
assert [i for i, _ in listed] == sorted(files)
assert all(arc == "files/" + img for img, arc in listed)

# provenance in the toolchain container: the layers are /adapt/<n> mounts named by PHOREPOC_ADAPTATION_NAMES, and a
# checkout owned by another user still gives its commit (the first CI archives said "0=unknown", "pdk=unknown")
layer = os.path.join(tmp, "layer")
os.makedirs(layer)
subprocess.run(["git", "init", "-q", layer], check=True)
subprocess.run(["git", "-C", layer, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"], check=True)
want = subprocess.run(["git", "-C", layer, "rev-parse", "HEAD"], capture_output=True, text=True, check=True).stdout.strip()
os.environ["PHOREPOC_ADAPTATIONS"], os.environ["PHOREPOC_ADAPTATION_NAMES"] = layer, "catalogue"
assert ("catalogue", want) in ad.built_against(), ad.built_against()
os.environ["PHOREPOC_ADAPTATIONS"] = ""
os.environ.pop("PHOREPOC_ADAPTATION_NAMES")


def run(*args):
    return subprocess.run([sys.executable, TOOL, *args], capture_output=True, text=True)


# rom-args: --data pairs with Windows image paths and unpacked host files
r = run("rom-args", "--arch", "arm", "--into", os.path.join(tmp, "into"), a1)
assert r.returncode == 0, r.stderr
lines = r.stdout.split("\n")[:-1]
assert lines[0::2] == ["--data"] * 3
pairs = dict(l.split("=", 1) for l in lines[1::2])
assert sorted(pairs) == sorted(k.replace("/", "\\") for k in files), pairs
for z, host in pairs.items():
    assert open(host, "rb").read() == files[z.replace("\\", "/")]

# another architecture is refused
r = run("rom-args", "--arch", "x32", "--into", os.path.join(tmp, "into2"), a1)
assert r.returncode != 0 and "built for arm" in r.stderr, r.stderr


def archive(manifest_files, members):
    p = os.path.join(tmp, "bad%d.tar.gz" % len(os.listdir(tmp)))
    with tarfile.open(p, "w:gz") as t:
        for n, d in [("ADDON.txt", ("format=1\nname=bad\narch=arm\n[files]\n" + manifest_files).encode())] + members:
            ti = tarfile.TarInfo(n)
            ti.size = len(d)
            t.addfile(ti, io.BytesIO(d))
    return p


# an unsafe path, and a listed file that is not there
for entry, members, why in [("../evil.dll=files/x\n", [("files/x", b"1")], "unsafe"),
                            ("sys/bin/x.dll=/etc/passwd\n", [], "unsafe"),
                            ("sys/bin/x.dll=files/sys/bin/x.dll\n", [], "not in the archive")]:
    r = run("rom-args", "--arch", "arm", "--into", os.path.join(tmp, "into3"), archive(entry, members))
    assert r.returncode != 0 and why in r.stderr, (entry, r.stderr)

# not an add-on archive at all
plain = os.path.join(tmp, "plain.tar.gz")
with tarfile.open(plain, "w:gz") as t:
    pass
r = run("info", plain)
assert r.returncode != 0 and "not an add-on" in r.stderr
print("addon_test: ok")
