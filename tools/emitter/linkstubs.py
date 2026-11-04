# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""linkstubs -- the stub_audit.py record of an image."""

# names the core-image builders pass the linker as roots/entry placeholders (spike.ld's ENTRY, euser's bench-only export
# slot past the real table): emitter plumbing, in no image's call path -- not stubs of anything
PLUMBING = {"spike_value", "euser_selftest"}


def write_linkstubs(out_path: str, names) -> None:
    """`<image>.linkstubs`: every name the image calls that no provider exports, one per line (empty = none). Every
    image the emitter writes carries one (tools/stub_audit.py check fails on an image without)."""
    with open(out_path + ".linkstubs", "w", newline="\n") as fh:
        fh.write("".join(n + "\n" for n in sorted(set(names) - PLUMBING)))
