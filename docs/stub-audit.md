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
`__exidx_start` ...) are kind 1 as well; they are counted apart (`#ceiling eh N` in the allowlist) because nothing
unwinds through them.

## The rule per item

* real source in the 2010 / RCL_3 release -> build it;
* never released -> write the real implementation (ours, Apache-2.0);
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
* the exception-runtime count goes over (or, on a board, under) its ceiling.

`python3 tools/stub_audit.py allow --arch x32` prints the allowlist for what is built now; `report` lists every stub
per image. `python3 tools/unimpl_exports.py --arch x32 --names` lists the GAP ordinals (kind 3).

## Reasons (tracked)

Each remaining allowlist line has one of these; the number is in `tools/stub-allow/reasons.txt`. They are the work list.

### Numbers (the published core layers, re-measured on this tree)

Link stubs, excluding the exception-runtime names, over every image the component build writes:

| | x32 (PC) before | x32 now | arm (PinePhone) before | arm now |
|---|---|---|---|---|
| images with a record | 1627 | 1659 | 1696 | 1729 (+ the board's own) |
| link-stub call sites | 1078 in 968 images | **53 in 6 images** | 2150 in 1033 images | **59 in 6 images** |
| exception-runtime names (kind 1, counted apart) | 0 | 0 | 5049 | 5069 (ceiling) |

What went: `__stack_chk_fail` (963 x32 images), `abort`, `__gnu_Unwind_Find_exidx` (~1000 arm images) and `_impure_ptr`
(18) now resolve to real definitions in the image (`rt_names.cpp`, `xu_rtnames.cpp`: see "Runtime names" below); the 25
audio message-handler libraries, `redircli`, `drmrightsinfo` and its implementation, `3gpextparser`,
`mobilitypolicyprovider` and `ostuser` (its GET_PC intrinsic is RVCT-only: a shim header) are built from their sources, the hotspot framework's `hssscanlist` static library is built (HssScan*, RHssInterface), and `imut` is built whole (its registry `drop` of 11 source files is gone: 220 of 235 ordinals real, was 41); the WBXML dictionaries (`xmlinterface`, `provisioningparser`) are
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
image import it changes the import table of ~1000 images, which wants a phone run first -- ceiling `eh` in
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
`gfxtransadapter`, `alfclient`/`alfdecoderserverclient`/`goommonitor` (`*_real` on the GPU boards), `sgresource`,
`surfaceupdateclient`.

Still stand-ins, with the way out: `hbcore` (null; the real Orbit core is registered in `components_s4.py` for the Qt
configuration), `commondsy` (null CDSY controller), `rcse` (null VoIP profile registry; the real engine is RCL_3
`sf.app.iptelephony/richcallsettingsengine/rcse2` and builds 56/56 here -- it needs `sipprofilecli`, its two CenRep
files in the ROM and the settings database: R10), `mcssathandler` (source left out), `c32` (`StartC32` from
`c32_null.cpp`), `power_save_display_mode` (release stub), `licenseetsy` / `simatktsy` (fake modem: a board with a
modem needs the real TSY), `wapisecuritysettingsui` (stub UI), `iaupdateapi` (kept by decision: the update service is
decided after going public).

### Kind 5: dropped sources

The registry's `drop=[...]` leaves source files of the real mmp out of a build: 89 components (`cone`, `avkon`,
`eikcoctl`, `apparc`, `wserv`, `libc`, `msgs`, `imut`, `javascriptcore` ... ), 4 `nosrc` components (`sgresource`,
`surfaceupdateclient`, `alfclient`, `alfdecoderserverclient`) and 1 `x32_mmp` substitution (`drmhelper`). Many drops are
replaced by a board source (`extra=`); the rest are gaps. They are not yet covered by a check; the next step is a per-drop
reason table like this one.

### The allowlist's reasons (the work list)

* **R1** M3G engine (`m3gcore`, 38 names, `hitchcock`): in no release; write it.
* **R3** JRT registry client (`Java::CJavaRegistry*`, `mcsmenu`, `mcsmenuhandler`): `javaregistryclient` is not in the released sources.
* **R4** BrowserNG's own start-up (`browserng.exe`): the stock browser is not shipped.
* **R6** Thumbnail engine (`CTNEVideoClipInfo`): `tneengine` does not compile yet (the MP4 parser header).
* **R8** WLAN hardware init (`CWlanHwInit`, `wlmserversrv`): by design the board supplies it -- adaptation-pinephone (the chip), adaptation-pc (no chip: no data, KErrNotFound for the MAC address). Only a core layer built alone shows the stub, hence the `board` flag.
* **R9** `eikcoctl`: one `CTextListItemDrawer` method the Avkon lift does not carry.
* **R10** (stand-in, no link stub) `rcse`, see above.
