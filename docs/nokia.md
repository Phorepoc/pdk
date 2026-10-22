# PhoneLab on stock Nokia Symbian phones

PhoneLab drives Phorepoc devices over phlink. The `nokia` driver does the same job for **stock Nokia Symbian^3 / Anna /
Belle phones**: your own phone and your own apps. You can list, read and write files, start your exe, list processes,
take screenshots and follow a log from `ph`, the REST API, the web UI or MCP. It does not depend on Phorepoc images.

The phone side is **phedbg**, a small native agent in the spirit of the on-device debug agents that Symbian SDKs used to
ship. The owner starts it, can always see that it is running, and can stop it at any time. A PyS60 script that speaks
the same protocol can stand in for it over TCP.

```
 ph / web / MCP ──► PhoneLab ──► driver "nokia" ──► transport ──► phone agent
                                                    serial (USB CDC ACM)   phedbg: RComm "ACM::n"
                                                    tcp dial / listen      phedbg: RSocket listen | PyS60 script: connects in
```

## 1. Transports

| | USB (main) | TCP (alternative) |
|---|---|---|
| Phone mode | USB mode "PC Suite" / "Nokia Suite" | Wi-Fi or USB networking, any mode |
| Host side | `port = "COM7"` / `/dev/nokia-acm-<if>` | `opts.transport="tcp"` + `dial="ip:7654"` or `listen="0.0.0.0:9999"` |
| Phone side | phedbg opens `ACM::<n>` through RComm (CSY `ECACM`) | phedbg listens; a PyS60 agent connects out |
| Setup | none beyond the cable | an access point, and the laptop's IP for PyS60 |

**Which ACM port.** In PC Suite mode the phone exposes several CDC ACM functions. One of them is the phone's own
modem: the AT command port that tethering and dial-up use. **phedbg never uses that one.** It opens a different ACM
unit, `ACM::1` by default (the setting is in the app). On the host it appears as a second COM port, or as a second
`ttyACM` on Linux, with a different USB interface number. To pin the port:

- Windows: Device Manager → Ports. The modem port shows up under *Modems*, not under *Ports*. The other ACM port is
  phedbg's. Put its `COMn` in `port`.
- Linux: `docker/99-phonelab.rules` names every ACM tty of vendor `0421` as `/dev/nokia-acm-<interface number>`. Run
  `udevadm info /dev/ttyACMx` to find the interface whose port answers `@0 hello`. The rule also sets
  `ID_MM_DEVICE_IGNORE=1`. **This is required**: otherwise ModemManager claims the modem port and probes the other ACM
  ports with AT commands.
- If the phone's USB personality gives only one free ACM unit, set phedbg's ACM unit to `0` (and check that it is not
  the modem).

`99-phonelab.rules` also adds the `dialout` group. `compose.yaml` passes in every `ttyACM` (major 166), so the
container
needs no extra device entry.

## 2. phedbg line protocol v1

Text lines end in `\n`. Field values are percent-encoded (`%`, space, `=`, CR, LF and control characters). The same
protocol runs over ACM and TCP. It is plain text so that you can debug it in a terminal and a 30-line PyS60 script
can implement it.

```
host -> phone   <id> <op> tok=<pairing code> [k=v ...]
                <id> d <base64>                    fs_write payload, 3072 raw bytes per line, after the request
phone -> host   @0 hello proto=1 agent=phedbg ver=1.0 ops=info,fs_list,...   on every connect
                @<id> e k=v ...                    one entry (fs_list, ps)
                @<id> d <base64>                   one data chunk (fs_read, screenshot)
                @<id> ok [k=v ...]                 final reply
                @<id> err code=<c> [msg=...]       final error
                # <text>                           session log line (anything else is a log line too)
```

Error codes: `auth` (wrong or missing pairing code), `unsupported` (the op is disabled in settings, or the build or
platform cannot do it), `denied` (outside the write allow-list, or the user rejected the on-phone confirmation),
`notfound`, `io`, `badarg`. The driver returns `unsupported` as a typed error (`nokia.IsUnsupported`) so tools can skip
the op.

| op | args | reply |
|---|---|---|
| info | | `agent ver model ram_free drives caps` |
| fs_list | path | `e name= size= dir=0/1` lines |
| fs_read | path | `d` lines + `ok size=` |
| fs_write | path, size + `d` lines | `ok size=` (allow-list only) |
| fs_delete | path | `ok` (allow-list only) |
| exec | path, args, wait | `ok pid=` / `ok exit_type= exit_reason=` |
| ps | | `e name= pid=` lines |
| screenshot | | `d` lines (24-bit BMP) + `ok fmt=bmp w= h=` |
| log | | the agent's session log, as `e text=` lines |
| key / touch | code / x,y | `unsupported` in the self-signed build (see 4) |

PhoneLab stores each line in the device's history like any other log line. A screenshot or a file read also becomes a
blob, which `fs_read out=` or `screenshot out=` saves to disk.

## 3. Safety model: it is somebody's own phone

- **Explicit.** phedbg is an ordinary application with a launcher icon. It does nothing until the user starts it,
  then selects *Start server*. There is no autostart, no boot-time registration, no background process after
  *Exit*, and no hidden mode.
- **Visible.** While the server runs, the app's title pane shows `phedbg: ON (USB/TCP)`, and the session counter and
  the last command update live. Leaving the app does not hide it: it stays in the task switcher under its own name.
- **Paired.** At first start phedbg makes a random 6-digit pairing code and shows it on screen. Every request must
  carry it (`tok=`). The user can regenerate the code, which cuts off old hosts. In PhoneLab the code goes in
  `[device.opts] token`.
- **Confined writes.** fs_write and fs_delete only work under the allow-list. The default is `C:\Data\phedbg\` and the
  app's private directory, plus any `E:\…`/`F:\…` paths the user adds in settings. `..` is refused. Reads are limited by
  the platform's data caging. With user capabilities, `\private\` of other apps and `\sys\` are unreadable.
- **Per-op switches.** File read, file write, exec, screenshot, log and input can each be turned off. A disabled op
  answers `unsupported`.
- **Confirmation (optional).** With *Confirm writes/exec* on, each fs_write, fs_delete and exec shows a query on the
  phone. *No* answers `denied`.
- **Session log.** The app records every request (op, path, verdict) and shows it in *Session log*.
- **Capabilities: `ReadUserData WriteUserData NetworkServices LocalServices`.** No TCB, AllFiles, DRM or SwEvent, so a
  self-signed SIS installs on a stock phone.

## 4. Capability-gated features (switched on in a later build)

v1 ships self-signed with the four user capabilities. Each feature that needs more is behind a compile-time switch in
`phedbg_caps.h`, and is reported in `info caps=`. When the switch is off, the op answers `unsupported`.

| feature | needs | v1 behaviour |
|---|---|---|
| key / touch injection into other apps | SwEvent (`RWsSession::SimulateRawEvent`, `SendEventToWindowGroup` to another group) | `unsupported` |
| read `\sys\bin`, other apps' `\private` | AllFiles | the platform returns `KErrPermissionDenied` → `err code=denied` |
| write to `\sys\`, `\resource\` | TCB | outside the allow-list anyway |
| kernel/system trace (`RBTrace`) | CommDD / extended caps | `log` returns the agent's own session log only |

The screenshot uses `CWsScreenDevice::CopyScreenToBitmap`. In the reference window server this command has no
capability check, so the self-signed build can take screenshots. If a firmware adds a check, screenshot answers
`denied`, and the other ops keep working.

## 5. phedbg on the phone

The main view is an Avkon settings list. Settings are saved in `phedbg.ini` in the app's private directory.

| setting | values |
|---|---|
| Server | Off / On (start/stop on demand) |
| Transport | USB / TCP / USB + TCP |
| USB ACM unit | 0..7 (default 1; not the modem) |
| TCP port | default 7654 |
| TCP listen | This phone only (127.0.0.1) / LAN (all interfaces) |
| Pairing code | shown; Options → *New pairing code* |
| Allow: file read / write / exec / screenshot / log / input | On / Off each (input is fixed Off in a build without SwEvent) |
| Confirm writes & exec | On / Off |

Options menu: Start/Stop server, New pairing code, Write allow-list (view, add, remove paths), Session log, Exit.
*Exit* closes every connection and the process.

## 6. Using it

```
# phonelab.toml
[[device]]
id = "nokia"
driver = "nokia"
port = "COM7"                 # phedbg's ACM port (Linux: /dev/nokia-acm-<if>)
[device.opts]
token = "123456"              # the code on the phone's screen
# transport = "tcp"
# dial = "192.168.1.20:7654"  # phedbg over Wi-Fi
# listen = "0.0.0.0:9999"     # a PyS60 agent connecting in
```

Then use `ph device op nokia fs_list path=c:\data\phedbg\`, `ph device shot nokia`, and so on. The mock agent
(`lib/mock/nokia.go`) implements the protocol, so you can test the driver without a phone.

## 7. Installing phedbg

phedbg ships as a self-signed SIS. Copy it to the phone and open it in the File manager. Choose *Install*, accept the
"untrusted application" warning, pick a drive and confirm. Start *phedbg* from the menu, read the pairing code and
select *Start server*.
