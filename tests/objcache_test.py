# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Checks the rule that decides whether an object is rebuilt (tools/emitter/objcache.py): by content, never by time.
# Run: python3 pdk/tests/objcache_test.py
import os
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "..", "tools", "emitter"))
import objcache as OC  # noqa: E402

OC.compiler_id = lambda cmd0: "cc-1"     # (no compiler needed here)


def write(path, text):
    with open(path, "w") as fh:
        fh.write(text)


def main() -> int:
    with tempfile.TemporaryDirectory() as d0:
        d = d0.replace(os.sep, "/")        # (make-syntax depfiles use forward slashes)
        src, hdr, obj = (os.path.join(d, n) for n in ("a.cpp", "a.h", "a.o"))
        write(src, "int a;\n")
        write(hdr, "#define A 1\n")
        write(obj, "object")
        write(obj + ".d", "a.o: %s %s\n" % (src, hdr))
        key = OC.make_key("gcc", ["gcc", "-O2", "-I/os/inc", src])
        assert OC.check(obj, key) == "no key"
        OC.record(obj, key)
        assert OC.check(obj, key) == ""
        # time proves nothing: touching a file, or making it older than the object, changes nothing
        os.utime(hdr, (time.time() + 1000, time.time() + 1000))
        os.utime(src, (1, 1))
        OC._FILE_HASH.clear()
        assert OC.check(obj, key) == "", "a newer mtime with the same bytes must not rebuild"
        # a changed byte does, whatever the mtime says
        write(hdr, "#define A 2\n")
        os.utime(hdr, (1, 1))
        OC._FILE_HASH.clear()
        assert OC.check(obj, key).startswith("a file it was made from changed"), OC.check(obj, key)
        # the same bytes back: reusable again
        write(hdr, "#define A 1\n")
        OC._FILE_HASH.clear()
        assert OC.check(obj, key) == ""
        # a different command line or compiler is a different object
        assert OC.check(obj, OC.make_key("gcc", ["gcc", "-O1", "-I/os/inc", src])) == "compiler or command changed"
        saved = OC.compiler_id
        OC.compiler_id = lambda cmd0: "cc-2"
        assert OC.check(obj, OC.make_key("gcc", ["gcc", "-O2", "-I/os/inc", src])) == "compiler or command changed"
        OC.compiler_id = saved
        # a header that is gone
        os.remove(hdr)
        OC._FILE_HASH.clear()
        assert OC.check(obj, key) != ""
        write(hdr, "#define A 1\n")
        OC._FILE_HASH.clear()
        # the build's mount points and prefix maps are not part of the key: the core layer (/os, no adaptations) and a
        # board build (merged view, /adapt/N) reuse each other's objects
        OC._ADAPT_NAMES[:] = ["soc", "board"]
        k1 = OC.make_key("gcc", ["gcc", "-I/os/toolchain/overlay", "-fmacro-prefix-map=/core/os/=os/", src])
        k2 = OC.make_key("gcc", ["gcc", "-I/build/osview/toolchain/overlay", "-fmacro-prefix-map=/adapt/1/os/=os/",
                                 "-fmacro-prefix-map=/build/=build/", src])
        assert k1 != k2, "different prefix map targets are different keys"
        k3 = OC.make_key("gcc", ["gcc", "-I/build/osview/toolchain/overlay", "-fmacro-prefix-map=/adapt/1/os/=os/", src])
        assert k1 == k3, (k1, k3)
        assert OC.norm("-I/adapt/1/os/src/x") == "-I/os/src/x" and OC.norm("/adapt/1/registry") == "/@board/registry"
        # the depfile of a header found through the per-container case shim is pinned to the real path
        if os.name != "nt" and os.path.isdir("/tmp"):
            with tempfile.TemporaryDirectory(dir="/tmp") as shim:
                link = os.path.join(shim, "real.h")
                os.symlink(hdr, link)
                write(obj + ".d", "a.o: %s %s\n" % (src, link))
                OC.pin_depfile(obj)
                assert OC.read_deps(obj) == [src, os.path.realpath(hdr)], OC.read_deps(obj)
    print("objcache_test: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
