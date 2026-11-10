# PhoneLab

PhoneLab is one long-running process that owns each phone's debug link and exposes it to people and tools. Nothing
else opens the phone's port: a missing capability is a new operation in the device driver, not a side script.

```
phone (debug link, USB CDC ACM) ── Session (one per device: owns the port, reconnects, log lines, frames, op queue)
                                     │            └── store: SQLite (lines, events, boots, ops) + blobs/<sha256>
                                     ├── REST + WebSocket  /api   ── web UI  /   (embedded in the binary)
                                     ├── MCP (streamable HTTP) /mcp ── AI agents and other MCP clients
                                     └── (phe, the command line, talks to /api)
```

## Code

| Path | What |
|---|---|
| `lib/core` | `Manager` (all devices, `Exec` = run an op for any front end, `Health`), `Session` (the port, reconnects, the op queue, log + event store, `wait_for`), the frame deframer, the message bus |
| `lib/device` | the driver interface: op specs (data) + `Exec` + a log classifier. The API, the MCP tools and the UI's forms are generated from the op specs, so a new phone is a new driver package |
| `lib/device/phlink` | the phlink driver (a phone over USB): push (full and delta), slots, memory, screen (screenshot, live view), input, files, terminals, profile, hotspots, threads |
| `lib/store` | SQLite (pure Go, WAL) + blob files |
| `lib/transport` | serial ports found by USB VID/PID (COMn, /dev/ttyACMn, /dev/cu.usbmodem* alike) |
| `lib/mock` | a fake phone speaking the protocol, for tests and `-mock` runs |
| `lib/client` | a REST client for a running service (used by `phe`) |
| `frontend/rest`, `frontend/mcp` | the two network front ends over the library |
| `web/` | the web UI (Vue 3 + Quasar), built into `web/embed/dist` and embedded |
| `cmd/phonelab`, `cmd/phe` | the service and the command line |

Windows runs the service natively (Docker Desktop cannot see COM ports, and the phone re-enumerates on every reboot).
Linux runs it in Docker: `docker/compose.yaml` passes the ttyACM device major through a cgroup rule and a live `/dev`,
so the port is found again after each reboot; `docker/99-phonelab.rules` gives it a stable name.

## Install, and what reaches what

Install only `phe`, only PhoneLab, or both: see the README's [Install](../README.md#install). `phe` never opens a
phone's port; every `phe device` command (and `phe package install`) is a call on a PhoneLab service through `lib/client`,
so the service can run on another machine or in Docker. The release ships `phe` and `phonelab` in one archive per system
and the service as the image `ghcr.io/phorepoc/phonelab:<version>` and `:latest`.

Each front end is a thin layer over the same library (`lib/core`, `lib/store`, `lib/client`); this is what each one reaches.
The ops themselves (`peek`, `poke`, `dump`, `profile`, `hotspots`, `threads`, `reboot`, `fs_*`, `tty_*`, `key`, `touch`, `text`,
`swipe`, `exec`, `ps` ... whatever the device's driver offers) are generated into REST (`POST /ops/<op>`), MCP (one tool each) and
the Ops console, and `phe device op <op> key=value` runs any of them.

| Capability | `phe` | REST | MCP | Web UI |
|---|---|---|---|---|
| Health, link state | `device status` | `GET /api/health` | `health` | status bar, device card |
| Device list | `device list` | `GET /api/devices` | `devices` | device card |
| List a device's ops | `device ops` | `GET .../ops` | `ops_list` | Ops console |
| Run any op | `device op` | `POST .../ops/<op>` | one tool per op | Ops console |
| Push an image, wait for the boot report | `device push` (uploads the file; `-s` server path, `-B` blob) | op `push` (+ `POST /api/blobs`) | `push`, `blob_put` | Push page |
| Wait for a boot / a log line | `device push -w`, op `wait_boot` / `wait_for` | ops | tools `wait_boot`, `wait_for` | Push page |
| Boot report, boot diff | `device report`, `device diff` | ops `boot_report`, `diff_boots` | tools | Boots page |
| Input script (keys, text, taps, swipes) | `device input` | op `input` | `input` | Phone controls |
| Screenshot | `device shot` | op `screenshot` + `GET /api/blobs/<sha>` | `screenshot` (`inline`) | Screenshots, live screen |
| Live screen | (screenshots) | `GET .../frame`, WebSocket stream | (screenshots) | Live screen |
| Stored log lines, filtered | `device log` (`-g -a -x -b -n`, `-f` follows) | `GET .../lines` | `log_lines` | Log history, live log |
| Structured events | `device events` | `GET .../events` | `events` | Timeline, event list |
| Boots | `device boots` | `GET .../boots` | `boots` | Boots page |
| Operation history | `device history` | `GET .../history` | `op_history` | Ops console |
| Phone files: list, read, write, delete | `device ls`, `pull`, `put`, `rm` | `.../fs`, `.../fs/content` | ops `fs_list`, `fs_read`, `fs_write`, `fs_delete` | file browser |
| Terminals | `package install` (scripted); ops `tty_*` | `.../ttys`, WebSocket `.../tty/<id>` | ops `tty_*` | terminal tabs |
| Blob store: upload, list, download | `device upload`, `blobs` | `/api/blobs` | `blob_put`, `blobs` | Push, Screenshots |
| Keep a boot or blob | `device pin` | `POST /api/pin` | `pin` | pin buttons |
| Live event stream | `device log -f` (polls) | WebSocket `/api/stream` | (poll `log_lines`) | live panels |

Not on the command line: an interactive terminal (use the web terminal; `phe device op tty_new` / `tty_write` / `tty_read`
script one) and the live screen as a video (`device shot` in a loop; the web page or `GET .../frame`).

## Operations (phlink)

| Op | What |
|---|---|
| `push` {file \| blob, mode, reboot} | write an image into slot B and reboot into it. `delta` sends only the 64 KB blocks whose hash differs from what the phone reports for slot B |
| `rearm`, `reboot` | another slot B trial / a watchdog reset |
| `peek`, `poke`, `dump` | physical memory and registers |
| `screenshot` {which, store, live} | the OS screen, the panel layer or both, as PNG |
| `key`, `text`, `touch`, `swipe` | input (scan codes; pointer in screen pixels of the current orientation, tracked from the phone log; `raw=true` = portrait OS pixels; a touch on a blanked screen first wakes it with the power key) |
| `fs_list`, `fs_read`, `fs_write`, `fs_delete` | the phone's file system |
| `tty_list`, `tty_new`, `tty_read`, `tty_write`, `tty_resize`, `tty_kill` | the phone's shells (the phetty daemon) |
| `profile`, `hotspots`, `threads` | CPU per thread, RAM per process, sampled program counters, thread states |
| `version` | the protocol and capabilities of the running image |
| `wait_for` {pattern, timeout_s, since_line} | wait for a log line (built in, for every device) |
| `wait_boot` {after, pattern, timeout_s, symbols} | wait for the next boot (and a line in it); returns its report (built in) |
| `boot_report` {boot, pass, fail, symbols} | how a boot went: slot, outcome, faults with function names, panics, PASS/FAIL lines (built in) |
| `diff_boots` {a, b, grep} | lines in one boot and not the other, numbers and addresses masked; event counts that differ (built in) |
| `input` {script} | `key`, `text`, `tap`, `long`, `swipe`, `wait`, `shot` steps in one call (built in) |
| `send` {data, hex} | raw bytes, the escape hatch |

Every op that reboots the device (`push`, `rearm`, `reboot`) also takes `wait` (a pattern; `.` = any line): the call
then returns the new boot's report too. The wait runs after the device is free again, so the live view keeps going.

MCP adds `health`, `ops_list`, `log_lines` (`any` / `exclude` = substrings separated by `|`, `boot` -1 = the current
boot), `events`, `boots` and `op_history`, which read the stored history; its `screenshot` takes `inline` to return the
picture itself.

## The phone side

The debug link interleaves the phone's log with binary frames: `A5 5A 'P' 'F'`, type, flags, sequence, length,
byte sum, payload. Commands from the PC are a letter and two 32-bit arguments (`S` screenshot, `L` live frame, `K` key,
`T` touch, `F` file request, `Y`/`X`/`G`/`Z` delta upload ...); `V` reports the protocol version and capabilities, so
the driver adapts to older images.

- **Screenshots:** run-length coded 24-bit rows. **Live view:** the screen in 32x32 tiles; the phone keeps a hash per
  tile and sends only tiles that changed, LZ4-compressed; an unchanged screen costs 60 bytes. A keyframe resets it.
- **Files and terminals:** the kernel link has no file server, so `F` requests go through a small driver to a user-side
  helper (`phfs.exe`) that runs them with the file server, or hands terminal requests to the phetty daemon.
  Replies come back as a blob frame: a Symbian status code, then data.
- **Delta upload:** the phone hashes slot B per 64 KB block; PhoneLab sends only the differing runs, then the phone
  re-hashes what is really on the card before it marks the slot for a trial boot.

## The web UI

An IDE-style layout: tool windows on the left (Project = the phone's files, Device, Views, Hotspots), a full-width
bottom area (Live log + events, Log history, Timeline, Terminal) and editor tabs in the middle (Live screen, Phone
controls with an input-script box, Ops console, Profile, Push image with the new boot's report, Boots = a boot's report
and the difference between two boots, Screenshots, files). Tabs and terminal tabs split by dragging onto an edge
of a group. The layout is kept in the browser.

## Open

- One request at a time per device: a push pauses the live view. Sharing the link between requests needs work on both
  sides.
- A boot's outcome counts any thread exit as a fault; only real panics and exceptions should count.
- Push as a background job (a reload loses the progress view today).
