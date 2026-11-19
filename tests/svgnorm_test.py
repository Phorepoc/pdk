# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# Checks tools/svgnorm.py: the NVG encoder (svg2nvg.parse_d) drops a path that has an arc command, so mif_gen normalises the status
# indicator art first. Tabler's `mail` body (four rounded corners, all arcs) must come out as a non-empty path of absolute M/L/C/Z
# whose end points are unchanged, and a shifted copy moves by the shift.
# Run: python3 pdk/tests/svgnorm_test.py
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "tools"))
import svg2nvg  # noqa: E402
import svgnorm  # noqa: E402

BODY = "M3 7a2 2 0 0 1 2 -2h14a2 2 0 0 1 2 2v10a2 2 0 0 1 -2 2h-14a2 2 0 0 1 -2 -2v-10"
assert not svg2nvg.parse_d(BODY).segs, "the encoder keeps arc paths now: the normalisation is no longer needed"
n = svgnorm.normalize_d(BODY)
p = svg2nvg.parse_d(n)
assert p.segs, n
assert set(c for c in n if c.isalpha()) <= set("MLCZ"), n
assert n.startswith("M3 7") and n.endswith("L3 7"), n
s = svgnorm.shift_d(n, -1.5, -1.5)
assert s.startswith("M1.5 5.5"), s
assert svgnorm.normalize_d("M12 18l.01 0") == "M12 18 L12.01 18"
print("svgnorm: ok")
