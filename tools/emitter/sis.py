# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""Minimal SISx package writer - emit an installable Symbian SIS from scratch.

A signed/unsigned SIS registers an executable (and its data files) with the software-install subsystem so
the loader will launch it. Emitted from the SISx format description (typed fields), with no Symbian
toolchain (makesis).

SISx is a tree of typed fields. Field wire format:
    [type u32 (omitted for array elements)] [len_low u32] [payload]
len_low is the payload byte count (bit31 set would add a len_high u32 - we stay < 2GB).
Every field is padded to a 4-byte boundary (valid_offset); the length written is the
UNPADDED payload size. Array fields carry one element_type u32 then their elements,
each WITHOUT a type prefix.

Tree we emit (unsigned, one file):
    [16-byte header: uid1=0x10201A7A magic, uid2, uid3, checksum]
    SISContents
      SISCompressed(algorithm=none)  -> raw bytes of the SISController field
        SISController
          SISInfo(uid, vendor, names[], vendor_names[], version, datetime, type, flags)
          SISSupportedOptions([])
          SISSupportedLanguages([EN])
          SISPrerequisites([], [])
          SISProperties([])
          SISInstallBlock(files=[SISFileDes -> our exe], [], [])
          SISDataIndex(0)
      SISData
        [ SISDataUnit [ SISFileData( SISCompressed(algorithm=none) = exe bytes ) ] ]

The compressed controller uses algorithm=none, so no zlib: the payload is the raw
controller bytes and uncompressed_size == the full SISController field size (the
parser asserts it consumes exactly that many bytes). Controller checksums are optional
(parse_contents only reads them if present) so we omit them.

Outer header (16 bytes) is the real Symbian-9.x SISX header: uid1@0 = 0x10201A7A
(fixed magic), uid2@4 = 0 (reserved), uid3@8 = package UID, chk@12 = TCheckedUid
checksum over the 12 UID bytes. Matches the public SIS-9.x spec. The checksum
MUST be valid: a real device's SISX recognizer validates it, and a zero checksum
leaves the file unrecognised (it opens in a document viewer, not SWInstall).
"""

from __future__ import annotations

import hashlib
import struct

# SIS field types
SISString = 1
SISArray = 2
SISCompressed = 3
SISVersion = 4
SISVersionRange = 5
SISDate = 6
SISTime = 7
SISDateTime = 8
SISUid = 9
SISLanguage = 11
SISContents = 12
SISController = 13
SISInfo = 14
SISSupportedLanguages = 15
SISSupportedOptions = 16
SISPrerequisites = 17
SISDependency = 18
SISProperties = 19
SISProperty = 20
SISIf = 26
SISFileDes = 24
SISHash = 25
SISCapabilities = 41
SISInstallBlock = 28
SISData = 30
SISDataUnit = 31
SISFileData = 32
SISSupportedOption = 33
SISControllerChecksum = 34
SISDataChecksum = 35
SISDataIndex = 40
SISBlob = 37

HEADER_MAGIC = 0x10201A7A
LANG_EN = 1
INSTALL_TYPE_INSTALLATION = 0
HASH_SHA1 = 1
OP_INSTALL = 1


def _field(ftype: int, payload: bytes, typed: bool = True) -> bytes:
    """Serialize one SIS field: [type?][len][payload], padded to 4 bytes."""
    hdr = struct.pack("<I", ftype) if typed else b""
    hdr += struct.pack("<I", len(payload))
    raw = hdr + payload
    return raw + b"\x00" * ((-len(raw)) % 4)


def _array(elem_type: int, elements: list[bytes], typed: bool = True) -> bytes:
    """A SISArray: element_type u32 then each element (already serialized no_type)."""
    payload = struct.pack("<I", elem_type) + b"".join(elements)
    return _field(SISArray, payload, typed)


def _u16str(s: str) -> bytes:
    return s.encode("utf-16-le")


SISCertificateChain = 22
SISSignature = 36
SISSignatureAlgorithm = 38
SISSignatureCertificateChain = 39
SHA1_WITH_RSA = "1.2.840.113549.1.1.5"


def _file_des(target: str, data: bytes, deflated: bytes, caps, index: int) -> bytes:
    """One SISFileDescription (an array element). Capabilities only for executables: SWI cross-checks
    them against the E32Image (a missing field fails mid-install)."""
    body = (_field(SISString, _u16str(target))                       # target
            + _field(SISString, _u16str("")))                        # mime type
    if caps is not None:
        body += _field(SISCapabilities, struct.pack("<I", caps))
    body += (_field(SISHash, struct.pack("<I", HASH_SHA1) + _field(SISBlob, hashlib.sha1(data).digest()))
             + struct.pack("<II", OP_INSTALL, 0)                       # operation, options
             + struct.pack("<QQ", len(deflated), len(data))            # stored, uncompressed length
             + struct.pack("<I", index))                               # file index in the data unit
    return _field(SISFileDes, body, typed=False)


def build_package(files, *, app_uid: int, vendor: str, app_name: str,
                  version=(1, 0, 0), signer=None, platform_uid=None) -> bytes:
    """A SISX package. `files` = [(data bytes, target path, caps or None)]; `signer` = (private key,
    certificate DER) from the `cryptography` package -> one SISSignatureCertificateChain.

    Signing follows the release's verifier (mw.appinstall securitymanager.cpp SignedSize + VerifyBlockL):
    the signed bytes are the raw controller AFTER its [type][len] header, from SISInfo through the
    SISInstallBlock (+ SISLogo) -- everything before the first signature chain; RSA PKCS#1 v1.5 over SHA-1
    (signatureverifier.cpp), the raw signature integer as the blob; the chain = the concatenated DER
    certificates. A self-signed chain validates as ESignatureSelfSigned, which the policy (swipolicy.ini)
    accepts with user-grantable capabilities."""
    import datetime
    import zlib
    now = datetime.datetime.now()
    info = _field(SISInfo,
                  _field(SISUid, struct.pack("<I", app_uid))
                  + _field(SISString, _u16str(vendor))
                  + _array(SISString, [_field(SISString, _u16str(app_name), typed=False)])
                  + _array(SISString, [_field(SISString, _u16str(vendor), typed=False)])
                  + _field(SISVersion, struct.pack("<iii", *version))
                  + _field(SISDateTime,
                           _field(SISDate, struct.pack("<HBB", now.year, now.month - 1, now.day - 1))
                           + _field(SISTime, struct.pack("<BBB", now.hour, now.minute, now.second)))
                  + struct.pack("<BB", INSTALL_TYPE_INSTALLATION, 0))
    supported_options = _field(SISSupportedOptions, _array(SISSupportedOption, []))
    # SISLanguage = a 4-byte language id (real SWI reads 4)
    supported_langs = _field(SISSupportedLanguages,
                             _array(SISLanguage, [_field(SISLanguage, struct.pack("<I", LANG_EN), typed=False)]))
    # empty arrays still carry their real element types (SWI rejects a placeholder type)
    # target devices: a platform product UID (SISDependency = uid [+ version range] + names); none -> the
    # installer warns "not compatible with the phone" (skippable). Always with a version range: without one the
    # names array is read type-assumed, and the in-memory parser (ReadMemberArrayL on a TPtrProvider) then skips
    # 4 bytes too many -> installserver panics USER 29 on the device.
    devices = [_field(SISDependency, _field(SISUid, struct.pack("<I", platform_uid))
                      + _field(SISVersionRange, _field(SISVersion, struct.pack("<iii", 0, 0, 0)))
                      + _array(SISString, [_field(SISString, _u16str("Symbian^3"), typed=False)]), typed=False)
               ] if platform_uid else []
    prerequisites = _field(SISPrerequisites, _array(SISDependency, devices) + _array(SISDependency, []))
    properties = _field(SISProperties, _array(SISProperty, []))

    deflated = [zlib.compress(d, 9) for d, _, _ in files]
    descs = [_file_des(t, d, z, c, i) for i, ((d, t, c), z) in enumerate(zip(files, deflated))]
    install_block = _field(SISInstallBlock,
                           _array(SISFileDes, descs) + _array(SISController, []) + _array(SISIf, []))
    signed = info + supported_options + supported_langs + prerequisites + properties + install_block

    chains = b""
    if signer is not None:
        from cryptography.hazmat.primitives import hashes
        from cryptography.hazmat.primitives.asymmetric import padding
        key, cert_der = signer
        sig = key.sign(signed, padding.PKCS1v15(), hashes.SHA1())
        signature = _field(SISSignature,
                           _field(SISSignatureAlgorithm, _field(SISString, _u16str(SHA1_WITH_RSA)))
                           + _field(SISBlob, sig), typed=False)
        chains = _field(SISSignatureCertificateChain,
                        _array(SISSignature, [signature])
                        + _field(SISCertificateChain, _field(SISBlob, cert_der)))

    controller = _field(SISController, signed + chains + _field(SISDataIndex, struct.pack("<I", 0)))
    # deflated controller (algorithm 1) as every real SIS
    compressed_controller = _field(SISCompressed,
                                   struct.pack("<IQ", 1, len(controller)) + zlib.compress(controller, 9))

    file_datas = [_field(SISFileData, _field(SISCompressed, struct.pack("<IQ", 1, len(d)) + z), typed=False)
                  for (d, _, _), z in zip(files, deflated)]
    data_unit = _field(SISDataUnit, _array(SISFileData, file_datas), typed=False)
    data = _field(SISData, _array(SISDataUnit, [data_unit]))

    # CRC-16-CCITT (poly 0x1021, init 0) over each whole field incl. header + padding; checksums first
    from e32image import crc16, uid_checksum
    contents = _field(SISContents,
                      _field(SISControllerChecksum, struct.pack("<H", crc16(0, compressed_controller)))
                      + _field(SISDataChecksum, struct.pack("<H", crc16(0, data)))
                      + compressed_controller + data)
    return struct.pack("<IIII", HEADER_MAGIC, 0, app_uid, uid_checksum(HEADER_MAGIC, 0, app_uid)) + contents


def build_rom_stub(*, pkg_uid: int, vendor: str, name: str, version) -> bytes:
    """A ROM stub SIS (makesis -s, Z:\\system\\install\\*_stub.sis): the raw, uncompressed SISController alone -- no SIS header,
    no data (as the release's stubs, e.g. aiwdialdata_stub.sis). SWI registers the package as installed in ROM at its version,
    so a SIS that depends on it (pkg_uid, version >= x) installs. An empty install block: the files are in ROM already."""
    import datetime
    now = datetime.datetime.now()
    info = _field(SISInfo,
                  _field(SISUid, struct.pack("<I", pkg_uid))
                  + _field(SISString, _u16str(vendor))
                  + _array(SISString, [_field(SISString, _u16str(name), typed=False)])
                  + _array(SISString, [_field(SISString, _u16str(vendor), typed=False)])
                  + _field(SISVersion, struct.pack("<iii", *version))
                  + _field(SISDateTime,
                           _field(SISDate, struct.pack("<HBB", now.year, now.month - 1, now.day - 1))
                           + _field(SISTime, struct.pack("<BBB", now.hour, now.minute, now.second)))
                  + struct.pack("<BB", INSTALL_TYPE_INSTALLATION, 0))
    body = (info
            + _field(SISSupportedOptions, _array(SISSupportedOption, []))
            + _field(SISSupportedLanguages,
                     _array(SISLanguage, [_field(SISLanguage, struct.pack("<I", LANG_EN), typed=False)]))
            + _field(SISPrerequisites, _array(SISDependency, []) + _array(SISDependency, []))
            + _field(SISProperties, _array(SISProperty, []))
            + _field(SISInstallBlock, _array(SISFileDes, []) + _array(SISController, []) + _array(SISIf, []))
            + _field(SISDataIndex, struct.pack("<I", 0)))
    return _field(SISController, body)


def build_sis(exe_bytes: bytes, *, target_path: str = "!:\\sys\\bin\\app.exe",
              app_uid: int = 0xE1234567, vendor: str = "Phorepoc",
              app_name: str = "App",
              version=(1, 0, 0),
              exe_caps: int = 0x1E000, signer=None) -> bytes:
    """The one-executable package (unsigned unless `signer`)."""
    return build_package([(exe_bytes, target_path, exe_caps)], app_uid=app_uid, vendor=vendor,
                         app_name=app_name, version=version, signer=signer)


def make_dev_signer(directory: str):
    """A self-signed RSA-2048 developer certificate, made once and kept in `directory` (key.pem, cert.der)."""
    import datetime
    import os
    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import rsa
    from cryptography.x509.oid import NameOID
    kp, cp = os.path.join(directory, "key.pem"), os.path.join(directory, "cert.der")
    if os.path.isfile(kp) and os.path.isfile(cp):
        key = serialization.load_pem_private_key(open(kp, "rb").read(), None)
        return key, open(cp, "rb").read()
    os.makedirs(directory, exist_ok=True)
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Phorepoc developer"),
                      x509.NameAttribute(NameOID.ORGANIZATION_NAME, "Phorepoc")])
    start = datetime.datetime(2020, 1, 1, tzinfo=datetime.timezone.utc)
    cert = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
            .serial_number(x509.random_serial_number()).not_valid_before(start)
            .not_valid_after(start + datetime.timedelta(days=365 * 30))
            .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
            .sign(key, hashes.SHA256()))   # sha256WithRSA: x509cert.cpp knows it (cryptography refuses SHA-1 certs)
    open(kp, "wb").write(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                           serialization.NoEncryption()))
    der = cert.public_bytes(serialization.Encoding.DER)
    open(cp, "wb").write(der)
    return key, der


def main() -> int:
    """python3 tools/emitter/sis.py <exe> <out.sis>                     (a one-exe unsigned package)
    python3 tools/emitter/sis.py --out x.sis --uid 0xE... --name N [--vendor V] [--sign DIR]
                      --file <host path>=<target>[@<caps hex>] ...  (caps = an executable's capability word)"""
    import argparse
    import sys
    if len(sys.argv) >= 2 and not sys.argv[1].startswith("--"):
        exe = open(sys.argv[1], "rb").read()
        out = sys.argv[2] if len(sys.argv) > 2 else "app.sis"
        open(out, "wb").write(build_sis(exe))
        print(f"Wrote {out}")
        return 0
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", required=True)
    ap.add_argument("--uid", required=True, type=lambda x: int(x, 0))
    ap.add_argument("--name", required=True)
    ap.add_argument("--vendor", default="Phorepoc")
    ap.add_argument("--version", default="1.0.0")
    ap.add_argument("--sign", default="", help="directory of the self-signed developer key (made on first use)")
    ap.add_argument("--file", action="append", default=[])
    a = ap.parse_args()
    files = []
    for f in a.file:
        host, _, rest = f.partition("=")
        target, _, caps = rest.partition("@")
        files.append((open(host, "rb").read(), target, int(caps, 16) if caps else None))
    signer = make_dev_signer(a.sign) if a.sign else None
    pkg = build_package(files, app_uid=a.uid, vendor=a.vendor, app_name=a.name,
                        version=tuple(int(x) for x in a.version.split(".")), signer=signer)
    open(a.out, "wb").write(pkg)
    print(f"Wrote {a.out} ({len(pkg)} bytes, {len(files)} file(s), {'signed' if signer else 'UNSIGNED'})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
