# Phorepoc PDK

The platform development kit of [Phorepoc](https://github.com/Phorepoc): the tools to build the OS, adapt it to a device
and work on a running phone. It is one Go library with several front ends, so every feature lives in one place:

- **`ph`**, the command line;
- **PhoneLab**, a service that owns each phone's debug link and offers it as a web UI, a REST API and an
  [MCP](https://modelcontextprotocol.io) server for AI agents.

The build tools (components, ROM, resources, SIS packages) are in `tools/`; they build the OS in the
[core](https://github.com/Phorepoc/core) repository inside the [toolchain](https://github.com/Phorepoc/toolchain)
image. `ph` runs them there.

## PhoneLab

Build and run (Go 1.27; Node 24 only to rebuild the web UI):

```sh
go build -o phonelab ./cmd/phonelab
./phonelab                         # listens on :8737; finds the phone by USB VID/PID
./phonelab -mock testdata/boot-slotb-rearm.log -data /tmp/phonelab-mock   # no hardware: a replayed phone
```

Open `http://localhost:8737`. Configuration: [`phonelab.toml`](phonelab.toml) (listen address, data folder, access
token, devices). Data goes to `%LOCALAPPDATA%\phonelab` or `~/.local/share/phonelab`.

- **Windows:** run it natively; `scripts/install-windows.ps1` (elevated, once) adds a logon task and a LAN firewall rule.
- **Linux:** `docker compose -f docker/compose.yaml up -d`.
- **MCP clients** connect to `http://localhost:8737/mcp` (streamable HTTP).

How it works, the operations and the phone side: [docs/phonelab.md](docs/phonelab.md).

## ph

Every command is a verb on one kind of object, in the manner of zfs(8): `ph source ...`, `ph component ...`,
`ph image ...`, `ph board ...`, `ph device ...`. `ph help` lists them, `ph help <command>` is a command's manual page,
and the same pages are in [docs/ph/](docs/ph/README.md).

Build commands need Docker; `core` and the adaptation repositories are expected next to this checkout (or set
`PHOREPOC_CORE`, `PHOREPOC_ADAPTATIONS`). A clean PC build:

```sh
go build -o ph ./cmd/ph
ph source fetch                        # the Symbian source -> core/symbian/
ph source export -a                    # the include tree (and the case links)
ph source mbg -a                       # the icon id headers
ph component resources -A -b pc        # resources and their .rsg headers
ph component build -A -b pc            # every component -> build/x32img/
ph image rom -b pc                     # the ROM
```

Device commands, through a running PhoneLab (`-url`, or `$PHONELAB_URL`, default `http://localhost:8737`):

```sh
ph device status
ph device push symbian.img -w 'running slot B'    # prints the new boot's report
ph device report -s symbian.syms                  # faults with function names, panics, PASS/FAIL
ph device diff 1094 1095
ph device input 'key menu; wait 1000; shot'
ph device log -g fault -n 50
ph device shot -o screen.png
ph device op peek addr=0x01c20000 count=4
```

## Layout

| Path | What |
|---|---|
| `lib/` | the library: the build runner, devices and their link, the store, the client |
| `frontend/` | the REST and MCP front ends |
| `web/` | the web UI (Vue 3 + Quasar); `npm ci && npm run build` writes `web/embed/dist`, which the binary embeds |
| `cmd/` | `phonelab` and `ph` |
| `tools/` | the build tools (Python): `emitter/` builders, `toolchain/` compiler front end, ROM, resource and SIS tools |
| `tests/` | `compare_images.py`: byte-compare built images with another build |
| `docker/`, `scripts/` | running PhoneLab on Linux and Windows |

## Development

```sh
go test ./...
cd web && npm ci && npm test && npx vue-tsc --noEmit
```

## Licence

[Apache-2.0](LICENSE), copyright The Phorepoc Authors ([AUTHORS](AUTHORS), [NOTICE](NOTICE)). Two files derived from
the Symbian source stay under [EPL-1.0](LICENSES/EPL-1.0.txt); each file's header says which licence it is under.
Contributions are welcome under the same terms: add yourself to `AUTHORS`.
