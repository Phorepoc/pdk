# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""crml2cenrep -- generate Central Repository initialisation files from S60 crml/confml.

S60 components describe their repositories as XML: `<x>_<UID>.crml` (keys, types, access
policies, backup flag) + `<x>.confml` (the settings' default values). The ConE tool
turns those into the `Z:\\private\\10202be9\\<uid>.txt` files at image-creation time. This
is the small subset of ConE's crml implementation the 2010 tree needs:

  repository  uidValue, owner, <access type=R|W capabilities=... sid=...>  -> [owner]/[platsec]
  key         int=<id> type=int|real|string|string8|binary, readOnly, backup=true -> meta
              0x01000000, ref="Feature/Setting" -> default value from <data> in the confml
  bit keys / keyRange (sequences)   -> a range policy line only (no rows by default)

A repository .txt SHIPPED in the tree always wins over a generated one.

    python3 tools/crml2cenrep.py <outdir> [package ...]     # default: all sf.mw + sf.os packages
"""
from __future__ import annotations

import os
import re
import sys
import xml.etree.ElementTree as ET

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import SYMBIAN  # noqa: E402

BASE = os.path.join(SYMBIAN, "symbian-2010")
SKIP = re.compile(r"[\\/](test|tsrc|tef|internal)[\\/]", re.I)


def _strip(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def _parse(path: str):
    raw = open(path, "rb").read()
    try:
        return ET.fromstring(raw)
    except ET.ParseError:
        # some files declare UTF-16 but are 8-bit (or vice versa): retry decoded
        for enc in ("utf-16", "utf-8", "latin-1"):
            try:
                txt = raw.decode(enc)
                txt = re.sub(r"<\?xml[^>]*\?>", "", txt, count=1)
                return ET.fromstring(txt.encode("utf-8"))
            except Exception:
                continue
    return None


def confml_defaults(paths: list[str]) -> dict:
    """'Feature/Setting' -> default value text, from every <data> block. A SEQUENCE setting (the
    element repeats, each with sub-settings) -> a list of {sub-setting: text}, one per item."""
    out: dict = {}
    for p in paths:
        root = _parse(p)
        if root is None:
            continue
        for data in root.iter():
            if _strip(data.tag) != "data":
                continue
            for feat in data:
                seqs: dict[str, list] = {}
                for setting in feat:
                    ref = f"{_strip(feat.tag)}/{_strip(setting.tag)}"
                    if len(setting) == 0:
                        out[ref] = setting.text or ""
                    elif (setting.get("template") or "").lower() != "true":
                        seqs.setdefault(ref, []).append({_strip(s.tag): s.text or "" for s in setting})
                out.update(seqs)
    return out


def _policy(elems) -> str:
    parts = []
    # read policies first: CIniFileIn parses "<rd...> <wr...>" in that order, and a cap_rd that
    # FOLLOWS a cap_wr is taken for the next setting ("Invalid single setting id" -> KErrCorrupt)
    elems = sorted(elems, key=lambda a: not a.get("type", "R").upper().startswith("R"))
    for a in elems:
        kind = "rd" if a.get("type", "R").upper().startswith("R") else "wr"
        # several capabilities are space- (or comma-) separated in the crml; the ini wants them comma-separated --
        # stripping the spaces glued them into one bogus name ("WriteDeviceDataPowerMgmtProtServ", 101F8762)
        # -> the repository failed to parse (KErrCorrupt) -> SSM's InitPsKeys failed -> the boot shut down
        caps = ",".join(c for c in re.split(r"[\s,]+", a.get("capabilities") or "") if c)
        sid = a.get("sid")
        if sid:
            parts.append(f"sid_{kind}={sid}")
        if caps:
            low = caps.lower()
            parts.append(f"cap_{kind}=" + (low if low in ("alwayspass", "alwaysfail") else caps))
    # the same policy twice (widgetinstaller_1028281F.crml repeats its <access type="R">): a second
    # cap_rd is taken for the next setting -> KErrCorrupt, the whole repository fails to open
    return " ".join(dict.fromkeys(parts))


def _value(typ: str, text: str | None) -> str:
    t = (text or "").strip()
    if typ in ("int", "selection", "boolean"):
        if t.lower() in ("true", "false"):
            return "1" if t.lower() == "true" else "0"
        try:
            return str(int(t, 0))
        except ValueError:
            return "0"
    if typ == "real":
        try:
            return repr(float(t))
        except ValueError:
            return "0.0"
    if typ == "binary":
        h = re.sub(r"[^0-9a-fA-F]", "", t)
        return h if h and len(h) % 2 == 0 else "-"
    # confml <data> is ALREADY in the cenrep-ini escaped form (httpcachemanager.confml: `c:\\system\\cache`
    # with two literal backslashes) and ConE copies it verbatim: escaping again doubled every backslash ->
    # the server read `c:\\system\\cache` -> RFs::MkDirAll KErrBadName (the browser cache; 27 repos).
    # Only a bare quote needs escaping.
    return '"' + re.sub(r'(?<!\\)"', '\\"', text or "") + '"'


def _sequence_rows(rng, items, pol: str) -> list[str]:
    """A sequence keyRange's rows, as ConE lays them out: item i's sub-key <key int=s> lives at
    firstInt + ((firstIndex + i) << lowest set bit of indexBits) | s. (Phonebook2's store
    configuration is two such sequences; without rows FindL found nothing -> KErrNotFound.)"""
    if not isinstance(items, list) or not rng.get("indexBits"):
        return []
    first, bits = int(rng.get("firstInt"), 0), int(rng.get("indexBits"), 0)
    shift = (bits & -bits).bit_length() - 1
    base = int(rng.get("firstIndex") or "0", 0)
    backup = (rng.get("backup") or "").lower() == "true"
    rows = []
    for i, item in enumerate(items):
        for sub in (c for c in rng if _strip(c.tag) == "key" and c.get("int")):
            if sub.get("ref") not in item:
                continue
            typ = (sub.get("type") or "int").lower()
            ctype = {"string8": "string8", "binary": "binary", "real": "real", "string": "string"}.get(typ, "int")
            key = first + (((base + i) << shift) & bits) | int(sub.get("int"), 0)
            meta = 0x01000000 if backup or (sub.get("backup") or "").lower() == "true" else 0
            rows.append(f"0x{key:08x} {ctype} {_value(typ, item[sub.get('ref')])} {meta}" + (" " + pol if pol else ""))
    return rows


def generate(crml: str, defaults: dict) -> tuple[str, str] | None:
    root = _parse(crml)
    if root is None or _strip(root.tag) != "repository":
        return None
    uid = root.get("uidValue")
    if not uid:
        return None
    uid = "%08x" % int(uid, 0)
    lines = ["cenrep", "version 1"]
    if root.get("owner"):
        lines += ["[owner]", root.get("owner")]
    lines += ["[defaultmeta]", " 0"]
    ranges, rows = [], []
    for el in root:
        tag = _strip(el.tag)
        acc = [c for c in el if _strip(c.tag) == "access"]
        if tag == "keyRange":
            pol = _policy(acc)
            if pol:
                ranges.append(f"{el.get('firstInt')} {el.get('lastInt')} {pol}")
            rows += _sequence_rows(el, defaults.get(el.get("ref") or ""), pol)
        elif tag == "key" and el.get("int"):
            typ = (el.get("type") or "int").lower()
            bits = [c for c in el if _strip(c.tag) == "bit"]
            if bits and typ != "binary":
                typ = "int"
            ctype = {"string8": "string8", "binary": "binary", "real": "real",
                     "string": "string"}.get(typ, "int")
            meta = 0x01000000 if (el.get("backup") or "").lower() == "true" else 0
            dflt = defaults.get(el.get("ref") or "")
            val = _value(typ, dflt if isinstance(dflt, str) else None)
            if bits:
                # a bit-field key: <bit ref="Feature/Setting">n</bit> = bit n, 1-based, set when the confml
                # boolean is true (value="false" inverts). type=binary stores the 32-bit word big-endian --
                # ProfileEngUtils::GetUint32Presentation. The 2010 confml gives 00FEB1EF for 101F8798.
                word = 0
                for b in bits:
                    on = str(defaults.get(b.get("ref") or "") or "").strip().lower() in ("true", "1")
                    if (b.get("value") or "").lower() == "false":
                        on = not on
                    if on:
                        word |= 1 << (int(b.text, 0) - 1)
                val = "%08X" % word if typ == "binary" else str(word)
            pol = _policy(acc)
            rows.append(f"{el.get('int')} {ctype} {val} {meta}" + (" " + pol if pol else ""))
    top = _policy([c for c in root if _strip(c.tag) == "access"])
    lines += ["[platsec]", " " + (top or "cap_rd=alwayspass cap_wr=alwayspass")] + ranges
    lines += ["[Main]"] + rows
    return uid, "\r\n".join(lines) + "\r\n"


# Repositories whose SHIPPED .txt is stale: the package's .iby comments the .txt out and ConE builds the ROM
# copy from the crml. 101F874B (Calendar): the .txt has key 0x11 as a string, the crml + code an int.
CRML_WINS = {"101f874b"}


def run(outdir: str, pkgs: list[str], only: set[str] | None = None) -> None:
    os.makedirs(outdir, exist_ok=True)
    shipped, crmls, confmls = {}, [], []
    for pkg in pkgs:
        for root, _d, files in os.walk(os.path.join(BASE, pkg)):
            if SKIP.search(root + os.sep):
                continue
            for f in files:
                low = f.lower()
                p = os.path.join(root, f)
                if low.endswith(".crml"):
                    crmls.append(p)
                elif low.endswith(".confml"):
                    confmls.append(p)
                elif re.fullmatch(r"[0-9a-f]{8}\.txt", low) and "cenrep" in root.lower():
                    shipped.setdefault(low[:8], p)
    for uid in CRML_WINS:
        shipped.pop(uid, None)
    if only is not None:
        shipped = {u: p for u, p in shipped.items() if u in only}
    defaults = confml_defaults(confmls)
    n_gen = 0
    for c in crmls:
        got = generate(c, defaults)
        if not got or got[0] in shipped or (only is not None and got[0] not in only):
            continue
        with open(os.path.join(outdir, got[0] + ".txt"), "wb") as fh:
            fh.write(b"\xff\xfe" + got[1].encode("utf-16-le"))
        n_gen += 1
    for uid, p in shipped.items():
        with open(p, "rb") as src, open(os.path.join(outdir, uid + ".txt"), "wb") as dst:
            dst.write(src.read())
    print(f"cenrep: {n_gen} generated from crml/confml + {len(shipped)} shipped -> {outdir}")


if __name__ == "__main__":
    out = sys.argv[1]
    # + the S^3 application packages (BRANCH_RCL_3: File Manager's 1020506B), after
    # the 2010 layers. (The RCL_3 middleware worktrees stay out of the default: their repositories
    # would replace the ones the working UI runs on today -- a separate, tested switch.)
    rcl3 = os.path.join(os.path.dirname(BASE), "symbian-rcl3")
    # RCL_3 middleware whose repositories are NEW to the build (nothing earlier to displace):
    # the remote storage framework's 101F9775 (File Manager's engine opens it at start)
    # + mw.homescreensrv: the homescreen / menu run on RCL_3's; its repositories (HSPS 200159C9, Active
    # Idle 10275102, MCS 101F8847) had been added by hand with --only -- a full regeneration dropped them
    # + mw.inputmethods: the ROM runs RCL_3's real AknFep, but 101F876D came from the 2010 fepstub's
    # crml -- 26 keys short (no KAknFepLastUsedPortraitInput 0x26 ...): the VKB never opened. Its only repository.
    RCL3_NEW_MW = {"oss.FCL.sf.mw.remotestorage", "oss.FCL.sf.mw.homescreensrv", "oss.FCL.sf.mw.inputmethods"}
    default_pkgs = (sorted(d for d in os.listdir(BASE) if re.match(r"oss\.FCL\.sf\.(mw|os)\.", d))
                            + sorted("../symbian-rcl3/" + d for d in os.listdir(rcl3)
                                     if re.match(r"oss\.FCL\.sf\.app\.", d) or d in RCL3_NEW_MW))
    # --only=<uid>[,<uid>...]: (re)write just these repositories into an existing <outdir>
    only = next((a.split("=", 1)[1] for a in sys.argv[2:] if a.startswith("--only=")), None)
    pkgs = [a for a in sys.argv[2:] if not a.startswith("--only=")] or default_pkgs
    run(out, pkgs, {u.lower() for u in only.split(",")} if only is not None else None)
