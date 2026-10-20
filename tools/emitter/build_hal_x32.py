# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""build_hal_x32 -- hal.dll (the user-side HAL client) as an x32 E32Image.

The last of EFILE's three dependencies: EFILE imports
`HAL::Get` (halu.def ordinal 1), and sf_cache_man/sf_ldr/sf_utl ask it for EMemoryPageSize /
ECPUSpeed / EPowerGood.

HAL turned out to be almost entirely board-independent: the two real TUs
(halservices/hal/src/{hal_main,userhal}.cpp) compile unmodified, and every handler the config
names lives in that same generic userhal.cpp. The only board-specific inputs are two TEXT files
-- tools/phorepoc-x86/hal/{values.hda,config.hcf} -- which the REAL generator (halcfg.pl) turns
into the attribute-default and attribute-to-handler tables. See port/hal_census_x32.sh.

    python3 tools/emitter/build_hal_x32.py [out.dll]     (default BUILD/x32core/hal_x32.dll; SKIP_CENSUS=1 reuses objects)
"""
from __future__ import annotations

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import x32_client_dll as CD                     # noqa: E402
from paths import resolve                       # noqa: E402

CFG = CD.Cfg(name="hal",
             def_path=os.path.join(resolve("baseline/kernelhwsrv"), "halservices",
                                   "hal", "eabi", "halu.def"),
             uid2=0x1000008D, uid3=0x100039E8,      # hal.mmp UID
             census_marker="=== hal census")


if __name__ == "__main__":
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(CD.EU.CORE_OUT, "hal_x32.dll")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    raise SystemExit(CD.build(CFG, out))
