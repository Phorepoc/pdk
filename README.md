# Phorepoc PDK

The PDK is the toolbox for [Phorepoc](https://github.com/Phorepoc). It builds the operating system, makes the images
you install, and helps you work on a phone while it's running. There are two ways in:

- **`phe`** is the command line you build with.
- **PhoneLab** is a small service that holds a phone's debug connection and gives you a web page to work with it. It
  also has a REST API, and an [MCP](https://modelcontextprotocol.io) server so AI agents can use it too.

Both are written in Go on top of the same library. The build tools themselves are Python scripts in `tools/`. They
build the OS from [core](https://github.com/Phorepoc/core) inside the [toolchain](https://github.com/Phorepoc/toolchain)
container, and `phe` starts that container for you.

## Building Phorepoc with phe

You need Go 1.27 and Docker. Clone `core`, and the repositories of the board you're building for, into the same folder
as this one, then build `phe`:

```sh
go build -o phe ./cmd/phe
```

A board is a repository with a `board.conf` that names everything it's made of. `phe board list` shows the boards it
finds next to core, and `phe board info <board>` what each needs and which image recipes it brings. The pdk itself
knows no boards: [docs/layering.md](docs/layering.md) explains how the repositories fit together, and how to make
your own.

Any board builds the same way:

```sh
phe source fetch                                  # download the Symbian source
phe source export -a                              # put together the include tree
phe source mbg -a                                 # generate the icon headers
phe component resources -A -b <board>             # compile the resource files
phe component build -A -arch <arch> -b <board>    # build every component
phe image release -b <board>                      # the board's release files, in build/release
```

`phe image release` runs the board's own recipe. The board's README describes the steps it takes, and the
`phe image recipe` commands that run them one at a time.

`phe help` lists every command and `phe help <command>` explains one. The same pages are in [docs/phe](docs/phe/README.md).

## PhoneLab

```sh
go build -o phonelab ./cmd/phonelab
./phonelab
```

Then open `http://localhost:8737`. PhoneLab finds the phone on USB by itself. If you don't have one plugged in, you can
replay a recorded session instead:

```sh
./phonelab -mock testdata/boot-slotb-rearm.log -data /tmp/phonelab-mock
```

Settings live in [phonelab.toml](phonelab.toml). On Windows, `scripts/install-windows.ps1` (run once as administrator)
starts it when you log in. On Linux, use `docker compose -f docker/compose.yaml up -d`. MCP clients connect to
`http://localhost:8737/mcp`.

With PhoneLab running, `phe device` talks to the phone through it:

```sh
phe device status
phe device push symbian.img -w 'running slot B'    # flash an image, wait for it to boot, show the report
phe device log -g fault -n 50                      # the last 50 lines mentioning a fault
phe device shot -o screen.png                      # a screenshot
```

[docs/phonelab.md](docs/phonelab.md) has the full story.

## Releases

Release images are built by GitHub Actions. The device repositories (the per-board repositories) call
[board-image.yml](.github/workflows/board-image.yml) here, which runs the steps above at the commits listed in that
repository's `board.lock`. When a `v*` tag is pushed, the images get attached to a draft release. `tools/board_lock.sh`
updates `board.lock`.

## Working on the PDK

```sh
go test ./...
cd web && npm ci && npm test && npx vue-tsc --noEmit
```

`lib/` is the shared library and `cmd/` has the two programs. `frontend/` has the REST and MCP servers, and `web/` the
PhoneLab web page (Vue 3 and Quasar). Build tools and the compiler wrapper are in `tools/`.

## License

The PDK is [Apache-2.0](LICENSE). A few files that started as Nokia's are EPL-2.0, with GPL-2.0-or-later as a Secondary
License, and some Symbian test data is still EPL-1.0. Every file's header says which applies.

Contributions are welcome under the same terms. Add your name to `AUTHORS` in your first pull request.
