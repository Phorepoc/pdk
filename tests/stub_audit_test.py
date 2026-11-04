# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Checks the stub ratchet (tools/stub_audit.py check): a new link stub, a stale allowlist line, an image without a
# record and exception-runtime stubs that are not allowed each fail; the allowed state passes.
# Run: python3 pdk/tests/stub_audit_test.py
import os
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
TOOLS = os.path.join(HERE, "..", "tools")


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", newline="\n") as fh:
        fh.write(text)


def main() -> int:
    with tempfile.TemporaryDirectory() as build:
        os.environ["PHOREPOC_BUILD"] = build
        sys.path.insert(0, TOOLS)
        import stub_audit as SA

        allow_dir = os.path.join(build, "allow")
        SA.ALLOW_DIR = allow_dir
        write(os.path.join(allow_dir, "reasons.txt"), "R1 | ^m3g | a reason | docs/stub-audit.md#r1\n"
                                                       "R2 | ^board | a board provides it | docs/stub-audit.md#r2 | board\n")
        write(os.path.join(allow_dir, "arm.allow"), "#eh allowed\nR1 a.dll m3gThing\nR2 b.dll boardThing\n")
        img = os.path.join(build, "arm", "img")
        for name, stubs in (("a.dll", "m3gThing\n__cxa_begin_cleanup\n"), ("b.dll", "__exidx_end\n"), ("c.dll", "")):
            write(os.path.join(img, name), "x")
            write(os.path.join(img, name + ".meta"), "stub=0x0")
            write(os.path.join(img, name + ".linkstubs"), stubs)
        SA.BUILD = build
        drops = set()
        SA.registry_drops = lambda arch: set(drops)       # the registry's dropped sources (kind 5)

        def run(**kw):
            return SA.cmd_check("arm", None, **kw)

        # b.dll no longer needs boardThing, but its reason is flagged `board`: not stale; everything else matches
        assert run() == 0, "the allowed state must pass"
        write(os.path.join(img, "c.dll.linkstubs"), "brandNewStub\n")
        assert run() == 1, "a new link stub must fail"
        write(os.path.join(img, "c.dll.linkstubs"), "")
        write(os.path.join(allow_dir, "arm.allow"), "#eh allowed\nR1 a.dll m3gThing\nR1 d.dll m3gGone\n")
        assert run() == 1, "an allowlist line nothing needs must fail on a board build"
        assert run(core=True) == 0, "...but not on a core build"
        write(os.path.join(allow_dir, "arm.allow"), "#ceiling eh 1\nR1 a.dll m3gThing\n")
        assert run(core=True) == 1, "exception-runtime stubs without '#eh allowed' must fail"
        write(os.path.join(allow_dir, "arm.allow"), "#eh allowed\nR1 a.dll m3gThing\n")
        os.remove(os.path.join(img, "c.dll.linkstubs"))
        assert run() == 1, "an image without a record must fail"
        # kind 5: a dropped source needs a line with a known reason; a line nothing drops any more fails (board builds)
        write(os.path.join(img, "c.dll.linkstubs"), "")
        write(os.path.join(allow_dir, "arm.allow"), "#eh allowed\nR1 a.dll m3gThing\n")
        write(os.path.join(allow_dir, "dropped-reasons.txt"), "D1 | a patched copy | not a stub | docs\n")
        write(os.path.join(allow_dir, "dropped.allow"), "D1 foo bar.cpp\n")
        drops.add(("foo", "bar.cpp"))
        assert run() == 0, "a listed dropped source must pass"
        drops.add(("foo", "new.cpp"))
        assert run() == 1, "a dropped source with no line must fail"
        drops.discard(("foo", "new.cpp"))
        write(os.path.join(allow_dir, "dropped.allow"), "D9 foo bar.cpp\n")
        assert run() == 1, "a dropped line with an unknown reason must fail"
        write(os.path.join(allow_dir, "dropped.allow"), "D1 foo bar.cpp\nD1 foo old.cpp\n")
        assert run() == 1, "a dropped line nothing drops any more must fail on a board build"
        assert run(core=True) == 0, "...but not on a core build"
    print("stub_audit_test: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
