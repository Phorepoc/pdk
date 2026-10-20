# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""x32_rom_args -- rombuild arguments for every built registry component.

    python3 tools/x32_rom_args.py > args.txt      (one argument per line, for rombuild @args.txt)

Walks the registry (tools/emitter/components*.py) over the images in BUILD/x32img (ROM_ARCH=arm: BUILD/arm/img):
  * every entry whose image exists -> --xip Sys\\Bin\\<rom>
    (window server slot renames: wserv.exe -> EWSRV.EXE, econswserv.dll -> ECONS.DLL)
  * what the resource step staged per image, which is CPU-independent data: BUILD/romfiles/<image>.romfiles
    (compiled resources), BUILD/plugins/<stem>.rsc next to an ECOM plug-in
  * the generated trees: the default skin (BUILD/skin-gen), the icon files (BUILD/icon-gen), the Z: data the
    packages export (BUILD/zdata-gen), the stub HWRM policies
The modules make_phorepoc_rom.sh adds itself (euser/efsrv/hal/efile/estart) are skipped, and so is the
text window server trio (ewsrv/econs/edisp), whose ROM slots the graphical ones take. mmps are parsed
with the board's variant.mmh on the include path, as build_mmp_x32.py does (the key map needs it).
Board files (variant.mmh, estart.txt, cenrep/) are read from OS_ROOT/tools/phorepoc-x86/board.
"""
import os
import sys
import glob

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import OS_ROOT, SYMBIAN, BUILD, resolve  # noqa: E402
import components as C  # noqa: E402
import mmp as MMP  # noqa: E402

BOARD = os.path.join(OS_ROOT, "tools", "phorepoc-x86", "board")
X32 = os.path.join(BUILD, "x32img")
ROMFILES = os.path.join(BUILD, "romfiles")     # <image>.romfiles: `<rom path>=<host path>` lines of the resource step
PLUGINS = os.path.join(BUILD, "plugins")       # <stem>.rsc: an ECOM plug-in's registration resource
ZGEN = os.path.join(BUILD, "zdata-gen")
ICONS = os.path.join(BUILD, "icon-gen")
SKINGEN = os.path.join(BUILD, "skin-gen")
# ROM_ARCH=arm: the same ROM over the ARM images build_mmp.py writes -- one ROM recipe for every board; the staged
# resources (.romfiles, skin, icons) are shared
ARM = os.environ.get("ROM_ARCH") == "arm"
if ARM:
    X32 = os.path.join(BUILD, "arm", "img")
    # (the S^4 set on Qt 4.8 builds for ARM too -- SHELLS=s4|both; the Qt 4.7 one stays x32-only)
    if os.environ.get("PHOREPOC_S4") == "1" and os.environ.get("PHOREPOC_QT48") != "1":
        raise SystemExit("ROM_ARCH=arm: no Qt 4.7 Symbian^4 ARM build (the S^4 set needs PHOREPOC_QT48=1)")
_EMITTED = set()   # ROM names already listed (see the --xip loop)
_UNBUILT = []      # registered components with no image: skipped, but their data files (.esk, .ini ...) still ship --
                   # ip.ipeventnotifier.esk without ipeventnotifier.prt failed every connection (-1)
# PHOREPOC_S4=1 (Symbian^4): the S^3 ROM + the S^4 components (components_s4.py), whose images live in BUILD/s4/x32img
S4 = os.environ.get("PHOREPOC_S4") == "1"
S4B = os.path.join(BUILD, "qt48" if os.environ.get("PHOREPOC_QT48") == "1" else "s4")   # the Qt 4.8 S^4 set has its own root
X32_S4 = X32 if ARM else os.path.join(S4B, "x32img")   # ARM: build_mmp.py writes every image to one directory
S4_KEYS = set(__import__("components_s4").COMPONENTS) if S4 else set()
DEFS = ("-I", BOARD)
SLOT = {"wserv.exe": "EWSRV.EXE", "econswserv.dll": "ECONS.DLL"}
# NGA=1: the NGA window server stack (registry nga=True keys) ships in
# place of its non-NGA twins; without it no nga=True key is in the ROM (the twins' image names would collide)
NGA = os.environ.get("NGA") == "1"
NGA_TWINS = {"ws32", "wserv", "wsgraphicdrawer", "wsstdplugin", "remotegc", "sgresource", "surfaceupdateclient", "bitgdi"}
# NGA_KEEP=<twin>[,<twin>...]: keep those non-NGA twins (and leave their nga keys out) -- bisecting an NGA ROM
NGA_KEEP = {k for k in os.environ.get("NGA_KEEP", "").split(",") if k}
NGA_TWINS -= NGA_KEEP
OWN = {"euser", "efsrv", "hal", "efile", "estart"}  # make_phorepoc_rom.sh adds these itself
# the TEXT window server trio: the GUI ROM's EWSRV.EXE / ECONS.DLL slots hold wserv / econswserv
TEXT_SHELL = {"ewsrv", "econs", "edisp"}


# ALF=s1b|s2|s3 (1 = s2, 2 = s3; s1 = s1b = s2: the real clients need the server, the compositor needs the alf stage): how much of the real Alfred/Hitchcock set goes in the ROM
ALF_LV = {"s1b": 2, "s1": 2, "1": 2, "s2": 2, "2": 3, "s3": 3}.get(os.environ.get("ALF", "").lower(), 0)

# UI_RENDERER=classic|alfred|both: which render path(s) the image carries. classic = today's
# image (default); alfred = the real Alfred/Hitchcock set only (= ALF=s2: wsini chain alf compositor); both = that set plus the Classic
# chain, chosen at boot by the developer menu's CenRep key 4 (wserv's "CLASSIC:"/"ALFRED:" wsini lines). make_phorepoc_rom.sh exports the
# sanitised value (a non-NGA board has no Alfred: classic) and sets ALF=s2 for alfred|both.
UI_RENDERER = os.environ.get("UI_RENDERER", "classic").lower()
UIR_MASK = {"classic": 1, "alfred": 2, "both": 3}.get(UI_RENDERER, 1)    # key 5 of the dev-menu repository: renderers present
UIR_DEFAULT = 1 if UI_RENDERER == "alfred" else 0                          # key 4 default: Alfred only when it is all there is
if os.environ.get("UIR_TEST") == "1":                                        # mechanism test (make_phorepoc_rom.sh): key 4 / key 5 forced
    UIR_DEFAULT = int(os.environ.get("UIR_KEY4", "0"))
    UIR_MASK = int(os.environ.get("UIR_MASK", "3"))


# SHELLS=s3|s4|both: which UI shell(s) the image carries. s3 = the S^3 homescreen + matrixmenu
# (default); s4 = the Qt/Hb homescreen only; both = both, chosen at boot by the developer menu's CenRep key 1
# (src/pheshellsel). Key 6 of the dev-menu repository = the shells present (mask 1 = S^3, 2 = S^4): the menu offers only those.
SHELLS = os.environ.get("SHELLS", "s3").lower()
SHELLS_MASK = {"s3": 1, "s4": 2, "both": 3}.get(SHELLS, 1)
SHELL_DEFAULT = 1 if SHELLS == "s4" else 0                                 # key 1 default: S^4 only when it is all there is


def rel(p: str) -> str:
    return os.path.abspath(p).replace("\\", "/")


def host_path(p: str) -> str:
    """A host path as the registry and the manifests spell it: absolute, a registry path (paths.resolve), or
    `emitter/<x>` = BUILD/<x> (the resource step's outputs, e.g. emitter/rsc/resource/errorres.rsc)."""
    p = p.replace("\\", "/")
    if os.path.isabs(p):
        return p
    if p.startswith("emitter/"):
        return os.path.join(BUILD, p[len("emitter/"):])
    return resolve(p)


def data_args(lines) -> list[str]:
    """Registry `rom_data` lines (`<rom path>=<host path>`) -> --data arguments with absolute host paths."""
    out = []
    for line in lines:
        z, _, host = line.partition("=")
        out += ["--data", z + "=" + host_path(host)]
    return out


def manifest_path(host: str) -> str:
    """A host path in a resource-step manifest: absolute, or relative to BUILD."""
    return host if os.path.isabs(host) else os.path.join(BUILD, host)


def manifest_args(lines) -> list[str]:
    """`<rom path>=<host path>` lines of a .romfiles manifest -> --data arguments."""
    out = []
    for line in lines:
        z, _, host = line.partition("=")
        out += ["--data", z + "=" + manifest_path(host)]
    return out


def s60_meshpreface(line: str) -> str:
    """CED's mesh preface as an S60 phone has it (mw.ipconnmgmt/inc/meshpreface1.cfg): the network
    tier (2048) is the S60 Tier Manager (0x101F6D4A) and its MCPR the S60 MCPR (0x101F6D4B), which hand
    connection selection to the MPM server -- S^3 apps start connections with TExtendedConnPref lists
    that Symbian's NetMCPR cannot resolve (BrowserNG: IAP 0 -> TierManagerUtils 4). Every other record
    stays as the package export we stage; each substitution must match exactly once."""
    path, _, src = line.partition("=")
    if path.lower() != "system\\data\\meshpreface1.cfg":
        return line
    text = open(host_path(src), encoding="latin-1").read()
    for old, new in (("TierImplUid=271064536", "TierImplUid=0x101F6D4A"),
                     ("MCprUid=0x10274547", "MCprUid=0x101F6D4B")):
        if text.count(old) != 1:
            raise SystemExit(f"s60_meshpreface: expected one {old!r} in {src}")
        text = text.replace(old, new)
    os.makedirs(ZGEN, exist_ok=True)
    out = os.path.join(ZGEN, "meshpreface1_s60.cfg")
    with open(out, "w", encoding="latin-1", newline="") as fh:
        fh.write(text)
    return path + "=" + rel(out)


SSM = os.environ.get("PHOREPOC_SSM") == "1"


def ssm_cmdlists(key: str = "ssmcmdlists") -> list[str]:
    """(BOOT=ssm): the S60 command lists where ssmcmdlists.iby puts them -- the _ext and hw lists merge into
    startup\\0 (wserv_hw.rsc becomes startup\\0\\wserv.rsc), the emulator's wserv_emu is left out. Sources = the
    rcomp output staged in BUILD/romfiles/ssmcmdlists.dll.romfiles, looked up by their DATAZ_ path."""
    # the iby of the SAME tree the lists are built from (RCL_3); key ssmcmdlists_s4 = master's lists, the Symbian^4 boot
    iby = os.path.join(MMP.BASE, os.path.dirname(os.path.dirname(C.COMPONENTS[key]["mmp"])), "rom", "ssmcmdlists.iby")
    man = os.path.join(ROMFILES, key + ".dll.romfiles")
    staged = {}
    for line in open(man).read().split():
        z, _, host = line.partition("=")
        staged[z.lower()] = manifest_path(host)
    out = []
    for line in open(iby, encoding="latin-1"):
        parts = line.split()
        if len(parts) != 2 or not parts[0].lower().startswith("data=dataz_\\"):
            continue
        src = parts[0][len("data=DATAZ_\\"):].lower()
        if src not in staged:
            raise SystemExit(f"x32_rom_args: {src} (ssmcmdlists.iby) was not compiled")
        out += ["--data", parts[1].lstrip("\\") + "=" + staged[src]]
    return out


ROM_TAIL = {k for k in os.environ.get("ROM_TAIL", "").split(",") if k}

# PHETTY=on|off|exclude: the developer tty daemon (phettyd + pheshell + pheterm). on (default) = in
# the ROM and started by the SSM non-critical list; off = in the ROM, phettyd exits at start (CenRep e7015904 key 1 = 0; set it
# to 1 and start phettyd, or run pheterm, to enable it at runtime); exclude = not in the ROM at all (product builds)
PHETTY = os.environ.get("PHETTY", "on").lower()
if PHETTY not in ("on", "off", "exclude"):
    raise SystemExit("PHETTY must be on, off or exclude")
PHETTY_KEYS = set(__import__("components_phetty").PHETTY_KEYS)


def phetty_cenrep() -> str:
    """The phettyd repository (KPheTtyCenRepUid 0xE7015904): key 1 = enabled, key 2 = ttys started at boot; written as the
    UTF-16 txt initialisation file CenRep reads from Z:\\private\\10202be9. Any process may change it (developer feature)."""
    lines = ["cenrep", "version 1", "# phettyd (PHETTY=%s): key 1 enabled, key 2 ttys started at boot" % PHETTY,
             "[defaultmeta]", " 0x00000000", "[platsec]", " cap_rd=alwayspass cap_wr=alwayspass", "[Main]",
             "0x00000001 int %d 0x00000000 cap_rd=alwayspass cap_wr=alwayspass" % (0 if PHETTY == "off" else 1),
             "0x00000002 int 1 0x00000000 cap_rd=alwayspass cap_wr=alwayspass"]
    os.makedirs(ZGEN, exist_ok=True)
    out = os.path.join(ZGEN, "e7015904.txt")
    with open(out, "wb") as fh:
        fh.write(b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
    return rel(out)


def devmenu_cenrep() -> str:
    """The developer-menu repository (KPheDevMenuRepoUid 0xE7015A01, src/phedevmenu/phedevmenu.h): key 1 = UI shell
    (0 = S^3, 1 = S^4), key 2 = Control Panel shortcut (0/1), key 3 = boot guard timeout (s; default 180 = the kernel guard's own), key 4 = UI renderer
    (0 Classic, 1 Alfred; default per UI_RENDERER), key 5 = the renderers this image carries (mask 1 Classic | 2 Alfred), key 6 = the UI
    shells it carries (mask 1 S^3 | 2 S^4, SHELLS=). UTF-16 txt initialisation file in
    Z:/private/10202be9; the app's changes persist to the C: drive's persists dir. Any process may change it (developer feature)."""
    lines = ["cenrep", "version 1", "# phedevmenu: key 1 UI shell, key 2 Control Panel shortcut, key 3 boot guard timeout (s)",
             "[defaultmeta]", " 0x00000000", "[platsec]", " cap_rd=alwayspass cap_wr=alwayspass", "[Main]",
             "0x00000001 int %d 0x00000000 cap_rd=alwayspass cap_wr=alwayspass" % SHELL_DEFAULT,
             "0x00000002 int 0 0x00000000 cap_rd=alwayspass cap_wr=alwayspass",
             "0x00000003 int 180 0x00000000 cap_rd=alwayspass cap_wr=alwayspass",
             "0x00000004 int %d 0x00000000 cap_rd=alwayspass cap_wr=alwayspass" % UIR_DEFAULT,
             "0x00000005 int %d 0x00000000 cap_rd=alwayspass cap_wr=alwayspass" % UIR_MASK,
             "0x00000006 int %d 0x00000000 cap_rd=alwayspass cap_wr=alwayspass" % SHELLS_MASK]
    os.makedirs(ZGEN, exist_ok=True)
    out = os.path.join(ZGEN, "e7015a01.txt")
    with open(out, "wb") as fh:
        fh.write(b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
    return rel(out)


def browser_cenrep() -> str:
    """The web browser's own repository (OURS, KPheBrowserRepoUid 0xE7015B01, phebrctl in webkit-symbian): key 1 = User-Agent string
    (read by phebrctl at engine start; empty = the engine's built-in default, a current Android Chrome). Edit it here or
    persist a new value from a tool; any process may change it."""
    ua = "Mozilla/5.0 (Linux; Android 14; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Mobile Safari/537.36"
    lines = ["cenrep", "version 1", "# phebrowser: key 1 User-Agent string",
             "[defaultmeta]", " 0x00000000", "[platsec]", " cap_rd=alwayspass cap_wr=alwayspass", "[Main]",
             '0x00000001 string "%s" 0x00000000 cap_rd=alwayspass cap_wr=alwayspass' % ua]
    os.makedirs(ZGEN, exist_ok=True)
    out = os.path.join(ZGEN, "e7015b01.txt")
    with open(out, "wb") as fh:
        fh.write(b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le"))
    return rel(out)


def main() -> None:
    out = []
    tail = []   # XIP images placed last (see the --xip loop)
    # ROM_EXCLUDE=<key>[,<key>...]: components that ship as a SIS instead (make_sis.py) -- a ROM file of
    # the same name would eclipse-block the install
    exclude = {k for k in os.environ.get("ROM_EXCLUDE", "").split(",") if k}
    for key, comp in C.COMPONENTS.items():
        if key in OWN or key in TEXT_SHELL or key in exclude:
            continue
        if key in PHETTY_KEYS and PHETTY == "exclude":    # PHETTY=exclude: a product ROM has no developer ttys at all
            continue
        if comp.get("nga") and not NGA:            # the NGA stack only in an NGA=1 ROM
            continue
        # ALF=1: the real Alfred/Hitchcock set only in an ALF=1 ROM (alf="build" = never);
        # its *_real twins (rom_as) replace the null clients, which are then left out
        if comp.get("alf") and (comp["alf"] == "build" or ALF_LV < int(comp["alf"])):
            continue
        if ALF_LV >= 2 and (key in ("alfclient", "alfdecoderserverclient", "libegl", "sflibopenvgrefimpl") or key == "goommonitor" or key == "goommonitor_alfnull" and ALF_LV >= 3):
            continue
        if NGA and comp.get("nga") and key[:-4] in NGA_KEEP and key.endswith("_nga"):
            continue                               # (NGA_KEEP: its non-NGA twin ships instead)
        if NGA and key in NGA_TWINS:               # ... which replaces the non-NGA twins
            continue
        # a device-only (ARM) build: never in an x32 ROM (nor its rom_data) -- except an ARM ROM's named boot test
        if comp.get("device") and not (ARM and (os.environ.get("SSM_TEST") == key or comp.get("device_rom"))):
            continue
        # the System State Manager set only in a BOOT=ssm ROM: ESTART starts SYSSTATEMGR.EXE whenever it exists
        if comp.get("ssm") and not SSM:
            continue
        if key == "e32strt":           # make_phorepoc_rom.sh stages it as Sys\Bin\estart.exe
            continue
        # one set of SSM lists: an S^4 ROM boots with master's (ssmcmdlists_s4), the S^3 ROM with RCL_3's
        if key in ("ssmcmdlists", "ssmcmdlists_s4"):
            # SHELLS=s4|both: the S^3 lists stay (pheshellsel, in the idle slot, starts what the S^4 shell needs: s4start.txt)
            s4_lists = "ssmcmdlists_s4" in C.COMPONENTS and SHELLS == "s3"
            if (key == "ssmcmdlists_s4") == s4_lists:
                out += ssm_cmdlists(key)
            continue
        # extra Z: copies of staged files (`<rom path>=<host path>`): a resource two readers open at two paths
        # (a test component's data only with the test itself: phevidtest's videos were in every ROM)
        if not comp.get("test") or os.environ.get("SSM_TEST") == key or key in ROM_TAIL:
            out += data_args(comp.get("rom_data", []))
        if comp.get("rsc_only"):
            # no image, but its resources are real ROM content (errorres.rsc: without it any CTextResolver
            # error note left KErrNotFound)
            man = os.path.join(ROMFILES, key + ".dll.romfiles")
            if os.path.isfile(man):
                out += manifest_args(open(man).read().split())
            continue
        try:
            m = MMP.parse(os.path.join(MMP.BASE, comp.get("x32_mmp", comp["mmp"])), DEFS)
        except RuntimeError as e:
            print(f"# {key}: {str(e).splitlines()[0]}", file=sys.stderr)
            continue
        rom = C.rom_name(key, m.targettype)
        img = os.path.join(X32_S4 if key in S4_KEYS else X32, rom)
        if not os.path.isfile(img):
            if m.targettype != "lib":
                _UNBUILT.append(key)
            # a STATICLIBRARY has no image but can own resources (WebCore's platform.mmp
            # builds webkit.rsc, which browserengine.dll opens)
            man = os.path.join(ROMFILES, rom + ".romfiles")
            if m.targettype == "lib" and os.path.isfile(man):
                out += manifest_args(open(man).read().split())
            continue
        # two keys can build the same image: it goes in once
        if rom in _EMITTED:
            continue
        _EMITTED.add(rom)
        # test=True components: only in a ROM built with SSM_TEST=<key>, staged as
        # ssmtest.exe -- the noncritical SSM list starts that name fire-and-forget (board/ssm/noncriticalcmdlist_hw.rss)
        # (a test named in ROM_TAIL but not the SSM_TEST is staged under its own name: a helper the boot test runs,
        # e.g. phebrlaunch runs wlanjoin.exe first)
        if comp.get("test"):
            if os.environ.get("SSM_TEST") == key:
                rom = "ssmtest.exe"
            elif key not in ROM_TAIL:
                continue
        elif os.environ.get("SSM_TEST") == key and rom.endswith(".exe"):	# any EXE can be the boot test
            rom = "ssmtest.exe"
        # an NGA twin has its own image file (rom=ws32_nga.dll) and is staged under rom_as (ws32.dll)
        stage = comp.get("rom_as", rom) if NGA else rom
        xip = ["--xip", "Sys\\Bin\\" + SLOT.get(stage, stage) + "=" + rel(img)]
        # the images that change between pushes go LAST (a delta upload: a rebuilt test EXE then
        # changes only the end of the ROM; rombuild puts the data files before the XIP images): test=True
        # components + ROM_TAIL="key,key" (e.g. the GL libraries while they are being worked on)
        if comp.get("test") or key in ROM_TAIL:
            tail += xip
        else:
            out += xip
        if m.targettype == "plugin":
            rsc = os.path.splitext(rom)[0] + ".rsc"
            if os.path.isfile(os.path.join(PLUGINS, rsc)):
                out += ["--data", "Resource\\Plugins\\" + rsc + "=" + rel(os.path.join(PLUGINS, rsc))]
        man = os.path.join(ROMFILES, rom + ".romfiles")
        if not os.path.isfile(man):
            # a component with its own ROM name (`rom=`) has its resources staged under the KEY's name: the
            # GSA normal policy (ssm.state.policy.0001.dll) owns normal\gsanormalcmdlist.rsc
            man = os.path.join(ROMFILES, key + os.path.splitext(rom)[1] + ".romfiles")
        if os.path.isfile(man):
            out += manifest_args(open(man).read().split())
    # the default skin package + its bitmaps, when a skin build left them in BUILD/skin-gen/data
    skin = os.path.join(SKINGEN, "data")
    if os.path.isfile(os.path.join(skin, "series60skin.skn")):
        out += ["--data", "private\\10207114\\import\\101f84b9\\series60skin.skn=" + rel(os.path.join(skin, "series60skin.skn"))]
        for ext in (".mif", ".mbm"):
            if os.path.isfile(os.path.join(skin, "series60skin" + ext)):
                out += ["--data", "resource\\skins\\101f84b9\\series60skin" + ext + "=" + rel(os.path.join(skin, "series60skin" + ext))]
    # extra complete 2010 themes (BUILD/skin-gen/<name>/data): Haze 2000B12D, Jing 2000B12E, listed by psln. THEMES=0 leaves
    # them out. (aknskins.iby leaves their .sel transition sets commented out, so they are not shipped.)
    if os.environ.get("THEMES", "1") != "0":
        for tn, uid in (("haze", "2000B12D"), ("jing", "2000B12E")):
            td = os.path.join(SKINGEN, tn, "data")
            if os.path.isfile(os.path.join(td, tn + ".skn")):
                out += ["--data", "private\\10207114\\import\\" + uid + "\\" + tn + ".skn=" + rel(os.path.join(td, tn + ".skn"))]
                for ext in (".mif", ".mbm"):
                    out += ["--data", "resource\\skins\\" + uid + "\\" + tn + ext + "=" + rel(os.path.join(td, tn + ext))]
    stubs = os.path.join(SYMBIAN, "symbian-2010", "oss.FCL.sf.adapt.stubs", "systemswstubs", "hwrmstubplugins", "data")
    for f in ("HWRMLightsPolicy.ini", "HWRMVibraPolicy.ini"):
        if os.path.isfile(os.path.join(stubs, f)):
            out += ["--data", "private\\101f7a02\\" + f + "=" + rel(os.path.join(stubs, f))]
    # x86: the drive map (C: = FAT32 via efat32.fsy; the board file explains). ARM boards stage their own (make_phorepoc_rom.sh).
    if not ARM:
        out += ["--data", "sys\\data\\estart.txt=" + rel(os.path.join(BOARD, "estart.txt"))]
    # eapol's configuration (accesssec bld.inf: eap_symbian.conf -> z:\private\101F8EC5\eap.conf, the WLAN server's
    # private dir). Without it every option takes its code default: EAPOL_key_state_allow_missing_PMKID_in_message_1
    # = false silently dropped a WPA2 message 1 without PMKID -> 4-Way Handshake timeout
    eapconf = os.path.join(SYMBIAN, "symbian-rcl3", "oss.FCL.sf.mw.accesssec", "eapol", "eapol_framework", "eapol_symbian",
                           "am", "core", "symbian", "file_config", "eap_symbian.conf")
    if os.path.isfile(eapconf):
        out += ["--data", "private\\101F8EC5\\eap.conf=" + rel(eapconf)]
    if os.path.isdir(ICONS):
        for f in sorted(os.listdir(ICONS)):
            if f.lower().endswith((".mif", ".mbm")):
                out += ["--data", "resource\\apps\\" + f + "=" + rel(os.path.join(ICONS, f))]
    # the board's own central-repository files (board/cenrep/<uid>.txt) replace the package's
    # export of the same repository; make_phorepoc_rom.sh then skips its generated copy too
    bcen = os.path.join(BOARD, "cenrep")
    board = {f.lower(): rel(os.path.join(bcen, f)) for f in (os.listdir(bcen) if os.path.isdir(bcen) else [])}
    if PHETTY != "exclude":
        board["e7015904.txt"] = phetty_cenrep()
    board["e7015a01.txt"] = devmenu_cenrep()
    board["e7015b01.txt"] = browser_cenrep()
    zman = os.path.join(ZGEN, "manifest.txt")
    zlines = [l.strip() for l in open(zman).read().splitlines() if l.strip()] if os.path.isfile(zman) else []
    zlines = [l for l in zlines if not (l.lower().startswith("private\\10202be9\\")
                                        and l.split("=", 1)[0][17:].lower() in board)]
    zlines += ["private\\10202be9\\" + f + "=" + p for f, p in sorted(board.items())]
    zlines = [s60_meshpreface(l) for l in zlines]
    # S^4: Orbit's Z: data (its central-repository files, i18n tables) and the other
    # S^4 packages': every BUILD/s4/*-zdata.txt
    for sman in sorted(glob.glob(os.path.join(S4B, "*-zdata.txt"))) if S4 else []:
        zlines += [l.strip() for l in open(sman).read().splitlines() if l.strip()]
    # SHELLS=s4|both: Z:\resource\phe\s4start.txt = what pheshellsel starts before hsapplicationlauncher. "wait <exe>" = start + wait for its
    # rendezvous (master's uiservicescmdlist: r_cmd_hbthemeserver and r_cmd_devicedialog, both ESsmWaitForSignal, prio 0xFF79); plain lines
    # = fire-and-forget: the servers our S^4 boots needed up before the homescreen asks (Qt service framework + publish/subscribe database
    # servers, the content arsenal, the software component registry, the system tone service).
    if S4 and SHELLS in ("s4", "both"):
        names = ["# the S^4-only servers (x32_rom_args.py)",
                 # the DRM Rights Server makes c:\system\temp\ at start-up: the Hb
                 # theme server's QtSingleApplication lock file lives in QDir::tempPath() -- without it, it thinks it already runs
                 # and exits 0. It never rendezvouses: start it, then wait for the directory
                 "z:\\sys\\bin\\rightsserver.exe", "waitdir c:\\system\\temp\\ 3000",
                 "wait z:\\sys\\bin\\hbthemeserver.exe", "wait z:\\sys\\bin\\hbdevicedialogappserver.exe"]
        names += ["z:\\sys\\bin\\" + n for n in ("qsfwdatabasemanagerserver.exe", "qpspathmapperserver.exe", "camenuserver.exe",
                                               "scrserver.exe", "softwaretransactionservices.exe", "stsserver.exe")]
        os.makedirs(ZGEN, exist_ok=True)
        s4list = os.path.join(ZGEN, "s4start.txt")
        with open(s4list, "w", newline="\n") as fh:
            fh.write("\n".join(names) + "\n")
        zlines += ["resource\\phe\\s4start.txt=" + rel(s4list)]
    out += [a for line in zlines for a in ("--data", line.partition("=")[0] + "=" + host_path(line.partition("=")[2]))]
    out += tail
    # one ROM path, one file: packages ship the same resource from more than one mmp (lch.rsc:
    # hlplch + hlpmodel, twice). The same source again is dropped, as a ROM builder merges it; two
    # DIFFERENT sources for one path is a real conflict and stops the build -- except in an S^4 ROM, where master's
    # SSM lists (BUILD/rsc-s4) replace the S^3 copy another component also ships (rfswpcmdlists.rsc:
    # rfstatusswppolicy's resources + the list set)
    s4_list = lambda p: "/rsc-s4/" in p.replace("\\", "/")
    seen, dedup, where = {}, [], {}
    for i in range(0, len(out), 2):
        flag, spec = out[i], out[i + 1]
        if flag == "--data":
            zpath, _, src = spec.partition("=")
            key = zpath.lower()
            if key in seen:
                if os.path.normpath(seen[key]) != os.path.normpath(src):
                    if s4_list(src) and not s4_list(seen[key]):
                        dedup[where[key] + 1] = spec
                        seen[key] = src
                        continue
                    if s4_list(seen[key]) and not s4_list(src):
                        continue
                    raise SystemExit(f"x32_rom_args: {zpath} from both {seen[key]} and {src}")
                continue
            seen[key] = src
            where[key] = len(dedup)
        dedup += [flag, spec]
    if _UNBUILT:
        print(f"# x32_rom_args: {len(_UNBUILT)} registered components not built (skipped): "
              + " ".join(sorted(_UNBUILT)[:40]) + (" ..." if len(_UNBUILT) > 40 else ""), file=sys.stderr)
    print("\n".join(dedup))


if __name__ == "__main__":
    main()
