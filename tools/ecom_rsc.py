# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""ecom_rsc -- compile an ECOM registration .rss (RESOURCE REGISTRY_INFO) to .rsc.

We have no rcomp; ECOM registration files are one fixed struct family
(ecom/registryinfo.rh, registryinfov2.rh), so this emits them directly.

Container = the standard Symbian resource file bafl reads (BaRscImpl.cpp
ReadHeaderAndResourceIndexL, UID1 0x101f4a6b arm):
    [0..16)  TCheckedUid: 0x101f4a6b, UID2, UID3, checksum
    [16]     flags (0)           [17..19) size of largest resource (u16)
    [19..)   bit-array "resource contains compressed unicode" ((n+7)/8 bytes, all 0)
    ...      resource data (uncompressed)
    index    (n+1) u16 file offsets; the LAST u16 = offset of the index itself
UID2 = ECOM_RESOURCE_FILE 0x101F747D, UID3 = the plug-in DLL UID (rcomp `UID2`/`UID3`).

Resource 1 payload (CRegistrar::ParseL / CParseImplementationDataFormatVersionN):
    v1: LONG dll_uid                      v2: LONG 0x101FB0B9, LONG dll_uid
    WORD n_interfaces { LONG interface_uid, WORD n_impls {
        LONG impl_uid, BYTE version, LTEXT display_name, LTEXT8 default_data,
        LTEXT8 opaque_data [, BYTE rom_only (v2)] } }
LTEXT = BYTE len + (0xAB pad if the text would start at an odd offset) + UTF-16LE.

    python3 tools/ecom_rsc.py <in.rss> <out.rsc>
"""
import re, struct, sys

KUidEComResourceFormatV2 = 0x101FB0B9
ECOM_RESOURCE_FILE = 0x101F747D


def uid_checksum(u1, u2, u3):
    def crc(data):
        c = 0
        for b in data:
            c ^= b << 8
            for _ in range(8):
                c = ((c << 1) ^ 0x1021) & 0xFFFF if c & 0x8000 else (c << 1) & 0xFFFF
        return c
    raw = struct.pack("<III", u1, u2, u3)
    return (crc(raw[1::2]) << 16) | crc(raw[0::2])


def _num(v):
    v = v.strip()
    return int(v, 16) if v.lower().startswith("0x") else int(v)


def parse_rss(text):
    text = re.sub(r"/\*.*?\*/", "", text, flags=re.S)
    text = re.sub(r"//[^\n]*", "", text)
    v2 = "RESOURCE_FORMAT_VERSION_2" in text or "registryinfov2" in text.lower()
    reg = {"dll_uid": 0, "interfaces": [], "v2": v2}
    for m in re.finditer(r'(\w+)\s*=\s*("(?:[^"\\]|\\.)*"|[^;{}]+?)\s*;', text):
        k, v = m.group(1), m.group(2)
        if k == "dll_uid":
            reg["dll_uid"] = _num(v)
        elif k == "interface_uid":
            reg["interfaces"].append({"uid": _num(v), "impls": []})
        elif k == "implementation_uid":
            reg["interfaces"][-1]["impls"].append(
                {"uid": _num(v), "version": 1, "name": "", "default": "", "opaque": "", "rom_only": 0})
        elif k in ("version_no", "rom_only"):
            reg["interfaces"][-1]["impls"][-1]["version" if k == "version_no" else "rom_only"] = _num(v)
        elif k in ("display_name", "default_data", "opaque_data"):
            key = {"display_name": "name", "default_data": "default", "opaque_data": "opaque"}[k]
            reg["interfaces"][-1]["impls"][-1][key] = v[1:-1] if v.startswith('"') else ""
    return reg


def resource_payload(reg):
    b = bytearray()
    if reg["v2"]:
        b += struct.pack("<I", KUidEComResourceFormatV2)
    b += struct.pack("<IH", reg["dll_uid"], len(reg["interfaces"]))
    for itf in reg["interfaces"]:
        b += struct.pack("<IH", itf["uid"], len(itf["impls"]))
        for im in itf["impls"]:
            b += struct.pack("<IB", im["uid"], im["version"] & 0xFF)
            name = im["name"]
            b.append(len(name))
            if name:
                if len(b) & 1:
                    b.append(0xAB)
                b += name.encode("utf-16-le")
            for s in (im["default"], im["opaque"]):
                raw = s.encode("latin-1")
                b.append(len(raw)); b += raw
            if reg["v2"]:
                b.append(im["rom_only"] & 0xFF)
    return bytes(b)


def build_rsc(resources, uid2, uid3):
    n = len(resources)
    out = bytearray(struct.pack("<IIII", 0x101F4A6B, uid2, uid3, uid_checksum(0x101F4A6B, uid2, uid3)))
    out += struct.pack("<BH", 0, max(len(r) for r in resources))
    out += bytes((n + 7) // 8)
    offs = []
    for r in resources:
        offs.append(len(out)); out += r
    offs.append(len(out))                       # == start of index
    for o in offs:
        out += struct.pack("<H", o)
    return bytes(out)


def compile_rss(src, dst):
    reg = parse_rss(open(src, encoding="latin-1").read())
    if not reg["dll_uid"] or not reg["interfaces"]:
        raise SystemExit(f"{src}: no REGISTRY_INFO parsed")
    data = build_rsc([resource_payload(reg)], ECOM_RESOURCE_FILE, reg["dll_uid"])
    open(dst, "wb").write(data)
    n_impl = sum(len(i["impls"]) for i in reg["interfaces"])
    return reg, n_impl, len(data)


if __name__ == "__main__":
    reg, n_impl, size = compile_rss(sys.argv[1], sys.argv[2])
    print(f"dll_uid=0x{reg['dll_uid']:08X} v{'2' if reg['v2'] else '1'} "
          f"{len(reg['interfaces'])} interfaces / {n_impl} implementations -> {size} B")
