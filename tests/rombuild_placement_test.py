# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
#
# Checks the stable file placement of the ROM builder (tools/rombuild.py SlotMap): a file keeps its slot while it fits,
# an outgrown or new file goes behind everything placed before, FULL=1 repacks, a map for another region start is dropped.
# Run: python3 pdk/tests/rombuild_placement_test.py
import importlib.util
import os
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("rombuild", os.path.join(HERE, "..", "tools", "rombuild.py"))
rb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rb)

path = os.path.join(tempfile.mkdtemp(), "map.json")
BASE = 0x80400000


def build(sizes, fresh=False, region=BASE):
    s = rb.SlotMap(path, fresh=fresh)
    s.begin(region)
    at = {n: s.reserve(n, sz) for n, sz in sizes.items()}
    s.save()
    return at, s


first, s1 = build({"a.dll": 5000, "b.dll": 100000, "c.dll": 700})
assert s1.kept == 0 and sorted(first.values()) == list(first.values()) and first["a.dll"] == BASE
# every slot is disjoint
spans = sorted(s1.new.values())
assert all(a + b <= c for (a, b), (c, _) in zip(spans, spans[1:])), spans

# a small change: everything stays
again, s2 = build({"a.dll": 5100, "b.dll": 99000, "c.dll": 700})
assert again == first and s2.kept == 3 and not s2.moved

# b outgrows its slot, d is new: both go to the end; a and c do not move
third, s3 = build({"a.dll": 5100, "b.dll": 400000, "c.dll": 700, "d.dll": 64})
assert third["a.dll"] == first["a.dll"] and third["c.dll"] == first["c.dll"]
assert third["b.dll"] >= max(a + b for a, b in s2.new.values()) and third["d.dll"] > third["b.dll"]
assert sorted(s3.moved) == ["b.dll", "d.dll"]

# a vanished file leaves a hole, the others stay
fourth, s4 = build({"a.dll": 5100, "c.dll": 700})
assert fourth["a.dll"] == first["a.dll"] and fourth["c.dll"] == first["c.dll"]

# FULL=1: compact repack, no slack reuse of old addresses beyond the fresh layout
packed, s5 = build({"a.dll": 5100, "c.dll": 700, "b.dll": 400000}, fresh=True)
assert s5.kept == 0 and packed["a.dll"] == BASE

# another region start: the old map is dropped
moved, s6 = build({"a.dll": 5100, "c.dll": 700}, region=BASE + 0x100000)
assert s6.kept == 0 and moved["a.dll"] == BASE + 0x100000

# the same name twice gets two slots
dup, s7 = build({"x.dll": 100})
s = rb.SlotMap(None)
s.begin(BASE)
assert s.reserve("X.dll", 100) != s.reserve("x.dll", 100)
print("rombuild placement: PASS")
