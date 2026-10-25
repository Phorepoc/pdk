# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

"""zdata_gen -- ROM data files straight from the packages' bld.inf PRJ_EXPORTS.

A bld.inf export whose destination is on the Z: drive (`z:/private/...`, or the equivalent
`/epoc32/data/z/...`) is ROM *data*: suite XML, layout templates, central-repository
initialisation files, the MCS menu content... A real image build picks them up from
/epoc32/data/z via the .iby files; here the bld.inf is the single source of truth.

    python3 tools/zdata_gen.py            # -> BUILD/zdata-gen/manifest.txt  (`<rom path>=<host path>` lines)

x32_rom_args.py / gui_rom_args.py add the manifest to the ROM. Host paths point INTO the source tree
(nothing is copied). Packages = PACKAGES below (paths relative to SYMBIAN/symbian-2010).
Later packages / later lines win on a duplicate ROM path (bld.inf variants export alternatives
to the same destination under #ifdef -- the preprocessor has already picked one).
"""
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.path.insert(0, os.path.join(HERE, "emitter"))
from paths import BUILD, resolve  # noqa: E402
import epoc32_export as EX  # noqa: E402

# the real launcher (matrixmenu, RCL_3) and its data source (Menu Content Service)
PACKAGES = [
    "../symbian-rcl3/oss.FCL.sf.app.homescreen/menufw",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/menucontentsrv",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/homescreensrv_plat/sapi_menucontent",
    # real S60 applications: DBMS policy files etc.
    "../symbian-rcl3/oss.FCL.sf.app.organizer/notepad",
    # the time-zone server's zone database (tzdb.dbz) + its DBMS policy
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/tzservices",
    # ... replaced by S60's: S60 Clock's own tzdb.dbz, whose zone ids match the S60
    # timezones.rsc Clock ships (London = 2800; Symbian's tzloc/tzdb say 2592). Later wins.
    "../symbian-rcl3/oss.FCL.sf.app.organizer/clock2/clockui/uilayer/group",
    # the comms stack's configuration: c32start.ini, the root server's .cmi files (which
    # CPMs = threads to load), ESOCK's .esk protocol lists + tcpip.ini, the Ethernet NIF's ini
    "oss.FCL.sf.os.commsfw/commsprocess/commsrootserverconfig/etc",
    "oss.FCL.sf.os.commsfw/datacommsserver/esockserver/etc",
    "oss.FCL.sf.os.commsfw/serialserver/c32serialserverconfig",
    "oss.FCL.sf.os.networkingsrv/networkprotocols/tcpipv4v6prt/data",
    "oss.FCL.sf.os.networkingsrv/networkcontrol/ipnetworklayer/addressinfohook",
    "oss.FCL.sf.os.networkingsrv/networkprotocols/ipeventnotifier",   # ip.ipeventnotifier.esk (the DHCP unicast translator)
    "oss.FCL.sf.os.networkingsrv/linklayerprotocols/ethernetnif",
    # CED's mesh preface (z:\system\data\meshpreface1.cfg): the tier / MCPR / CPR records CommsDat
    # needs (this ESOCK finds its providers through them, not through .esk files)
    "oss.FCL.sf.os.commsfw/commsfwtools/preparedefaultcommsdatabase/Tools/ced/group",
    # the crypto SPI's plug-in list (z:\resource\cryptospi\plug-ins.txt): cryptospisetup publishes the
    # plug-in characteristics from it; without it every SPI user (the DRM rights server) fails
    "oss.FCL.sf.os.security/crypto/weakcryptospi/group",
    # ETel as a comms root-server CPM: etel.cmi
    "oss.FCL.sf.os.cellularsrv/telephonyserver/etelserverandcore/data",
    # the SMS/WAP protocols' ESKs for ESOCK's SmsWap worker: without a protocol behind it, the
    # ESock_SmsWap <-> ESock_Main bind never completed and sat out the root server's 60 s job timeout
    "oss.FCL.sf.os.cellularsrv/smsprotocols/smsstack/smsprot/Group",
    "oss.FCL.sf.os.cellularsrv/smsprotocols/smsstack/wapprot/Group",
    # the DOS server's DSY choice (z:/private/101f6efa/dosserver.ini -> CommonDSY.dll); exported
    # to the ARM target's release tree, /epoc32/release/armv5/urel/z/, as a device ROM build takes it
    "oss.FCL.sf.os.devicesrv/dosservices/commondsy",
    # the S^3 widget homescreen: the 360x640 touch configuration ONLY (idlehomescreen/data/group
    # includes vga_tch too, exporting to the same Z: paths), and the homescreen services' own data
    "../symbian-rcl3/oss.FCL.sf.app.homescreen/idlehomescreen/data/qhd_tch",
    "../symbian-rcl3/oss.FCL.sf.app.homescreen/idlehomescreen/widgetmanager",
    "../symbian-rcl3/oss.FCL.sf.app.homescreen/idlehomescreen/xmluirendering",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/homescreenpluginsrv",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/idlefw",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/contentpublishingsrv",
    "../symbian-rcl3/oss.FCL.sf.mw.homescreensrv/contentcontrolsrv",
    # the S60 browser: the favourites DBMS policy (101FD685.SPD), browser/engine data
    "../symbian-rcl3/oss.FCL.sf.mw.web",
    "../symbian-rcl3/oss.FCL.sf.app.webuis",
    # software install: swipolicy.ini (the production policy: self-signed allowed, user capabilities
    # grantable) + the installer UI's data
    "../symbian-rcl3/oss.FCL.sf.mw.appinstall/installationservices/swiconfig",
    "../symbian-rcl3/oss.FCL.sf.mw.appinstall/appinstaller",
    # the SWI root store z:\resource\swicertstore.dat (the release's: Symbian's ACS roots) -- without it
    # CSWICertStore::NewL left -1 and every install stopped there
    "oss.FCL.sf.os.security/cryptoservices/rootcertificates",
    # the Bluetooth stack: the
    # ESock_Bt worker's .cmi (the _399 one: our variant defines SYMBIAN_NON_SEAMLESS_NETWORK_BEARER_MOBILITY) + the
    # bt.bt_v2.esk protocol list, bluetooth_stack.ini (the CoreHCI plug-in), cmdq.ini (the QDP plug-in), the BT
    # registry's DBMS policy. The plug-in choice (corehci_symbian.ini) and the UART (hctl_h5.ini) are OURS: GENERATED
    "oss.FCL.sf.os.bt/bluetoothmgmt/btconfig",
    "oss.FCL.sf.os.bt/bluetooth/btstack",
    "oss.FCL.sf.os.bt/bthci/bthci2/hcicmdq",
    "oss.FCL.sf.os.bt/bluetoothmgmt/btmgr",
    # the S60 Bluetooth engine's data (RCL_3): btrfs.txt, backup_registration.xml, the 10204DA9 settings XML
    "../symbian-rcl3/oss.FCL.sf.mw.btservices/bluetoothengine/bteng",
    # the Context Framework: private\10282BC4\ backup registration, the autorotateui_*.rul
    # rules (ApplicationStateSourcePlugIn exports its .xml as .rul) and each context-source plug-in's Settings\<uid>\*.xml
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfw",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/ApplicationStateSourcePlugIn",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/PSStateSourcePlugIn",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/cenrepsourceplugin",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/displaysourceplugin",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/sensorsourceplugin",
    "../symbian-rcl3/oss.FCL.sf.mw.appsupport/contextframework/cfwplugins/orientationactionplugin",
]
# generated headers the menufw .iby files ship as ROM *data* (menusuites.iby): the hierarchy navigator
# resolves `foldersuite:R_...` localisation keys by parsing the .rsg AT RUNTIME.  (rom path, repo path)
GENERATED = [
    ("resource/apps/foldersuite.rsg", ".epoc32/include/foldersuite.rsg"),
    # ... and icon keys `matrixmenudata:qgn_menu_games` by parsing the .mbg (python3 tools/mbg.py
    # ../symbian-rcl3/oss.FCL.sf.app.homescreen; the .mif files themselves: python3 tools/mif_gen.py --vector
    # baseline/symbian-rcl3/oss.FCL.sf.app.homescreen/menufw/menusuites/foldersuite/group)
    ("resource/apps/gridroot.mbg", ".epoc32/include/gridroot.mbg"),
    ("resource/apps/matrixmenudata.mbg", ".epoc32/include/matrixmenudata.mbg"),
    # the SWI policy: exported by the release only for non-S60 builds -> a board file
    ("system/data/swipolicy.ini", "tools/phorepoc-x86/board/swi/swipolicy.ini"),
    # the organizer's alarm tones, decoded to WAV on the build host under their release names
    # (python3 tools/transcode_tones.py)
    ("data/sounds/digital/clock.aac", ".zdata-gen/tones/clock.aac"),
    ("data/sounds/digital/alarm.aac", ".zdata-gen/tones/alarm.aac"),
    # the ARM board's Bluetooth HCI plug-ins (OURS hctl_h5 + the stock initialisor for now) and its UART1 port
    ("private/101f7989/bluetooth/corehci_symbian.ini", "src/phorepoc-arm/board/bt/corehci_symbian.ini"),
    ("private/101f7989/bluetooth/hctl_h5.ini", "src/phorepoc-arm/board/bt/hctl_h5.ini"),
    # R3: the Realtek initialisor's options and the RTL8723CS firmware (linux-firmware's, unmodified, with Realtek's
    # notice -- the licence allows binary redistribution so; never into deliverables/)
    ("private/101f7989/bluetooth/initialisor_rtl.ini", "src/phorepoc-arm/board/bt/initialisor_rtl.ini"),
    # the ARM board's auto-rotate activation rule: the stock rule minus its device-lock condition -- no
    # lockapp in the ROM yet, so the lock status context stays "uninitialised" (see the file's header)
    ("private/10282bc4/rules/autorotateui_activate.rul", "src/phorepoc-arm/board/cf/autorotateui_activate.rul"),
    ("private/101f7989/bluetooth/rtl8723cs_xx_fw.bin", "src/phorepoc-arm/board/bt/fw/rtl8723cs_xx_fw.bin"),
    ("private/101f7989/bluetooth/rtl8723cs_xx_config.bin", "src/phorepoc-arm/board/bt/fw/rtl8723cs_xx_config.bin"),
    ("private/101f7989/bluetooth/LICENCE.rtlwifi_firmware.txt", "src/phorepoc-arm/board/bt/fw/LICENCE.rtlwifi_firmware.txt"),
    # WiFi (the chip core): the RTL8703B 8051 firmware (linux-firmware's, unmodified, same licence); the kernel reads
    # it out of the XIP ROM (a64wlan.cpp) until the OURS wlanhwinit loads it from user side
    ("resource/wlan/rtw8703b_fw.bin", "src/phorepoc-arm/board/wlan/fw/rtw8703b_fw.bin"),
    ("resource/wlan/rtw8703b_wow_fw.bin", "src/phorepoc-arm/board/wlan/fw/rtw8703b_wow_fw.bin"),
    ("resource/wlan/LICENCE.rtlwifi_firmware.txt", "src/phorepoc-arm/board/wlan/fw/LICENCE.rtlwifi_firmware.txt"),
]
# never from here: SIS stubs (no software installer on the board)
SKIP = ("system/install/",)


def zdata(bldinf):
    """yield (src_abs, rom_path) for the Z:-drive exports of one bld.inf."""
    cur_dir, section = os.path.dirname(bldinf), ""
    for ln in EX.preprocessed_lines(bldinf):
        m = re.match(r'^#\s*\d+\s+"([^"]+)"', ln)
        if m:
            f = m.group(1).replace("\\\\", "\\")
            if not f.startswith("<"):
                cur_dir = os.path.dirname(os.path.abspath(f))
            continue
        m = EX.SECTION.match(ln)
        if m:
            section = m.group(1).upper()
            continue
        if section != "EXPORTS":
            continue
        toks = [t.strip('"') for t in re.findall(r'"[^"]*"|\S+', ln)]
        if len(toks) < 2 or toks[0].startswith(":"):
            continue
        dest = toks[1].replace("\\", "/")
        m = re.match(r"^(?:z:/|/?epoc32/data/z/|/?epoc32/release/armv5/urel/z/)(.+)$", dest, re.I)
        if not m:
            continue
        rom = m.group(1)
        src = os.path.normpath(os.path.join(cur_dir, toks[0].replace("\\", "/")))
        if rom.endswith("/"):
            rom += os.path.basename(src)
        if rom.lower().startswith(SKIP):
            continue
        yield src, rom


def main() -> int:
    out, missing = {}, 0
    for pkg in PACKAGES:
        root = os.path.normpath(os.path.join(EX.BASE, pkg))
        for dp, dns, fns in os.walk(root):
            dns.sort(key=str.lower); fns.sort(key=str.lower)   # NTFS order on every file system (see epoc32_export.py)
            dns[:] = [d for d in dns if d.lower() not in ("tsrc", "test", "internal", "help")]
            for fn in fns:
                if fn.lower() != "bld.inf":
                    continue
                for src, rom in zdata(os.path.join(dp, fn)):
                    if not os.path.isfile(src):
                        missing += 1
                        continue
                    out[rom.lower()] = (rom, src)
    for rom, rel in GENERATED:
        if os.path.isfile(resolve(rel)):
            out[rom.lower()] = (rom, resolve(rel))
        else:
            missing += 1
    gen = os.path.join(BUILD, "zdata-gen")
    os.makedirs(gen, exist_ok=True)
    with open(os.path.join(gen, "manifest.txt"), "w", newline="\n") as fh:
        for rom, src in sorted(out.values()):
            fh.write(rom.replace("/", "\\") + "=" + os.path.abspath(src).replace("\\", "/") + "\n")
    print(f"zdata: {len(out)} ROM data files ({missing} missing-src) -> {os.path.join(gen, 'manifest.txt')}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
