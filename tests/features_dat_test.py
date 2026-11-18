# Copyright The Phorepoc Authors
# SPDX-License-Identifier: AGPL-3.0-only
#
# Checks tools/features_dat.py's mapping from the feature XML and the product variant's macros to the Feature Manager
# database: a feature with a UniqueRuntimeId is supported iff its macro is defined; the location management runtime feature
# (NFeature::KLocationManagement, 0x10279818: no UniqueRuntimeId in the XML) follows SYMBIAN_EXCLUDE_LOCATION_MANAGEMENT
# inverted (defined = excluded = not supported); the file header and the default supported range are unchanged.
# Run: python3 pdk/tests/features_dat_test.py
import importlib.util
import os
import struct
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
os.environ.setdefault("PHOREPOC_ADAPTATIONS", "")
sys.path.insert(0, os.path.join(HERE, "..", "tools", "emitter"))
spec = importlib.util.spec_from_file_location("features_dat", os.path.join(HERE, "..", "tools", "features_dat.py"))
fd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fd)

XML = """<FeatureSet>
 <FeatureSpecification><FeatureName>__WITH_UID_ON</FeatureName><UniqueRuntimeId>1001</UniqueRuntimeId></FeatureSpecification>
 <FeatureSpecification><FeatureName>__WITH_UID_OFF</FeatureName><UniqueRuntimeId>1002</UniqueRuntimeId></FeatureSpecification>
 <FeatureSpecification><FeatureName>SYMBIAN_EXCLUDE_LOCATION_MANAGEMENT</FeatureName></FeatureSpecification>
 <FeatureSpecification><FeatureName>NO_UID_MACRO</FeatureName></FeatureSpecification>
</FeatureSet>"""

e = fd.spec_entries(XML, {"__WITH_UID_ON", "SYMBIAN_EXCLUDE_LOCATION_MANAGEMENT"})
assert e[1001] == 1 and e[1002] == 0, e
assert e[0x10279818] == 0, "location management must be unsupported when its EXCLUDE macro is defined"
assert set(e) == {1001, 1002, 0x10279818}, e                       # a macro without a runtime id adds nothing else

e = fd.spec_entries(XML, {"__WITH_UID_ON"})
assert e[0x10279818] == 1, "location management is supported when the product does not exclude it"

e = fd.spec_entries("", set())                                       # no XML entry at all: the exclusion macro still decides
assert e == {0x10279818: 1}, e
assert fd.spec_entries("", {"SYMBIAN_EXCLUDE_LOCATION_MANAGEMENT"}) == {0x10279818: 0}

# the default supported range still covers the feature, so an entry (not the range) is what makes it unsupported
lo, hi = fd.DSR[0]
assert lo <= 0x10279818 <= hi
print("features_dat_test: ok")
