# Phorepoc PDK

The platform development kit of [Phorepoc](https://github.com/Phorepoc): the tools to build the OS, adapt it to a device
and work on a running phone. It is one Go library with several front ends, so every feature lives in one place:

- **`ph`**, the command line;
- **PhoneLab**, a service that owns each phone's debug link and offers it as a web UI, a REST API and an
  [MCP](https://modelcontextprotocol.io) server for AI agents.

This first version holds PhoneLab and the start of `ph`. The build tools (components, ROM, resources, skins, SIS
packages, images for stock Symbian devices) move in next; the compilers themselves come from the
[toolchain](https://github.com/Phorepoc/toolchain) images.

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

```sh
go build -o ph ./cmd/ph
ph health
ph push symbian.img -wait 'running slot B'
ph log -grep fault -n 50
ph shot -o screen.png
ph op peek addr=0x01c20000 count=4
```

`ph` talks to a running PhoneLab (`-url`, or `$PHONELAB_URL`, default `http://localhost:8737`).

## Layout

| Path | What |
|---|---|
| `lib/` | the library: devices and their link, the store, the client |
| `frontend/` | the REST and MCP front ends |
| `web/` | the web UI (Vue 3 + Quasar); `npm ci && npm run build` writes `web/embed/dist`, which the binary embeds |
| `cmd/` | `phonelab` and `ph` |
| `docker/`, `scripts/` | running PhoneLab on Linux and Windows |

## Development

```sh
go test ./...
cd web && npm ci && npm test && npx vue-tsc --noEmit
```

## Licence

[EPL-1.0](LICENSE), as the rest of Phorepoc.
