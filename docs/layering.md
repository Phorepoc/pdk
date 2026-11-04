# How the repositories fit together

Phorepoc is split into repositories so that each one can be reused without the others above it. The rule that makes
this work:

> **A repository may name only what is below it.** Nothing in a lower layer knows which layers, chips or devices sit
> on top of it.

```
board          one device: picks everything below, makes its images
SoC            one chip family: kernel port, drivers
layers         optional libraries and apps on top of the OS
core           the operating system, architecture-neutral
pdk            the tools: phe, the build scripts, the CI pipeline, PhoneLab
toolchain      the compilers, as container images
```

So core builds for an *architecture* (`x32`, `arm`), never for a device. The pdk builds whatever a board describes, and
has no list of boards in it. To bring Phorepoc to new hardware you write your own SoC and board repositories. You don't
change core or the pdk.

## What each layer may contain

| Layer | Contains | Never contains |
|---|---|---|
| toolchain | compilers and host tools, one image per architecture family | anything about Phorepoc's sources |
| pdk | `phe`, the emitter and ROM tools, the generic CI workflows, PhoneLab and its protocols | board names, chip names, image recipes for a device |
| core | the OS components, the default settings every device starts from (`os/config/`), the interfaces drivers implement (HAL functions, plug-in APIs), null drivers | drivers for real hardware, device settings, mentions of the repositories above |
| layers | one feature each, buildable for any architecture core supports | device names. A layer that only fits some hardware (Mesa's backend for one GPU family) names the *hardware*, not a device |
| SoC | the kernel port and drivers for a chip, implementing core's interfaces | board names, a device's settings or images |
| board | the board manifest, the pinned commits, settings, data and image recipes for one device | |

## The board manifest

A board repository has a `board.conf` at its root. The pdk reads it, both locally (`phe ... -b <name>`) and in CI:

```sh
NAME=myboard
SUMMARY="my phone"
ARCH=arm                                          # the architecture core is built for: x32 or arm
REPOS="mysoc myboard mesa-symbian tls"            # the repositories it is made of, besides pdk and core
RELEASE=recipes/release.sh                        # makes the release files into $OUT (CI and `phe image release`)
```

`phe` finds boards by looking for a `board.conf` in each checkout next to core, so a new board needs no change
anywhere else. `board.lock`, next to it, pins the commit of each repository a release is built from
(`tools/board_lock.sh <board repository>` in the pdk).

## Settings: `config/` and `board/`

Every repository's `os/` folder is merged into one OS tree for the build. Two folders in it hold settings:

- **`config/`** is core's: the defaults every device starts from (central repository files and their list,
  `cenrep_files.txt`, start-up command lists, the installer policy).
- **`board/`** is the board's own: `cenrep/` (overrides core's repositories), `hal/` (the HAL values and its
  additions), `estart.txt`, `variant.mmh`, and `zdata.txt`, the extra files its Z: drive carries (one
  `rom path = OS tree path` per line).

The architecture's user-side runtime (for x32: `toolchain/x32/`, the startup code, the C++ runtime glue, the
`setjmp`) is core's too, so core builds for an architecture with no SoC repository present.

## Recipes

Any repository can have a `recipes/` folder of shell scripts. The pdk knows three kinds of them:

- **`recipes/prepare.sh`** is run for every repository of the board before the components are built, on the host,
  with the `PHOREPOC_*` variables set and `PHOREPOC_ARCH` set to the architecture. `tls` uses it to fetch mbedTLS,
  for example, and `mesa-symbian` to build Mesa for ARM. It may run containers itself.
- **`recipes/<name>.sh`** is what `phe image recipe <name> -b <board>` runs: a kernel, a disk image, an appliance, a
  boot test. The board's repositories are searched from the top (the board first), so a board can replace a SoC's
  recipe. These run inside the toolchain container.
- **The board's `RELEASE` recipe** (`phe image release -b <board>`) runs on the host after every component is built.
  It writes the files to publish into `$OUT`, and can call `phe` for each step.
- **`recipes/host/*.sh`** are host-side helpers other recipes call (a SoC's boot shim that needs a cross-compiler,
  for example). `phe` never runs them by itself.

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

### What a build reuses

A build is incremental at every level, and every reuse is decided by **content**, never by a time (a fresh checkout dates
every file now, an unpacked layer dates every object when it was unpacked):

- **Objects.** `tools/emitter/objcache.py` stores next to each object a key (the compiler's own bytes and the command
  line, with the mount points written as what they are, so the core layer and a board build name things alike) and a
  digest of the content of every file the compiler read (the `-MMD` depfile). The object is reused when both are
  unchanged. So the board starts from `core-<arch>` and compiles only its own components and whatever really changed,
  and no cache can hand back an object made from other bytes. (Like make, it cannot see a *new* file that would now be
  found earlier on an include path; the include directories under the merged OS tree list the files a layer other than
  core adds to them in the key.) `OBJCACHE_EXPLAIN=1` (workflow input `explain`) names the file that made an object rebuild.
- **Exports.** `phe source export` resolves which bld.inf wins each header first and writes a header only when its bytes
  differ, so an unchanged tree keeps every header untouched.
- **Layers' prepare recipes.** A repository lists in `recipes/prepare.cache` the images, key files and folders its
  prepare recipe leaves behind (Mesa: its source, host build and objects); `tools/ci/prepare_cache.sh` turns that into one
  `actions/cache` entry keyed by their content. What the cached files are compiled against is still checked by content
  when they are used.
- **The Symbian source** (`core/fetch.sh`'s output) is cached by the content of `sources.lock` and `fetch.sh`.
- **Jobs.** When the published layer was made by the same compile front end (`tools/ci/frontend_id.sh`: the trees of
  `tools/emitter` and `tools/toolchain`, the toolchain image), little is left to compile, so the job that prepared the
  tree also links and releases (`.github/actions/board-finish`). Otherwise (no layer, a changed compiler or flag set) the
  compile is split over shards as before. The split only decides how fast it goes, not what is reused.

A board's release workflow keeps `concurrency: {group: release, cancel-in-progress: false}`: GitHub keeps one running and
one pending run and cancels an older *pending* one when a newer is queued, which is safe because each run works out its
version from the tags and the commits since the last release. A running release is never cancelled (it may be
publishing). `layer_prefix` lets a test run read and write its own layers (`test-<commit>`) instead of the boards'.

## Keeping it this way

Every repository's lint checks for names from the layers above it (`tools/ci/layering.sh` in the pdk, given that
repository's own list). A pull request that makes core mention a device fails.

A line `!<path>` in `.layering` leaves vendored upstream files out (they stay unmodified); a line of ours may end in
`layering: ok` when it quotes an upstream name on purpose.
