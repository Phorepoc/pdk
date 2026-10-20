# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""mkwsini -- write a minimal WSERV wsini.ini (UTF-16LE + BOM) that starts <app> (- = nothing).

    python3 tools/mkwsini.py <out.ini> <app> [extra wsini line ...]
"""
import sys

out, app, *extra = sys.argv[1:]
# app "-" (BOOT=ssm): no startup line -- SSM starts WSERV from its command lists, and the UI after it
lines = ([] if app == "-" else [f"startup {app}"]) + ["logenable 1", "log rd", *extra]
with open(out, "wb") as fh:
    fh.write(b"\xff\xfe" + "".join(l + "\r\n" for l in lines).encode("utf-16-le"))
