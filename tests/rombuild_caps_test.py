# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Checks the capability rule of the ROM builder (tools/rombuild.py): with <build>/rom/caps.tsv present the ROM carries the mmp
# capabilities (core images efile.exe / e32strt.exe get the words of f32caps.mmh / estart.mmp, unlisted images the 20 defined
# bits), with ALLCAPS=1 or no map the old header rule applies. Run: python3 pdk/tests/rombuild_caps_test.py
import importlib.util
import os
import sys
import tempfile
import types

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("rombuild", os.path.join(HERE, "..", "tools", "rombuild.py"))
rb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rb)


def fresh(env, tsv):
    for k in ("ALLCAPS", "HONOUR_CAPS", "ROM_CAPS_TSV", "PHOREPOC_BUILD"):
        os.environ.pop(k, None)
    os.environ.update(env)
    if tsv is not None:
        d = tempfile.mkdtemp()
        p = os.path.join(d, "caps.tsv")
        with open(p, "w", newline="\n") as fh:
            fh.write(tsv)
        os.environ["ROM_CAPS_TSV"] = p
    rb._CAPS_MAP = None


TSV = "dosserver.exe\t783678\nefat32.fsy\t2823\n"
fail = 0


def check(what, got, want):
    global fail
    if got != want:
        print("FAIL", what, got, want)
        fail += 1


fresh({}, TSV)
check("mapped exe", rb._cap_override("Sys\\Bin\\dosserver.exe"), (783678, 0))
check("mapped fsy", rb._cap_override("sys/bin/efat32.fsy"), (2823, 0))
check("efile core", rb._cap_override("Sys\\Bin\\efile.exe"), (0xB07, 0))
check("estart core", rb._cap_override("Sys\\Bin\\e32strt.exe"), (0xB25, 0))
check("unlisted gets the 20 defined bits", rb._cap_override("Sys\\Bin\\euser.dll"), (0xFFFFF, 0))
fresh({"ALLCAPS": "1"}, TSV)
check("ALLCAPS=1", rb._cap_override("Sys\\Bin\\dosserver.exe"), None)
fresh({"HONOUR_CAPS": "0"}, TSV)
check("HONOUR_CAPS=0", rb._cap_override("Sys\\Bin\\dosserver.exe"), None)
fresh({}, None)
os.environ["ROM_CAPS_TSV"] = os.path.join(tempfile.mkdtemp(), "none.tsv")
rb._CAPS_MAP = None
check("no map = header rule", rb._cap_override("Sys\\Bin\\dosserver.exe"), None)
img = types.SimpleNamespace(is_exe=True, cap1=5, cap2=0)
check("header caps of an EXE", rb._header_caps(img, 0xFFFFFFFF, 0xFFFFFFFF), (5, 0))
img = types.SimpleNamespace(is_exe=False, cap1=5, cap2=0)
check("a DLL keeps the fallback", rb._header_caps(img, 0xFFFFFFFF, 0xFFFFFFFF), (0xFFFFFFFF, 0xFFFFFFFF))
print("rombuild_caps_test:", "FAILED %d" % fail if fail else "ok")
sys.exit(1 if fail else 0)
