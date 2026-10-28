#!/usr/bin/env bash
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# make_phorepoc_rom.sh -- build the prelinked XIP ROM section that the x86-64 (x32) kernel carries as its ROM.
# Runs on Linux inside the toolchain container (ph image rom does that). Steps:
#
#   1. euser.dll   -- the real 2549-ordinal export table              (tools/emitter/build_euser_x32.py)
#   2. efsrv.dll   -- the F32 client library, 135 euser ordinals imported (build_efsrv_x32.py)
#   3. hal.dll     -- the user-side HAL client, 10 euser ordinals imported (build_hal_x32.py)
#                     2+3 are EFILE's dependencies.
#   4. efile.exe   -- the F32 file server: an exexp EXE importing across all three of them (build_efile_x32.py)
#   4b. estart.exe -- ESTART (e32strt.exe), which F32 starts once it is up (build_estart_x32.py)
#   5. the section -- all of them prelinked at 0x80400000, with real TRomEntry +
#                     TRomImageHeader records                        (tools/rombuild.py --section)
#
# Paths: PHOREPOC_OS_ROOT (board files, our components), PHOREPOC_SYMBIAN (the Symbian source),
# PHOREPOC_BUILD (all output; the ROM goes to $BUILD/rom). The kernel boot files the section is wrapped by (ekern
# link, board lists under tools/phorepoc-x86/board) live in OS_ROOT.
#
# SKIP_CENSUS=1 reuses the euser objects from a previous run (much faster).
# SECONDARY_IMAGE names the bundled image the kernel starts first (the ROM's secondary). Default efile.exe.
set -eu
PDK="$(cd "$(dirname "$0")/.." && pwd)"
TOOLS="$PDK/tools"
export PHOREPOC_CORE="${PHOREPOC_CORE:-$PDK/../core}"
export PHOREPOC_OS_ROOT="${PHOREPOC_OS_ROOT:-$PHOREPOC_CORE/os}"
export PHOREPOC_SYMBIAN="${PHOREPOC_SYMBIAN:-$PHOREPOC_CORE/symbian}"
export PHOREPOC_BUILD="${PHOREPOC_BUILD:-$PHOREPOC_CORE/build}"
OS_ROOT="$PHOREPOC_OS_ROOT"
# with adaptations (PHOREPOC_ADAPTATIONS) the OS tree is the merged view paths.py builds (core os/ + each adaptation os/)
[ -z "${PHOREPOC_ADAPTATIONS:-}" ] || OS_ROOT="$(python3 "$TOOLS/emitter/paths.py")"
SYMBIAN="$PHOREPOC_SYMBIAN"
BUILD="$PHOREPOC_BUILD"
SYM10="$SYMBIAN/symbian-2010"
PX86="$OS_ROOT/tools/phorepoc-x86"
# the board CommsDat source for CED (the x86 board brings its generator; a board that ships the CED-built database
# itself, os/data/commsdat/cccccc00.cre, needs none)
mk_ced_xml() { [ -f "$PX86/commdb_xml.py" ] && python3 "$PX86/commdb_xml.py" "$BUILD/rom/phorepoc_ced.xml" >/dev/null || true; }
mkdir -p "$BUILD/rom"

# UI_RENDERER=classic|alfred|both (default: both on NGA boards, classic elsewhere): which render path(s) the image carries.
# Alfred = the real alf set (NGA boards only: it composes through libWFC) = the ALF=s2 content + its wsini chain; "both" also keeps
# the Classic chain and lets the developer menu's CenRep key 4 pick at boot (wserv's CLASSIC:/ALFRED: wsini lines).
# A board without NGA has no Alfred: the request is reported and the image stays Classic.
UIR_DEFAULT=classic; [ "${NGA:-0}" = 1 ] && UIR_DEFAULT=both
UI_RENDERER="$(echo "${UI_RENDERER:-$UIR_DEFAULT}" | tr A-Z a-z)"
case "$UI_RENDERER" in classic|alfred|both) ;; *) echo "UI_RENDERER=$UI_RENDERER: use classic|alfred|both" >&2; exit 1 ;; esac
if [ "$UI_RENDERER" != classic ] && [ "${NGA:-0}" != 1 ]; then
  echo "note: UI_RENDERER=$UI_RENDERER needs the NGA window server (NGA=1): this image has no Alfred -> classic" >&2
  UI_RENDERER=classic
fi
# SHELLS=s3|s4|both (default s3): which UI shell(s) the image carries. s4 adds the Qt 4.8 + Hb + S^4 homescreen set;
# both lets the developer menu's CenRep key 1 pick at boot (src/pheshellsel), key 6 tells the menu which shells are present.
SHELLS="$(echo "${SHELLS:-s3}" | tr A-Z a-z)"
case "$SHELLS" in s3|s4|both) ;; *) echo "SHELLS=$SHELLS: use s3|s4|both" >&2; exit 1 ;; esac
export SHELLS
# SHELLS with s4: the S^4 component set (Qt 4.8 build, $BUILD/qt48/x32img) joins the S^3 ROM; the SSM lists stay S^3 (pheshellsel starts the S^4 side)
SHELL_S4SET=""
if [ "$SHELLS" != s3 ]; then export PHOREPOC_S4=1 PHOREPOC_QT48=1; SHELL_S4SET=1; fi
ALF_FROM_UIR=""
# s3 = the whole real alf set (transition effects, hint/CRP plug-ins, the app-framework proxy), as a device ships it
if [ "$UI_RENDERER" != classic ] && [ -z "${ALF:-}" ]; then ALF=s3; ALF_FROM_UIR=1; fi
export UI_RENDERER ALF_FROM_UIR
[ -z "${ALF:-}" ] || export ALF

# FAST=1: skip the six core images when the last ROM built them with the same
# LEAVE_TRACE -- ~90 s saved per ROM. Opt-in: a change to euser/F32/HAL/ESTART itself needs a normal build.
CORE_STAMP="$BUILD/rom/core.stamp"
# ARGS_ONLY=1: only the ROM CONTENTS (the BOOT= block below: images, data, boot config) -> the args
# file path on stdout, no core builds and no ROM -- used with ROM_ARCH=arm (x32_rom_args.py over the ARM images),
# so every board shares this one ROM recipe
if [ "${ARGS_ONLY:-0}" = 1 ]; then
  [ -n "${BOOT:-}" ] || { echo "ARGS_ONLY=1 needs BOOT=" >&2; exit 1; }
elif [ "${FAST:-0}" = 1 ] && [ "$(cat "$CORE_STAMP" 2>/dev/null)" = "LEAVE_TRACE=${LEAVE_TRACE:-0} SCHED_TRACE=${SCHED_TRACE:-0}" ] \
     && [ -f "$BUILD/x32core/euser_x32.dll" ] && [ -f "$BUILD/x32core/efile_x32.exe" ]; then
  echo "FAST=1: core images (euser efsrv hal efile estart) kept from the last build"
else
  rm -f "$CORE_STAMP"
  python3 "$TOOLS/emitter/build_euser_x32.py" | tail -3
  python3 "$TOOLS/emitter/build_efsrv_x32.py" | tail -3
  python3 "$TOOLS/emitter/build_hal_x32.py" | tail -3
  python3 "$TOOLS/emitter/build_efile_x32.py" | tail -4
  python3 "$TOOLS/emitter/build_estart_x32.py" | tail -2
  # the stamp only when every core image is there (a failed build must not be "kept" by the next FAST=1 ROM)
  for f in euser_x32.dll efsrv_x32.dll hal_x32.dll efile_x32.exe estart_x32.exe; do
    [ -f "$BUILD/x32core/$f" ] || { echo "core image $f was not built (see above)" >&2; exit 1; }
  done
  echo "LEAVE_TRACE=${LEAVE_TRACE:-0} SCHED_TRACE=${SCHED_TRACE:-0}" > "$CORE_STAMP"
fi
# The text shell stack, each from its real .mmp through the generic x32 builder: EWSRV + its console/key-translation/domain
# clients, EDISP (the x86 VGA-text flavour), the key map, ESHELL, and the FAT file system ELOCAL.FSY for C:.
#
# GUI=1: Symbian's "minigui" configuration instead (windowserver/minigui/*.oby): the GRAPHICAL non-NGA window server in
# the EWSRV.EXE slot (wserv.iby), the WSERV-window console econswserv.dll in the ECONS.DLL slot (econs_wserv.iby), fbserv +
# the gdi/bitgdi/fntstore/scdv stack, ECOM + WSERV's std plug-in and its registration, wsini.ini
# (`startup eshell`), and a bitmap font. All of it = the build_mmp_x32 closure of the roots.
X="$BUILD/x32img"
B32() { python3 "$TOOLS/emitter/build_mmp_x32.py" "$@"; }
SHELL_XIP=""
DATA=""
OPTIONAL=""								# closure images allowed to be missing (a component that did not build)
if [ -n "${BOOT:-}" ]; then
  # BOOT=: the FULL GUI ROM -- every x32-built registry component + its staged resources, skin, icons and Z: data
  # (tools/x32_rom_args.py), cenrep, the feature database, the language list -- started
  # by OUR src/bootui, which runs the BOOT list in order (waits for each server's rendezvous;
  # `!` = don't wait, `+capserver` = the real RAknUiServer::StartAknCapserverL, `=uid:uid:val` a P&S
  # set, `~cat:ms` a domain transition). BOOT=@ = the standard S60 list ending in the real Symbian^3
  # launcher (matrixmenu); BOOT=@<app> = the same list with another foreground app (@aknapp).
  # Build every component first: python3 tools/emitter/build_mmp_x32.py --all.
  G="$SYM10/oss.FCL.sf.os.graphics"
  # every S60 (BOOT=@) ROM carries CommsDat, as a phone's does (CTelephony reads the TSY name from
  # it -- the thumbnail server's IMEI query, so Clock -- and failed without NET=1); NET=1 adds the live stack
  COMMSDAT=0; case "$BOOT" in @*) COMMSDAT=1;; esac; [ "${NET:-0}" = 1 ] && COMMSDAT=1
  # BOOT=ssm: the REAL System State Manager replaces bootui: the S60 phone's SSM set
  # (x32_rom_args with PHOREPOC_SSM=1: sysstatemgr + policies + the S60 command lists + custom commands + the SAA
  # adaptation over Symbian's stub), ESTART from its real mmp (domain manager on), and a wsini with NO startup
  # line -- SSM starts WSERV itself (startup\0\wserv.rsc) and then the UI. Same Z: data as BOOT=@.
  SSMBOOT=0; [ "$BOOT" = ssm ] && { SSMBOOT=1; COMMSDAT=1; export PHOREPOC_SSM=1; ESTART_IMG="$BUILD/x32img/e32strt.exe"
                                    mk_ced_xml; }
  case "$BOOT" in @*)
    BOOT_APP="${BOOT#@}"
    # after apparc's first scan (it registers widgets with apparc): the WRT first-boot pre-installer (waited for -- it
    # installs z:\data\WidgetPreInstall\* once, then exits)
    POSTSCAN="widgetpreinstaller"
    # !rightsserver: the DRM rights server, started (not waited for: it never rendezvouses) as a phone's
    # first DRM client starts it. Its ConstructL is what creates C:\System\Temp\ on a phone -- the init RFS
    # script and SSM's InitRamDrive make \System\Temp on the RAM drive (D:) only; the WRT icon converter and many
    # S60 apps write to c:\system\temp\ and rely on it
    BOOT="=101f75b6:102047b7:0 =101f8766:42:100 =101f8766:41:109 =101f8766:43:101 =101f8766:44:101 =101f8766:46:104 sysagt2svr !domainsrv ~2:10000 baksrvs featmgrserver !rightsserver cdlserver ssmutilsrv tzserver +capserver +lights calensvr !${BOOT_APP:-matrixmenu}";;
  esac
  # NET=1: the comms stack. CED imports the board's CommsDat (tools/phorepoc-x86/commdb_xml.py,
  # the Ethernet config with this board's PDD + NAT addresses), c32start brings up the root
  # server + ESOCK, ntpsync sets the clock over SNTP, and nettest walks RConnection -> DNS -> a TCP GET (NET_TEST="<host> <port>").
  # All go in before the foreground app (the list's last item). Without NET=1 an S60 ROM runs only CED.
  if [ "$COMMSDAT" = 1 ] && [ "$SSMBOOT" = 0 ]; then
    mk_ced_xml
    NT="${NET_TEST:-}"
    # NET_DUMP=1: + ceddump, the resulting database as c:\cedout.cfg (read it off the DISK=1 image)
    DUMP=""; [ "${NET_DUMP:-0}" = 1 ] && DUMP=" ceddump"
    # ntpsync: SNTP against pool.ntp.org every 30 min (NTP_SERVER= another), sets the clock + RTC
    # cmsetup: what S60's dbcreator adds on top -- the S60 CommsDat tables + an "Internet" destination
    # (SNAP) holding the DHCP IAP; MPM/S60MCPR select from destinations (src/cmsetup)
    NETBOOT='ced|-i|z:\sys\data\phorepoc_ced.xml'"$DUMP"' cmsetup'
    [ "${NET:-0}" = 1 ] && NETBOOT="$NETBOOT"' c32start %PhoneTsy ntpsync'"${NTP_SERVER:+|$NTP_SERVER}"' !nettest'"${NT:+|${NT// /|}}"
    set -f
    ITEMS=($BOOT)
    set +f
    LAST="${ITEMS[${#ITEMS[@]}-1]}"
    # ^ = bootui waits for apparc's first app-list scan before the foreground app
    BOOT="${ITEMS[*]:0:${#ITEMS[@]}-1} $NETBOOT ^ ${POSTSCAN:-} $LAST"
  fi
  # UTC_OFFSET=<minutes> (e.g. 180 = UTC+3): bootui sets it first (home time = UTC + offset)
  BOOT="${UTC_OFFSET:+@$UTC_OFFSET }$BOOT"
  printf '%s\n' $BOOT > "$BUILD/rom/bootui.txt"
  # S60's fader (avkon AknExtendedFader): the dark dim behind popups / the task switcher.
  # The [EXTENDEDFADER] section must stay last (every later line would belong to it).
  # ABSOLUTEFADING: a window's fade is then a flag, not WSERV's default COUNT (CWsClientWindow::iFadeCount). S60 fades through
  # RWsSession::SetSystemFaded from two servers (aknnfysrv's notifier dialogs, akncapserver) and no one unfades
  # symmetrically (CAknNotifierAppServerAppUi::SetFadedL(EFalse) is a no-op unless the keyguard is on): with the count,
  # the second fade left every window at count 1 after the single unfade = the whole UI stayed dimmed after the
  # Bluetooth pairing dialogs.
  FADER_INI=("ABSOLUTEFADING" "PLUGINS FLICKERBUFFER STD EXTENDEDFADER" "FADER AknExtendedFader" "[EXTENDEDFADER]" "ID 0x2001B299")
  # NGA=1: the NGA window server composes through OpenWF-C -- its standard plug-in (10286378)
  # offers the "flickerbuffer" + "display" render stages (display = libWFC -> the display channel); same S60 fader
  [ "${NGA:-0}" = 1 ] && [[ ",${NGA_KEEP:-}," != *,wserv,* ]] && FADER_INI=("ABSOLUTEFADING" "PLUGINS FLICKERBUFFER DISPLAY EXTENDEDFADER" "RENDERSTAGES flickerbuffer display"
                                    "FADER AknExtendedFader" "[EXTENDEDFADER]" "ID 0x2001B299")
  CLASSIC_INI=("${FADER_INI[@]}")
  # ALF=... (off by default): the S^3 render-stage chain is
  #   PLUGINS sssrenderstage alf compositor platformcompositor / [alf] ID 0x200184B6 / [compositor] ID 0x2002BCF9.
  # Ours: alf (NO flickerbuffer on top: it needs MWsGraphicsContext + MWsDrawableSourceProvider from the NEXT stage, CFbRenderStage::ConstructL,
  # and fails to build above alf -> EWsPanicNoRenderStagePipeline 112) -> alf (alfrenderstage: streams the draw commands to the alf server thread it starts
  # inside WSERV) -> compositor (alfopenwfrs 0x2002BCFA = the open twin of the closed alfcompositorrs; libWFC underneath).
  # ALF=s1b|s2|s3 (1 = s2, 2 = s3; s1 = s2): there is NO client-only stage -- the real clients CONNECT to the alf server;
  # s1b = s2 (a compositor-only chain draws nothing: openwfcrs has no MWsGraphicsContext, the alf stage supplies it);
  # s2 = the real clients + hitchcock + the alf stage (chain: alf compositor) + the VG renderer + alfopenwfrs; s3 = + the rest (ROM args only).
  # The [alf]/[compositor] sections go before [EXTENDEDFADER], which must stay last.
  case "$(echo "${ALF:-}" | tr A-Z a-z)" in
    1|2|s1|s1b|s2|s3) ALFST="alf compositor"; ALFPL="ALF COMPOSITOR"
               ALFSEC=("[alf]" "ID 0x200184B6" "[compositor]" "ID 0x2002BCFA") ;;
    *) ALFST="" ;;
  esac
  [ "${NGA:-0}" = 1 ] && [ -n "$ALFST" ] && FADER_INI=("ABSOLUTEFADING" "PLUGINS $ALFPL EXTENDEDFADER"
                                    "RENDERSTAGES $ALFST" "FADER AknExtendedFader" "${ALFSEC[@]}" "[EXTENDEDFADER]" "ID 0x2001B299")
  # UI_RENDERER=both: the two chains in ONE wsini: the PLUGINS / RENDERSTAGES lines of each get a CLASSIC: / ALFRED: prefix that wserv's
  # INIFILE shim (toolchain/shims/wserv_board/inifile_board.cpp) resolves from CenRep e7015a01 key 4; every other line is shared.
  # UIR_TEST=1 (mechanism test, no NGA): the same two prefixes around two harmless marker lines instead of the chains.
  if [ "$UI_RENDERER" = both ] || [ "${UIR_TEST:-0}" = 1 ]; then
    UIR_A=(); UIR_C=(); UIR_N=()
    if [ "${UIR_TEST:-0}" = 1 ]; then UIR_C=("CLASSIC: PHUIRTEST 0"); UIR_A=("ALFRED: PHUIRTEST 1"); fi
    for l in "${CLASSIC_INI[@]}"; do
      [ "${UIR_TEST:-0}" = 1 ] && break
      case "$l" in "PLUGINS "*|"RENDERSTAGES "*) UIR_C+=("CLASSIC: $l");; esac
    done
    for l in "${FADER_INI[@]}"; do
      if [ "${UIR_TEST:-0}" != 1 ]; then case "$l" in "PLUGINS "*|"RENDERSTAGES "*) UIR_A+=("ALFRED: $l"); continue;; esac; fi
      UIR_N+=("$l")
    done
    FADER_INI=("${UIR_C[@]}" "${UIR_A[@]}" "${UIR_N[@]}")
  fi
  # ROM_ARCH=arm: TWO screen modes -- 1 = portrait 360x640 (the panel's own orientation), 2 = landscape 640x360
  # rotated 90 -- and the two Avkon hardware states the orientation action plug-in switches between by sending
  # akncapserver EKeyDeviceB (portrait) / EKeyDeviceA (landscape); the model is S60's wsini_tube_extendedfader.ini
  # (AknExtendedFader/rom), which has the nHD panel landscape-native, hence its 270/0
  [ "${ROM_ARCH:-}" = arm ] && FADER_INI=("SCR_WIDTH1 360" "SCR_HEIGHT1 640" "SCR_ROTATION1 0" "SCR_TWIP_WIDTH1 2235" "SCR_TWIP_HEIGHT1 3973"
                                    "S60_SCR_STYLE_NAME1 QHDTOUCH" "SCR_WIDTH2 640" "SCR_HEIGHT2 360" "SCR_ROTATION2 90" "SCR_TWIP_WIDTH2 3973"
                                    "SCR_TWIP_HEIGHT2 2235" "S60_SCR_SOFTKEY_LOCATION2 right" "S60_SCR_STYLE_NAME2 QHDTOUCH"
                                    "S60_HWSTATE_KEYCODE0 EKeyDeviceB" "S60_HWSTATE_SCREENMODE0 1" "S60_HWSTATE_ALT_SCREENMODE0 2"
                                    "S60_HWSTATE_KEYCODE1 EKeyDeviceA" "S60_HWSTATE_SCREENMODE1 2" "S60_HWSTATE_ALT_SCREENMODE1 1" "${FADER_INI[@]}")
  if [ "$SSMBOOT" = 1 ]; then python3 "$TOOLS/mkwsini.py" "$BUILD/rom/wsini_app.ini" - "${FADER_INI[@]}"
  else python3 "$TOOLS/mkwsini.py" "$BUILD/rom/wsini_app.ini" bootui "${FADER_INI[@]}"; fi
  python3 "$TOOLS/features_dat.py" "$BUILD/rom/features.dat" >/dev/null
  # the language list: UTF-16LE with a BOM, "01,d" (English, default)
  printf '\377\3760\0001\000,\000d\000\r\000\n\000' > "$BUILD/rom/languages.txt"
  # one software version per ROM build: apparc drops its C: app-list cache when it changes
  python3 "$([ -f "$PX86/sw_version.py" ] && echo "$PX86" || echo "$TOOLS")/sw_version.py" "$BUILD/rom/sw.txt" >/dev/null
  ARGS="$BUILD/rom/full.args"
  # every registered component's icon files (bld.inf mifconv) before the recipe stages BUILD/icon-gen (MIF_ALL=0 = off):
  # missing .mif files crash apps that load their icons at start
  [ "${MIF_ALL:-1}" = 0 ] || python3 "$TOOLS/mif_all.py" | grep -v "^  " || true
  python3 "$TOOLS/x32_rom_args.py" > "$ARGS"
  CGEN="$BUILD/cenrep-gen"
  # the base set once (.base = complete; a folder holding only the media apps used to skip it: no 102858f2)
  [ -f "$CGEN/.base" ] || { python3 "$TOOLS/crml2cenrep.py" "$CGEN" >/dev/null && touch "$CGEN/.base"; }
  # the media apps (Voice Recorder, FM Radio, Video Player, Photos): their crml/confml repositories (RCL_3 app packages are not in the default set)
  python3 "$TOOLS/crml2cenrep.py" "$CGEN" ../symbian-rcl3/oss.FCL.sf.app.voicerec ../symbian-rcl3/oss.FCL.sf.app.radio oss.FCL.sf.app.videoplayer ../symbian-rcl3/oss.FCL.sf.app.photos >/dev/null
  declare -A CENREP=()
  for f in "$CGEN"/*.txt; do u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; done
  while IFS= read -r f; do
    case "$f" in ''|'#'*) continue ;; esac
    u=${f##*/}; u=${u%.txt}; case "$f" in ../../*) p="$OS_ROOT/${f#../../}" ;; *) p="$SYM10/$f" ;; esac; CENREP[${u,,}]="$p"   # ../../ = ours, in OS_ROOT
  done < <(tr -d '\r' < "$OS_ROOT/tools/kernel/cenrep_files.txt")
  # the board's own repositories (whole files)
  if [ "${ROM_ARCH:-}" = arm ]; then for f in "$OS_ROOT"/src/phorepoc-arm/board/cenrep/*.txt; do [ -f "$f" ] && { u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; }; done; fi
  # CENREP_EXTRA="<path>/<uid>.txt ..." = per-run overrides, read last -- e.g. a wallpaper test
  for f in ${CENREP_EXTRA:-}; do u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; done
  # LeaveAppsInMemory (AvkonInternalCRKeys.h KCRUidLeaveAppsInMemory 102858F2, CAknAppUi::ExitHidesInBackground FindEqL on the app
  # UID): SSM starts Messaging, Contacts, Logs, Clock and Calendar in the background; unlisted, each exits on its first "hide"
  # (mce: HideOrExit at EMsvServerReady -> KLeaveExit) -- the S60 device default lists them
  python3 "$TOOLS/cenrep_set.py" "${CENREP[102858f2]}" "$BUILD/rom/102858f2.txt" 0x1=$((0x100058C5)) 0x2=$((0x101F4CCE)) 0x3=$((0x101F4CD5)) \
    0x4=$((0x10005903)) 0x5=$((0x10005901)) && CENREP[102858f2]="$BUILD/rom/102858f2.txt"
  # transition effects ON with alf (themes KThemesTransitionEffects 0x9: the release default KMaxTInt = off; 0 = all
  # effect groups on): the skin server then registers our z:esource\effects set (core os/data/effects)
  [ -z "${ALF:-}" ] || { python3 "$TOOLS/cenrep_set.py" "${CENREP[102818e8]}" "$BUILD/rom/102818e8.txt" 0x9=0 && CENREP[102818e8]="$BUILD/rom/102818e8.txt"; }
  # ALF test images (ALF=...): keep the display on while we work on speed (frames take seconds): SysAp's display lights timeout
  # (hwrmlightdomaincrkeys.h KDisplayLightsTimeout, repository 10200C8C key 6, 15 s) and the screen saver period
  # (SettingsInternalCRKeys.h KSettingsScreenSaverPeriod, 101F877C key 8, 15 s) -> 3600 s (SysAp clamps the first to its maximum)
  if [ -n "${ALF:-}" ] && [ -z "${ALF_FROM_UIR:-}" ] && [ "${ALF_KEEP_DISPLAY:-1}" = 1 ]; then
    python3 "$TOOLS/cenrep_set.py" "${CENREP[10200c8c]}" "$BUILD/rom/10200c8c.txt" 0x6=3600 && CENREP[10200c8c]="$BUILD/rom/10200c8c.txt"
    python3 "$TOOLS/cenrep_set.py" "${CENREP[101f877c]}" "$BUILD/rom/101f877c.txt" 0x8=3600 && CENREP[101f877c]="$BUILD/rom/101f877c.txt"
  fi
  # PH_PORTRAIT_INPUT=qwerty: the stock portrait QWERTY (peninputgenericvkb 0x20026837) is the default portrait on-screen
  # keyboard instead of ITU-T: AknFep's KAknFepLastUsedPortraitInput (101F876D key 0x26) = EPluginInputModePortraitFSQ (128)
  # (default 8 = ITU-T). Needs the feature 0x7FFF0001 (tools/features_dat.py, on by default).
  if [ "${PH_PORTRAIT_INPUT:-}" = qwerty ]; then
    python3 -c "import sys;s=open(sys.argv[1],encoding='utf-16').read();o='0x26 int 8 0';assert o in s;open(sys.argv[2],'w',encoding='utf-16',newline='').write(s.replace(o,'0x26 int 128 0'))" "${CENREP[101f876d]}" "$BUILD/rom/101f876d.txt"
    CENREP[101f876d]="$BUILD/rom/101f876d.txt"
  fi
  {
    printf -- '--data\nSystem\\Data\\wsini.ini=%s\n' "$BUILD/rom/wsini_app.ini"
    printf -- '--data\nSys\\Data\\bootui.txt=%s\n' "$BUILD/rom/bootui.txt"
    # fonts: the S60 system faces made from Ubuntu (Dalton Maag's face, Ubuntu Font Licence; third_party/fonts/ubuntu in
    # OS_ROOT, + Ubuntu Mono for the Monospace alias), by tools/font_gen.py: "Series 60 Sans" Regular + SemiBold (bold) and
    # "Series 60 Sans TitleSmBd", with the S60 faces' vertical extent -- the names FontProvider asks for, so weight and size
    # come out right; the plain Ubuntu files are NOT staged (nearest-match would pick them). FONTS=dejavu = the
    # release's own reference TrueType set (the files the Symbian Foundation's "Sirocco" ROM carried,
    # referencefonts/group/reffonts_for_sirocco.iby), FONTS=gdr = the uibench test bitmap font (ceurope.gdr).
    if [ "${FONTS:-ubuntu}" = ubuntu ]; then
      [ -f "$BUILD/font-gen/s60sans_r.ttf" ] || python3 "$TOOLS/font_gen.py" >/dev/null
      for ttf in "$BUILD"/font-gen/s60sans_*.ttf "$OS_ROOT"/third_party/fonts/ubuntu/UbuntuMono-*.ttf; do
        printf -- '--data\nResource\\Fonts\\%s=%s\n' "$(basename "$ttf")" "$ttf"
      done
    elif [ "${FONTS:-ubuntu}" = gdr ]; then
      printf -- '--data\nResource\\Fonts\\ceurope.gdr=%s\n' "$G/graphicstest/uibench/data/CEUROPE.GDR"
    else
      RF="$SYM10/oss.FCL.sf.os.textandloc/fontservices/referencefonts"
      for ttf in $(grep '^data=' $RF/group/reffonts_for_sirocco.iby | grep -o '[A-Za-z0-9-]*\.ttf' | sort -u); do
        printf -- '--data\nResource\\Fonts\\%s=%s\n' "$ttf" "$RF/truetype/$ttf"
      done
    fi
    # web-script fallback faces for the BrowserNG engine (Skia's glyph fallback by coverage; tools/font_web_gen.py, Noto, OFL):
    # Arabic, Hebrew, Thai, Devanagari + a CJK subset (~3.3 MB in all), in the engine-private Resource\phweb\fonts (Skia scans it
    # after Resource\Fonts). NOT under Resource\Fonts, not even a subdirectory: with them there the boot stalled
    # after cryptospisetup (SSM). WEBFONTS=0 leaves them out.
    if [ "${WEBFONTS:-1}" = 1 ] && [ -f "$OS_ROOT/tools/font_web_gen.py" ]; then
      [ -f "$BUILD/font-gen/web/NotoSansCJKsc-Subset.otf" ] || python3 "$OS_ROOT/tools/font_web_gen.py" >/dev/null
      for ttf in "$BUILD"/font-gen/web/*.ttf "$BUILD"/font-gen/web/*.otf; do
        [ -f "$ttf" ] || continue
        printf -- '--data\nResource\\phweb\\fonts\\%s=%s\n' "$(basename "$ttf")" "$ttf"
      done
    fi
    printf -- '--data\nprivate\\10205054\\features.dat=%s\n' "$BUILD/rom/features.dat"
    printf -- '--data\nresource\\bootdata\\languages.txt=%s\n' "$BUILD/rom/languages.txt"
    grep -qiF 'resource\versions\sw.txt=' "$ARGS" || \
      printf -- '--data\nresource\\versions\\sw.txt=%s\n' "$BUILD/rom/sw.txt"
    if [ "$COMMSDAT" = 1 ]; then
      [ -f "$BUILD/rom/phorepoc_ced.xml" ] && printf -- '--data\nSys\\Data\\phorepoc_ced.xml=%s\n' "$BUILD/rom/phorepoc_ced.xml"
      # CommsDat's repository: the default database commsdat.iby ships (CED replaces its content)
      CDB="$SYM10/oss.FCL.sf.os.commsfw/commsfwtools/preparedefaultcommsdatabase/defaultcommdb/bin/cccccc00.cre"
      # BOOT=ssm runs no CED: the ROM carries CED's OUTPUT, as a phone ROM does (the database a BOOT=@ NET=1
      # boot built from phorepoc_ced.xml + cmsetup, taken off its disk)
      if [ "$SSMBOOT" = 1 ]; then
        if [ -f "$BUILD/commsdat/cccccc00.cre" ]; then CDB="$BUILD/commsdat/cccccc00.cre"
        elif [ -f "$OS_ROOT/data/commsdat/cccccc00.cre" ]; then CDB="$OS_ROOT/data/commsdat/cccccc00.cre"   # the board's own
        else echo "BOOT=ssm: no CED-built CommsDat -- ESOCK/connmon will fail (bash $PX86/make_commsdat.sh)" >&2; fi
      fi
      printf -- '--data\nprivate\\10202be9\\cccccc00.cre=%s\n' "$CDB"
    fi
    # (a package's own bld.inf export in BUILD/zdata-gen wins over the generated copy)
    BS='\'
    ARGS_SO_FAR="$(<"$ARGS")"		# one read, bash substring tests below (a grep per uid is slow)
    for u in "${!CENREP[@]}"; do
      [[ "$ARGS_SO_FAR" == *"private${BS}10202be9${BS}$u.txt="* ]] && continue
      printf -- '--data\nprivate\\10202be9\\%s.txt=%s\n' "$u" "${CENREP[$u]}"
    done
    # ROM_DATA_EXTRA="dest=src ..." : extra Z: files, dest relative to Z:\ (e.g. 'hello.js=/path/hello.js' for jsc)
    for kv in ${ROM_DATA_EXTRA:-}; do printf -- '--data\n%s\n' "$kv"; done
    # platform version marker: VersionInfo::GetVersion (platformver.dll) reads the version from the NAME of
    # z:\system\install\Symbianv<major>.<minor>.sis; with none it returns KErrNotSupported (-5) and apps that check it
    # exit with that. 5.4 = the S60 platform version this ROM identifies as.
    python3 -c "import sys; sys.path.insert(0, sys.argv[2]); from sis import build_rom_stub; open(sys.argv[1],'wb').write(build_rom_stub(pkg_uid=0x101F7961, vendor='Symbian', name='Symbian', version=(5,4,0)))" "$BUILD/rom/Symbianv5.4.sis" "$TOOLS/emitter" || exit 1
    printf -- '--data\nsystem\\install\\Symbianv5.4.sis=%s\n' "$BUILD/rom/Symbianv5.4.sis"
    # PHOREPOC_QT48=1: Qt's ROM stub (qt.iby: System\Install\qt_stub.sis) -- registers package 0x2001E61C at 4.8.7 so
    # Qt apps' SIS dependency "Qt >= 4.7.x" is met
    if [ "${PHOREPOC_QT48:-}" = 1 ]; then
      python3 "$OS_ROOT/tools/phorepoc-qt48/mk_qt_stubs.py" "$BUILD/rom" >&2 || exit 1
      printf -- '--data\nsystem\\install\\qt_stub.sis=%s\n' "$BUILD/rom/qt_stub.sis"
    fi
  } >> "$ARGS"
  # the source's .iby recipe: the plain data files (DBMS policies, backup registrations, .conf/.xml/.rul...) of every
  # component above, found in its own package (tools/iby_data_args.py). IBY_DATA=0 = off.
  if [ "${IBY_DATA:-1}" != 0 ]; then python3 "$TOOLS/iby_data_args.py" "$ARGS" > "$ARGS.iby" && cat "$ARGS.iby" >> "$ARGS"; fi
  echo "full GUI ROM: $(grep -c '^--xip' "$ARGS") images, $(grep -c '^--data' "$ARGS") data files; bootui: $BOOT"
  if [ "${ARGS_ONLY:-0}" = 1 ]; then echo "$ARGS"; exit 0; fi
  ROMS=""
  DATA="@$ARGS"
elif [ "${GUI:-0}" = 1 ]; then
  ROOTS="wserv econswserv fbserv ecomserver wsstdplugin ekdata eshell elocal"
  G="$SYM10/oss.FCL.sf.os.graphics"
  WSINI=$G/windowing/windowserver/minigui/group/wsini_minigui-stripped.ini
  # APP=<component>: WSERV starts that app instead of ESHELL. Its whole closure is built and carried; closure
  # members that fail to build are warned about and left out (an app's LIBRARY list is wider than what it loads on its path).
  if [ -n "${APP:-}" ] && [ "$APP" != eshell ]; then
    ROOTS="$ROOTS $APP ${APP_EXTRA:-}"
    OPTIONAL=1
    python3 "$TOOLS/mkwsini.py" "$BUILD/rom/wsini_app.ini" "$APP"
    WSINI="$BUILD/rom/wsini_app.ini"
  fi
  B32 --closure $ROOTS | grep -E "^===|wrote|FAIL|stub:|^  !" | head -200
  ROMS="$(B32 --list-closure-roms $ROOTS | tr -d '\r')"
  DATA="--data System\\Data\\wsini.ini=$WSINI
        --data Resource\\Fonts\\ceurope.gdr=$G/graphicstest/uibench/data/CEUROPE.GDR"
  # the files the resource step staged per image -- compiled resources (BUILD/romfiles/<image>.romfiles, host paths
  # relative to BUILD) and ECOM registrations (BUILD/plugins/<stem>.rsc -> Resource\Plugins): resources are CPU-independent data
  for f in $ROMS; do
    [ -f "$BUILD/romfiles/$f.romfiles" ] && for l in $(tr -d '\r' < "$BUILD/romfiles/$f.romfiles"); do DATA="$DATA --data ${l%%=*}=$BUILD/${l#*=}"; done
    [ -f "$BUILD/plugins/${f%.*}.rsc" ] && DATA="$DATA --data Resource\\Plugins\\${f%.*}.rsc=$BUILD/plugins/${f%.*}.rsc"
  done
  if [ -n "$OPTIONAL" ]; then
    # central repository initialisation files, Z:\private\10202be9\<uid>.txt (tools/crml2cenrep.py's output,
    # overridden by OS_ROOT/tools/kernel/cenrep_files.txt)
    CGEN="$BUILD/cenrep-gen"
    # the base set once (.base = complete; a folder holding only the media apps used to skip it: no 102858f2)
    [ -f "$CGEN/.base" ] || { python3 "$TOOLS/crml2cenrep.py" "$CGEN" >/dev/null && touch "$CGEN/.base"; }
    declare -A CENREP=()
    for f in "$CGEN"/*.txt; do u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; done
    while IFS= read -r f; do
      case "$f" in ''|'#'*) continue ;; esac
      u=${f##*/}; u=${u%.txt}; case "$f" in ../../*) p="$OS_ROOT/${f#../../}" ;; *) p="$SYM10/$f" ;; esac; CENREP[${u,,}]="$p"   # ../../ = ours, in OS_ROOT
    done < <(tr -d '\r' < "$OS_ROOT/tools/kernel/cenrep_files.txt")
    if [ "${ROM_ARCH:-}" = arm ]; then for f in "$OS_ROOT"/src/phorepoc-arm/board/cenrep/*.txt; do [ -f "$f" ] && { u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; }; done; fi
    for f in ${CENREP_EXTRA:-}; do u=${f##*/}; u=${u%.txt}; CENREP[${u,,}]="$f"; done
    # LeaveAppsInMemory: see the BOOT= block
    python3 "$TOOLS/cenrep_set.py" "${CENREP[102858f2]}" "$BUILD/rom/102858f2.txt" 0x1=$((0x100058C5)) 0x2=$((0x101F4CCE)) 0x3=$((0x101F4CD5)) \
      0x4=$((0x10005903)) 0x5=$((0x10005901)) && CENREP[102858f2]="$BUILD/rom/102858f2.txt"
  # transition effects ON with alf (themes KThemesTransitionEffects 0x9: the release default KMaxTInt = off; 0 = all
  # effect groups on): the skin server then registers our z:esource\effects set (core os/data/effects)
  [ -z "${ALF:-}" ] || { python3 "$TOOLS/cenrep_set.py" "${CENREP[102818e8]}" "$BUILD/rom/102818e8.txt" 0x9=0 && CENREP[102818e8]="$BUILD/rom/102818e8.txt"; }
    # ALF test images (ALF=...): keep the display on (see the BOOT= block)
    if [ -n "${ALF:-}" ] && [ -z "${ALF_FROM_UIR:-}" ] && [ "${ALF_KEEP_DISPLAY:-1}" = 1 ]; then
      python3 "$TOOLS/cenrep_set.py" "${CENREP[10200c8c]}" "$BUILD/rom/10200c8c.txt" 0x6=3600 && CENREP[10200c8c]="$BUILD/rom/10200c8c.txt"
      python3 "$TOOLS/cenrep_set.py" "${CENREP[101f877c]}" "$BUILD/rom/101f877c.txt" 0x8=3600 && CENREP[101f877c]="$BUILD/rom/101f877c.txt"
    fi
    if [ "${PH_PORTRAIT_INPUT:-}" = qwerty ]; then
      python3 -c "import sys;s=open(sys.argv[1],encoding='utf-16').read();o='0x26 int 8 0';assert o in s;open(sys.argv[2],'w',encoding='utf-16',newline='').write(s.replace(o,'0x26 int 128 0'))" "${CENREP[101f876d]}" "$BUILD/rom/101f876d.txt"
      CENREP[101f876d]="$BUILD/rom/101f876d.txt"
    fi
    for u in "${!CENREP[@]}"; do DATA="$DATA --data private\\10202be9\\$u.txt=${CENREP[$u]}"; done
  fi
elif [ -n "${TESTEXE:-}" ]; then
  # TESTEXE=<component>: the text-shell ROM with that exe (and its closure: Open C libs...) as the ROM SECONDARY (SECONDARY_IMAGE=<exe>.exe) --
  # no file server, no shell: a small ROM for a console test, no S60 stack to rebuild.
  ROOTS="econs ektran domaincli ewsrv edisp ekdata elocal $TESTEXE"
  B32 --closure $ROOTS | grep -E "^===|wrote|FAIL|stub:" | head -200
  ROMS="$(B32 --list-closure-roms $ROOTS | tr -d '\r')"

  OPTIONAL=1
else
  ROOTS="econs ektran domaincli ewsrv edisp ekdata eshell elocal"
  B32 $ROOTS | grep -E "^===|wrote|FAIL|stub:" | head -60
  ROMS="econs.dll ektran.dll domaincli.dll ewsrv.exe edisp.dll ekdata.dll eshell.exe elocal.fsy"
fi
for f in $ROMS; do
  if [ ! -f "$X/$f" ]; then
    [ -n "$OPTIONAL" ] && { echo "  (not built, left out of the ROM: $f)"; continue; }
    echo "missing $X/$f"; exit 1
  fi
  case "$f" in                              # the minigui slot renames
    wserv.exe) z=ewsrv.exe ;;
    econswserv.dll) z=econs.dll ;;
    *) z=$f ;;
  esac
  SHELL_XIP="$SHELL_XIP --xip Sys\\Bin\\$z=$X/$f"
done
# which ROM this is: phorepoc (the S^3 set, with or without the S^4 shell) or s4 (the S^4 set alone)
mkdir -p "$BUILD/rom"
if [ "${PHOREPOC_S4:-}" = 1 ] && [ -z "$SHELL_S4SET" ]; then echo s4; else echo phorepoc; fi > "$BUILD/rom/flavour.txt"
UHELLO=()
[ -f "$BUILD/uexe/uhello.imp.exe" ] && UHELLO=(--xip 'Sys\Bin\uhello.exe='"$BUILD/uexe/uhello.imp.exe")
python3 "$TOOLS/rombuild.py" --section 0x80400000 $SHELL_XIP $DATA \
   --xip 'Sys\Bin\euser.dll='"$BUILD/x32core/euser_x32.dll" \
   --xip 'Sys\Bin\efsrv.dll='"$BUILD/x32core/efsrv_x32.dll" \
   --xip 'Sys\Bin\hal.dll='"$BUILD/x32core/hal_x32.dll" \
   --xip 'Sys\Bin\efile.exe='"$BUILD/x32core/efile_x32.exe" \
   --xip 'Sys\Bin\estart.exe='"${ESTART_IMG:-$BUILD/x32core/estart_x32.exe}" \
   "${UHELLO[@]}" \
   --builtin-device 'Sys\Bin\enet.ldd=ldd:1' --builtin-device 'Sys\Bin\ethernet.pdd=pdd:2' \
   --builtin-device 'Sys\Bin\esoundsc.ldd=ldd:3' --builtin-device 'Sys\Bin\soundsc.pdd=pdd:4' \
   --builtin-device 'Sys\Bin\ecamerasc.ldd=ldd:5' --builtin-device 'Sys\Bin\camerasc.pdd=pdd:6' \
   --secondary-name "${SECONDARY_IMAGE:-efile.exe}" \
   "$BUILD/rom/romsect.bin"
