# How the repositories fit together

Phorepoc is split into repositories so that each one can be reused without the others above it. The rule that makes
this work:

> **A repository may name only what is below it.** Nothing in a lower layer knows which layers, chips or devices sit
> on top of it.

```
board          adaptation-pinephone, adaptation-pc, ...   one device: picks everything below, makes its images
SoC            adaptation-a64, adaptation-x86-64, ...     one chip family: kernel port, drivers
layers         tls, mesa-symbian, codecpack, webengine     optional libraries and apps on top of the OS
core           the operating system, architecture-neutral
pdk            the tools: ph, the build scripts, the CI pipeline, PhoneLab
toolchain      the compilers, as container images
```

So core builds for an *architecture* (`x32`, `arm`), never for a device. The pdk builds whatever a board describes, and
has no list of boards in it. To bring Phorepoc to new hardware you write your own SoC and board repositories. You don't
change core or the pdk.

## What each layer may contain

| Layer | Contains | Never contains |
|---|---|---|
| toolchain | compilers and host tools, one image per architecture family | anything about Phorepoc's sources |
| pdk | `ph`, the emitter and ROM tools, the generic CI workflows, PhoneLab and its protocols | board names, chip names, image recipes for a device |
| core | the OS components, the default settings every device starts from (`os/config/`), the interfaces drivers implement (HAL functions, plug-in APIs), null drivers | drivers for real hardware, device settings, mentions of the repositories above |
| layers | one feature each, buildable for any architecture core supports | device names. A layer that only fits some hardware (Mesa's lima for Mali GPUs) names the *hardware*, not a device |
| SoC | the kernel port and drivers for a chip, implementing core's interfaces | board names, a device's settings or images |
| board | the board manifest, the pinned commits, settings, data and image recipes for one device | |

## The board manifest

A board repository has a `board.conf` at its root. The pdk reads it, both locally (`ph ... -b <name>`) and in CI:

```sh
NAME=pinephone
SUMMARY="the PINE64 PinePhone"
ARCH=arm                                          # the architecture core is built for: x32 or arm
REPOS="adaptation-a64 adaptation-pinephone mesa-symbian tls"   # the repositories it is made of, besides pdk and core
RELEASE=recipes/release.sh                        # makes the release files into $OUT (CI and `ph image release`)
```

`ph` finds boards by looking for a `board.conf` in each checkout next to core, so a new board needs no change
anywhere else. `board.lock`, next to it, pins the commit of each repository a release is built from
(`tools/board_lock.sh <board repository>` in the pdk).

## Recipes

Any repository can have a `recipes/` folder of shell scripts. The pdk knows three kinds of them:

- **`recipes/prepare.sh`** is run for every repository of the board before the components are built, on the host,
  with the `PHOREPOC_*` variables set and `PHOREPOC_ARCH` set to the architecture. `tls` uses it to fetch mbedTLS,
  for example, and `mesa-symbian` to build Mesa for ARM. It may run containers itself.
- **`recipes/<name>.sh`** is what `ph image recipe <name> -b <board>` runs: a kernel, a disk image, an appliance, a
  boot test. The board's repositories are searched from the top (the board first), so a board can replace a SoC's
  recipe. These run inside the toolchain container.
- **The board's `RELEASE` recipe** (`ph image release -b <board>`) runs on the host after every component is built.
  It writes the files to publish into `$OUT`, and can call `ph` for each step.
- **`recipes/host/*.sh`** are host-side helpers other recipes call (a SoC's boot shim that needs a cross-compiler,
  for example). `ph` never runs them by itself.

## CI

The pdk's reusable workflow, `board-image.yml`, builds in one of two ways:

- **Core alone**: given `arch`, it builds pdk and core for that architecture and, with `publish_layer`, publishes the
  build tree as `ghcr.io/<owner>/core-<arch>:<core commit>` (and `:main`). Core's own workflow calls it once per
  architecture.
- **A board**: without `arch`, it reads the calling repository's `board.conf` and `board.lock`, starts from the
  `core-<arch>` layer at the pinned core commit, runs the prepare recipes, builds every component, then runs the
  board's `RELEASE` recipe. A board's release workflow is only this:

```yaml
on: { push: { tags: ["v*"] }, workflow_dispatch: }
jobs:
  image:
    uses: Phorepoc/pdk/.github/workflows/board-image.yml@main
    with: { release: "${{ startsWith(github.ref, 'refs/tags/') }}" }
    permissions: { contents: write, packages: write }
```

## Keeping it this way

Every repository's lint checks for names from the layers above it (`tools/ci/layering.sh` in the pdk, given that
repository's own list). A pull request that makes core mention a device fails.
