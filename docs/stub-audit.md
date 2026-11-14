# Stub audit

**No stub has a place in an image.** A stub is anything that stands in for a piece of the system without doing what
that piece does: a jump that panics the caller, a null component, a release "stub" library, a source file left out of
a build. This page is the inventory, the rule for replacing each kind, and the CI check that keeps new ones out.

## The kinds

| # | Kind | Where it shows | Check |
|---|------|----------------|-------|
| 1 | **Link stub** (consumer side): an image calls a name no provider exports; the emitter links it to `__unimpl_export`, which panics the caller | `<image>.linkstubs`, one record per image | `tools/stub_audit.py check` (CI, **fails the build** on a new one) |
| 2 | **Missing provider**: a library the mmp names that is not built or registered (its imports become kind 1) | the build log, `! LIBRARY x.lib: no provider registered` | shows as kind 1 |
| 3 | **Unimplemented export** (provider side): an ordinal of the provider's `.def` served by the stub. `ABSENT` in the `.def` = retired in the release the `.def` comes from (fine); anything else is a **GAP** | `<image>.unimpl` | `tools/unimpl_exports.py` (report) |
| 4 | **Named stand-ins**: a whole component replaced by a null provider or a release stub library | the registry (`*_null`, `x32_mmp`, `nosrc`), the ROM list | listed below |
| 5 | **Dropped sources**: `drop=[...]` in a registry entry leaves source files of the real mmp out of the build | the registry | listed below |

The exception-handling runtime names a C++ image references for unwinding (`__cxa_begin_cleanup`, `__cxa_type_match`,
`__exidx_start` ...) are kind 1 as well; they are counted apart (`#eh allowed` in the allowlist) because nothing
unwinds through them.

## The rule per item

* real source in the 2010 / RCL_3 release -> build it;
* never released -> write the real implementation (ours, AGPL-3.0-only);
* hardware absent on a board -> a real adaptation that reports "not present" the way the real phone does when the part
  is missing (derived from the reference device's behaviour), never a stub that pretends.

## The ratchet

`tools/stub_audit.py check --arch x32|arm` runs at the end of every core build (`--core`) and every board build, after
the image recipe (`.github/actions/board-finish`). It fails when

* an (image, name) link stub is not in `tools/stub-allow/<arch>.allow` -- a **new stub**;
* an allowlist line is no longer needed -- delete it (**the list only shrinks**; board builds only);
* an allowlist line names no reason in `tools/stub-allow/reasons.txt` (each reason carries its tracking line, below);
* an image has no `.linkstubs` record (every image the emitter writes has one; the core libraries and the kernel-side
  images built by their own scripts write theirs too; the kernel links with unresolved names as errors, so it has no
  stub mechanism to record);
* an exception-runtime name appears and the architecture has no `#eh allowed` line (x32 has none: it has no such stub).

`python3 tools/stub_audit.py allow --arch x32` prints the allowlist for what is built now; `report` lists every stub
per image. `python3 tools/unimpl_exports.py --arch x32 --names` lists the GAP ordinals (kind 3).

## Reasons (tracked)

Each remaining allowlist line has one of these; the number is in `tools/stub-allow/reasons.txt`. They are the work list.

### Numbers (the published core layers, re-measured on this tree)

Link stubs, excluding the exception-runtime names, over every image the component build writes:

| | x32 before | x32 now | arm before | arm now |
|---|---|---|---|---|
| images with a record | 1627 | 1659 | 1696 | 1729 (+ the board's own) |
| link-stub call sites | 1078 in 968 images | **52 in 5 images** | 2150 in 1033 images | **58 in 5 images** |
| exception-runtime names (kind 1, counted apart) | 0 | 0 | 5049 | 5074 (allowed) |

What went: `__stack_chk_fail` (963 x32 images), `abort`, `__gnu_Unwind_Find_exidx` (~1000 arm images) and `_impure_ptr`
(18) now resolve to real definitions in the image (`rt_names.cpp`, `xu_rtnames.cpp`: see "Runtime names" below); the 25
audio message-handler libraries, `redircli`, `drmrightsinfo` and its implementation, `3gpextparser`,
`mobilitypolicyprovider` and `ostuser` (its GET_PC intrinsic is RVCT-only: a shim header) are built from their sources, the hotspot framework's `hssscanlist` static library is built (HssScan*, RHssInterface), `tneengine` is built (the XML header's include guard hid its base class), and `imut` is built whole (its registry `drop` of 11 source files is gone: 220 of 235 ordinals real, was 41); the WBXML dictionaries (`xmlinterface`, `provisioningparser`) are
exported as C objects; `snprintf` is exported by JavaScriptCore with C linkage; `nan` (WebKit's trio) has a definition;
the va_list-by-reference export of `RFileLogger::WriteFormat` matches on x32; the null `rcse`'s `.def` spelt
`RPointerArray` with the wrong length (`R12`), so the one lookup its clients call was served by the stub.

### Runtime names

The GCC archives an image links (libsupc++, libgcc) reference a few names no system library exports. The emitter now
links the real definition when no provider has the name, and keeps a provider's when there is one:

* `abort`: `User::Panic` (the process-ending call libstdc++'s terminate path makes);
* `__gnu_Unwind_Find_exidx`: the exception-index lookup over the image's own `.ARM.exidx`, from the kernel's
  descriptor of the code segment (the lookup `cxx_eh` images already carried);
* `_impure_ptr`: it is read by libsupc++'s verbose terminate handler (`vterminate.o`), which prints through newlib's
  `stderr`, a structure this system's libc does not have; the member is left out and `__verbose_terminate_handler`
  ends the thread with `User::Panic` (before: it read the stub's code as a `FILE`);
* `__stack_chk_fail` (x32): `User::Panic`; `nan` (x32): a quiet NaN.

The arm exception-runtime names (`__cxa_begin_cleanup`, `__cxa_type_match`, `__exidx_start` ...) are still stubs in
every C++ image that is not an exception-handling image: the real system imports them from `drtaeabi`. Making every
image import it changes the import table of ~1000 images, which wants a phone run first -- `#eh allowed` in
`stub-allow/arm.allow`, to be lowered to 0 by that change.

### Kind 3 (unimplemented exports), classified against the `.def`

`python3 tools/unimpl_exports.py --arch arm|x32 [--names] [--rom ARGS]`. Of the ordinals the `.def`s name and an image
serves with the stub: **ABSENT** (retired in the release the `.def` comes from: fine) 7542 (arm) / 7535 (x32);
typeinfo of a component built without RTTI (its importers synthesize the typeinfo) counted apart; **GAP** 8385 ordinals
in 27 images (arm) / 8312 in 36 (x32). Most are four stand-ins: `hbcore` 5697 (null), `alfclient` 1294, `phgl` 263,
`aknskinsrv.exe` 191 / 199 -- then `irobex` 163, `imut` 157 (the dropped sources), `libstdcpp` 156-219,
`alfdecoderserverclient` 76, `dossrv` 63, `dsclient` 39, `goommonitor*` 20/18, `commondsy` 15, `avkon` 7, and a tail of
1-6 per image (`esocksvr`, `netmeta`, `nifman`, `javascriptcore`, `osncore` ...). `--names` lists them per image.

### Kind 4: named stand-ins (status)

Already the real thing or our implementation: `frametable` (`src/frametable`), `startupadaptationstub` (our
`StartupAdaptationStub.cpp`: the board's power and reset), `hwrmstubplugins` (our light and vibra plug-ins),
`messagehandlerfactory` and `custominterfaceproxyfactory` (the reference factories are real code; the message handlers
they link were the missing part, now built), `wifiprotclient` / `wifiprotplugin` (the real client and plug-in),
`gfxtransadapter`, `alfclient`/`alfdecoderserverclient`/`goommonitor` (`*_real` on the GPU boards),
`surfaceupdateclient`; `sgresource` is the real one on every board since the x86 kernel carries the sgdevice LDD.

Still stand-ins, with the way out: `hbcore` (null; the real Orbit core is registered in `components_s4.py` for the Qt
configuration), `commondsy` (null CDSY controller), `rcse` (null VoIP profile registry; the real engine is RCL_3
`sf.app.iptelephony/richcallsettingsengine/rcse2` and builds 56/56 here -- it needs `sipprofilecli`, its two CenRep
files in the ROM and the settings database: R10), `mcssathandler` (source left out), `c32` (`StartC32` from
`c32_null.cpp`), `power_save_display_mode` (release stub), `licenseetsy` / `simatktsy` (fake modem: a board with a
modem needs the real TSY), `wapisecuritysettingsui` (stub UI), `iaupdateapi` (kept by decision: the update service is
decided after going public).

### Kind 5: dropped sources (checked)

The registry's `drop=[...]` leaves source files of the real mmp out of a build: 87 components, 3 `nosrc` components
(`surfaceupdateclient`, `alfclient`, `alfdecoderserverclient`) and 1 `x32_mmp` substitution (`drmhelper`).
Every (component, source) now has a line in `tools/stub-allow/dropped.allow` with a reason of `dropped-reasons.txt`, and
`stub_audit.py check` fails on a drop with no line (a new one) and on a line nothing drops any more (the list only
shrinks). `stub_audit.py dropped` prints what the registry drops now. The reasons come from a measurement, not a guess:
for each dropped file the replacement was compared with the release's file (share of the release's lines present in the
replacement, or an `#include` of the release's file):

| reason | meaning | lines |
|---|---|---|
| D1 | a patched copy of the release's file (port, board adaptation, fix, trace) or a wrapper that includes it, >= 75% the release's lines: the release's code runs | 141 |
| D2 | the release's own alternative (CMLA stub, RealAudio-off variant: the real source was never released) | 3 |
| D3, D4 | the sisx recogniser's ROM patch constant; ARM `.cia` assembler replaced by C++ on x32 | 3 |
| D16, D18 | our implementation of what the release stubs (frame table, HWRM plug-ins, start-up adaptation); rasteriser tuning | 7 |
| D19 | the release's generated sources (charconv plug-ins: cnvtool over the release's data) | 43 |
| D10 - D15 | the work list: ALF/graphics null clients (4), C32 null on ARM (12 files), the SAT menu handler (1), the licensee TSY mock (1), the power-save and SVG-T parts never released (2), the emergency-call stem (1) | 21 |

The list per component is in `stub-audit-replaced-sources.md`.

### The allowlist's reasons (the work list)

* **R1** M3G engine (`m3gcore`, 38 names, `hitchcock`): in no release; write it.
* **R3** JRT registry client (`Java::CJavaRegistry*`, `mcsmenu`, `mcsmenuhandler`): `javaregistryclient` is not in the released sources.
* **R4** BrowserNG's own start-up (`browserng.exe`): the stock browser is not shipped.
* **R8** WLAN hardware init (`CWlanHwInit`, `wlmserversrv`): by design the board supplies it -- the board with a WLAN chip answers its data and MAC address; the board without one answers no data and KErrNotFound. Only a core layer built alone shows the stub, hence the `board` flag.
* **R10** (stand-in, no link stub) `rcse`, see above.

## Plans (assessed 2026-11-04, nothing started)

* **hbcore (kind 4 and 3, 5697 GAP ordinals).** The real Orbit source is in the 2010 release (`oss.FCL.sf.mw.hb`: hbcore 349 .cpp / ~178k
  lines, hbwidgets 115 / 80k, hbinput, hbutils, hbfeedback, hbplugins, hbservers). It needs Qt (`core gui svg network xml`, OpenVG);
  the registry already has the real build (`components_s4.py`, `PHOREPOC_S4=1`, Qt 4.7 or Qt 4.8 with `PHOREPOC_QT48=1`) and the frozen
  `.def` is wired into the null. 51 mmps import it (SysAp, loghandling, hsappkeyplugin, phone client/server, startup, appinstall UIs).
  Plan: build the Qt 4.8 + Orbit chain, replace the null mmp with the real one, re-run the audit. Not small: Qt 4.8 in the ROM, the
  theme server and resources, and the 51 importers then run real Orbit code (days).
* **R1 M3G (`hitchcock`, 38 names).** No `m3g*.h` or engine source in any release tree; only the Java glue (`javauis/m3g_qt`). Callers:
  hitchcock's `huim3gmesh.cpp`, `alfm3gmesh.cpp`, mesh visuals and the VG10 surface setup; nothing else uses it and the shipped apps
  load no meshes. API used: loader (`m3gCreateLoader`, `m3gDecodeData`), reference counting, scene graph (World/Group/Camera/Mesh/
  SkinnedMesh/MorphingMesh), Appearance/CompositingMode/PolygonMode, Background, transforms, animation. Plan: write `m3gcore` (ours,
  AGPL-3.0-only): object model + `.m3g` decoder + transforms + animation (~2k lines for the 38 names), rendering through the existing VG
  surface later.
* **R3 JRT registry client.** The real client is in the release (`oss.FCL.sf.app.JRT/javamanager/javaregistry/client`, 9 files, 2337 lines,
  API headers in `jrt_plat/java_registry_api`); the tree's `java_stubs` copy only leaves `KErrNotSupported`. It needs `javastorage`,
  `javafileutils`, `javasecurity`, `javasizehelperclient`, all in the tree, built from hand-written mmps (the release's are qmake `.pro`).
  About 5 small components, ~a day; a phone with no Java apps answers an empty registry (verify in `javaregistry.cpp`).
* **Kind 5 work list (D10-D16):** retire the null ALF / surfaceupdateclient variants (sgresource: done) (make the `*_real` / `*_nga` the
  only ones; touches the ROM-args scripts of every board); real C32 on ARM (D11, wants a phone run); the SAT menu handler (D12) needs the
  SAT server running without a SIM, which is the same real "no modem" adaptation as D13 (licensee TSY / simatktsy).
* **R10 rcse:** the real engine is in `sf.app.iptelephony/richcallsettingsengine/rcse2`; still held (SIP framework, CenRep, boot test).
