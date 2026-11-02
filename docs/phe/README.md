# ph

The Phorepoc platform development kit: build Phorepoc from the Symbian source, and drive a test device through PhoneLab.

Generated from the command definitions (`phe help -markdown <dir>`); do not edit.

## Source

The Symbian source and the include tree every build compiles against.

| command | what it does |
|---|---|
| [`phe source fetch`](phe-source-fetch.md) | Fetch the pinned Symbian source |
| [`phe source export`](phe-source-export.md) | Build the include tree from the packages' exports |
| [`phe source casefix`](phe-source-casefix.md) | Link the header spellings a case-sensitive file system misses |
| [`phe source mbg`](phe-source-mbg.md) | Generate the icon id headers (.mbg) |
| [`phe source status`](phe-source-status.md) | Show where the source, the OS tree, the adaptations and the build output are |

## Components

The registry's components: list them, build them.

| command | what it does |
|---|---|
| [`phe component list`](phe-component-list.md) | List the registered components |
| [`phe component info`](phe-component-info.md) | Show a component's registry entry |
| [`phe component build`](phe-component-build.md) | Build components into images |
| [`phe component resources`](phe-component-resources.md) | Compile components' resources (.rsc) and their headers (.rsg) |
| [`phe component stale`](phe-component-stale.md) | List images linked before a provider of theirs existed |
| [`phe component check`](phe-component-check.md) | Check that the registry's paths exist |

## Images

What goes onto a device: the ROM, its data, installable packages.

| command | what it does |
|---|---|
| [`phe image rom`](phe-image-rom.md) | Build the ROM |
| [`phe image recipe`](phe-image-recipe.md) | Run an image recipe of the board's repositories |
| [`phe image release`](phe-image-release.md) | Make the board's release files |
| [`phe image skin`](phe-image-skin.md) | Build the skin packages (the default skin and the Haze and Jing themes) |
| [`phe image icons`](phe-image-icons.md) | Build every component's icon files (.mif) |
| [`phe image zdata`](phe-image-zdata.md) | Collect the Z: data the packages export |
| [`phe image sis`](phe-image-sis.md) | Package components as an installable SIS file |
| [`phe image compare`](phe-image-compare.md) | Byte-compare images with another build |

## Boards

The devices phe builds for, made of core plus adaptation repositories.

| command | what it does |
|---|---|
| [`phe board list`](phe-board-list.md) | List the boards phe builds for |
| [`phe board info`](phe-board-info.md) | Show a board's adaptation repositories |

## Packages

Installable items from the Phorepoc index and others: list, inspect, download.

| command | what it does |
|---|---|
| [`phe package list`](phe-package-list.md) | List the installable items of the indexes |
| [`phe package info`](phe-package-info.md) | Show an item: description, compatibility, approved releases |
| [`phe package fetch`](phe-package-fetch.md) | Download approved releases into the package cache |
| [`phe package install`](phe-package-install.md) | Install an item on a device through PhoneLab |

## Device

A device on the PhoneLab service: push images, read logs, take screenshots.

| command | what it does |
|---|---|
| [`phe device status`](phe-device-status.md) | Show the service's and the devices' health |
| [`phe device ops`](phe-device-ops.md) | List the device's operations |
| [`phe device op`](phe-device-op.md) | Run any device operation |
| [`phe device push`](phe-device-push.md) | Install an image and boot it |
| [`phe device input`](phe-device-input.md) | Press keys, type, tap and swipe on the device |
| [`phe device shot`](phe-device-shot.md) | Take a screenshot |
| [`phe device log`](phe-device-log.md) | Show stored log lines |
| [`phe device report`](phe-device-report.md) | Show how a boot went |
| [`phe device diff`](phe-device-diff.md) | Show the log lines one boot has and another has not |

## Other

Everything else.

| command | what it does |
|---|---|
| [`phe shell`](phe-shell.md) | Open a shell in the toolchain container |
| [`phe version`](phe-version.md) | Show phe's version |
| [`phe help`](phe-help.md) | Show the commands, a command's manual page, or a topic |

## Environment

phe finds its pieces through these variables ("phe source status" shows what they resolve to):

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

Given as -O name=value, as many as needed (phe image rom -b myboard -O net=1 -O fonts=dejavu):

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
      Kernel recipes: the kernel's trace mask (default 0x80000000: panics only).

  screen=<w>x<h>
      Kernel recipes: the screen the display HAL reports (default 360x640).

  scale=n
      Kernel recipes: show each screen pixel as an n x n block (default 2).

  dbg=0|1
      Kernel recipes: a debug kernel (default 1).

  kclean=0|1
      Kernel recipes: rebuild the kernel objects even when nothing changed.

  x32-ld=bfd
      Force GNU ld for the x32 links.

  x32-defs=<component>:<MACRO>,...
      Extra defines for some components (diagnostic).

  x32-include=<component>:<header>,...
      Force-include a header in some components (diagnostic).

  x32-fullerr=0|1
      The whole compiler output of the first failure.
