# ph

The Phorepoc platform development kit: build Phorepoc from the Symbian source, and drive a test device through PhoneLab.

Generated from the command definitions (`ph help -markdown <dir>`); do not edit.

## Source

The Symbian source and the include tree every build compiles against.

| command | what it does |
|---|---|
| [`ph source fetch`](ph-source-fetch.md) | Fetch the pinned Symbian source |
| [`ph source export`](ph-source-export.md) | Build the include tree from the packages' exports |
| [`ph source casefix`](ph-source-casefix.md) | Link the header spellings a case-sensitive file system misses |
| [`ph source mbg`](ph-source-mbg.md) | Generate the icon id headers (.mbg) |
| [`ph source status`](ph-source-status.md) | Show where the source, the OS tree, the adaptations and the build output are |

## Components

The registry's components: list them, build them.

| command | what it does |
|---|---|
| [`ph component list`](ph-component-list.md) | List the registered components |
| [`ph component info`](ph-component-info.md) | Show a component's registry entry |
| [`ph component build`](ph-component-build.md) | Build components into images |
| [`ph component resources`](ph-component-resources.md) | Compile components' resources (.rsc) and their headers (.rsg) |
| [`ph component stale`](ph-component-stale.md) | List images linked before a provider of theirs existed |
| [`ph component check`](ph-component-check.md) | Check that the registry's paths exist |

## Images

What goes onto a device: the ROM, its data, installable packages.

| command | what it does |
|---|---|
| [`ph image rom`](ph-image-rom.md) | Build the ROM |
| [`ph image kernel`](ph-image-kernel.md) | Build the x86-64 kernel and its bootable ISO |
| [`ph image vbox`](ph-image-vbox.md) | Package the ISO as a VirtualBox appliance |
| [`ph image pinephone`](ph-image-pinephone.md) | Build the PinePhone image (kernel + ROM, for U-Boot) |
| [`ph image skin`](ph-image-skin.md) | Build the default skin package |
| [`ph image icons`](ph-image-icons.md) | Build every component's icon files (.mif) |
| [`ph image zdata`](ph-image-zdata.md) | Collect the Z: data the packages export |
| [`ph image sis`](ph-image-sis.md) | Package components as an installable SIS file |
| [`ph image compare`](ph-image-compare.md) | Byte-compare images with another build |

## Boards

The devices ph builds for, made of core plus adaptation repositories.

| command | what it does |
|---|---|
| [`ph board list`](ph-board-list.md) | List the boards ph builds for |
| [`ph board info`](ph-board-info.md) | Show a board's adaptation repositories |

## Packages

Installable items from the Phorepoc index and others: list, inspect, download.

| command | what it does |
|---|---|
| [`ph package list`](ph-package-list.md) | List the installable items of the indexes |
| [`ph package info`](ph-package-info.md) | Show an item: description, compatibility, approved releases |
| [`ph package fetch`](ph-package-fetch.md) | Download approved releases into the package cache |
| [`ph package install`](ph-package-install.md) | Install an item on a device through PhoneLab |

## Device

A device on the PhoneLab service: push images, read logs, take screenshots.

| command | what it does |
|---|---|
| [`ph device status`](ph-device-status.md) | Show the service's and the devices' health |
| [`ph device ops`](ph-device-ops.md) | List the device's operations |
| [`ph device op`](ph-device-op.md) | Run any device operation |
| [`ph device push`](ph-device-push.md) | Install an image and boot it |
| [`ph device input`](ph-device-input.md) | Press keys, type, tap and swipe on the device |
| [`ph device shot`](ph-device-shot.md) | Take a screenshot |
| [`ph device log`](ph-device-log.md) | Show stored log lines |
| [`ph device report`](ph-device-report.md) | Show how a boot went |
| [`ph device diff`](ph-device-diff.md) | Show the log lines one boot has and another has not |

## Other

Everything else.

| command | what it does |
|---|---|
| [`ph shell`](ph-shell.md) | Open a shell in the toolchain container |
| [`ph version`](ph-version.md) | Show ph's version |
| [`ph help`](ph-help.md) | Show the commands, a command's manual page, or a topic |

## Environment

ph finds its pieces through these variables ("ph source status" shows what they resolve to):

  PHOREPOC_CORE
      The core checkout. Default: the core folder next to the pdk.

  PHOREPOC_ADAPTATIONS
      The adaptation checkouts built with core, as a path list. Their os/ trees are laid over
      core's, their registries add their components. -b <board> sets it for one command.

  PHOREPOC_SYMBIAN
      The Symbian source: symbian-2010/ and symbian-rcl3/. Default: <core>/symbian.

  PHOREPOC_BUILD
      All generated files: a directory, or a Docker volume name. Default: <core>/build.

  PHOREPOC_OS_ROOT
      core's OS tree. Default: <core>/os.

  PHOREPOC_IMAGE
      The toolchain container image. Default: ghcr.io/phorepoc/base:latest.

  PHONELAB_URL, PHONELAB_TOKEN
      The PhoneLab service the device commands talk to, and its bearer token.

## Image-options

Given as -O name=value, as many as needed (ph image rom -b pc -O net=1 -O fonts=dejavu):

  net=0|1
      The networking stack: ESOCK, TCP/IP, DNS, DHCP, the e1000 driver and SNTP.

  net-test=<host> <port>
      The host the nettest component reaches at boot (RConnection, DNS, a TCP GET).

  ntp-server=<host>
      The SNTP server (default pool.ntp.org).

  fonts=ubuntu|dejavu|gdr
      The UI font set (default ubuntu).

  webfonts=0|1
      The web fonts (CJK subset); 0 leaves them out.

  themes=0|1
      The extra 2010 themes (Haze, Jing); 0 leaves them out.

  ui-renderer=classic|alfred|both
      The UI render path(s) (default: both on NGA boards, classic elsewhere).

  shells=s3|s4|both
      The UI shell(s): Symbian^3, the Symbian^4 home screen (Qt 4.8 + Hb), or both.

  nga=0|1
      The NGA window server (composition through OpenWF-C).

  nga-keep=<component>,...
      Keep these non-NGA twins in an NGA image (bisecting).

  alf=<stage>
      The Alfred (ALF) set at a bring-up stage.

  phetty=on|off|exclude
      The developer terminal daemon (phettyd, pheshell, pheterm).

  utc-offset=<minutes>
      The home time zone, minutes from UTC (180 = UTC+3).

  ssm-test=<component>
      Start this test component at boot (as ssmtest.exe).

  rom-tail=<component>,...
      Put these images last in the ROM (a delta push then sends only them).

  rom-exclude=<component>,...
      Leave these out of the ROM (they ship as SIS packages).

  rom-data=<dest>=<src> ...
      Extra Z: files, dest relative to Z:\.

  cenrep=<path>/<uid>.txt ...
      Central repository files that override the built ones.

  rom-heap-limits=0|1
      The Phorepoc heap and stack table; 0 = each image's own values.

  secondary=<image>
      The first user process the kernel starts (default efile.exe).

  s4=0|1
      Build the Symbian^4 components in their own tree.

  qt48=0|1
      Register Qt 4.8 as installed (its ROM stub).

  icons=0|1
      Generate every component's icon files before the ROM (default 1).

  iby-data=0|1
      The data files the packages' .iby files list (default 1).

  fast=0|1
      Keep the core images (euser, efsrv, hal, efile, estart) from the last build.

  prune=0|1
      Drop images whose imports are not in the ROM instead of failing.

  leave-trace=0|1
      Log each User::Leave with its caller (diagnostic).

  sched-trace=0|1
      The active scheduler with a dispatch log (diagnostic).

  trace=<hex>
      Ph image kernel: the kernel's trace mask (default 0x80000000: panics only).

  screen=<w>x<h>
      Ph image kernel: the screen the display HAL reports (default 360x640).

  scale=n
      Ph image kernel: show each screen pixel as an n x n block (default 2).

  dbg=0|1
      Ph image kernel: a debug kernel (default 1).

  kclean=0|1
      Ph image kernel: rebuild the kernel objects even when nothing changed.

  x32-ld=bfd
      Force GNU ld for the x32 links.

  x32-defs=<component>:<MACRO>,...
      Extra defines for some components (diagnostic).

  x32-include=<component>:<header>,...
      Force-include a header in some components (diagnostic).

  x32-fullerr=0|1
      The whole compiler output of the first failure.
