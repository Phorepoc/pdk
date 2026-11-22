# The GPD Win 2 test rig in PhoneLab

Device type `gpd` (`lib/device/gpd`). The rig netboots the Phorepoc kernel from PhoneLab and reports back over UDP; every
other PhoneLab tool (`log_lines`, `wait_for`, `boots`, `boot_report`, `screenshot`, `key`, ...) works on it through the
`device` argument (`gpd-win2`).

- Image slot: PhoneLab serves `/gpd/boot.ipxe`, `/gpd/grubx64.efi`, `/gpd/phorepoc.elf` (no token: iPXE sends none) from
  `<data_dir>/gpd`. `push {file|blob, name, reboot}` writes the slot file, records its sha256 (the boot's slot is the
  first 12 hex digits) and sends the reboot command. It works with the link down; the rig then takes the image at its next
  netboot. An empty slot is a 404 on purpose (the rig falls back to Manjaro). `boot.ipxe` and `grubx64.efi` come from
  adaptation-pc `tools/gpd-rig`; grubx64.efi must be built with `GRUB_LAB=<lab ip>:8737`.
- Log: UDP `PHL1` packets on :6666 become the device's lines. A new boot id = a new boot; a sequence gap = a
  `link.loss` event; a new boot id without a commanded reset = a `fault` event (kind `watchdog` when the silence was 15 s to
  5 min, else `reset`) that marks the boot that ended as faulty.
- Ops: `reboot` (R), `hang` (H), `threads`/`dump` (D), `key` (K, same names as the phone plus F5/F6/F7/F9/F10),
  `text`, `touch`/`swipe` (T), `screenshot` (S, re-asked on a gap, PNG blob), `info` (ping: boot id + MAC, slot files, fetches).
- Health: `connected` = a log packet or ping reply within `link_timeout_s` (the lab pings every `ping_s`).
- Rig reports: at each Linux boot the rig's collector (adaptation-pc `tools/gpd-rig/linux/phorepoc-collect`) reads the
  last Phorepoc boot's log from the SSD's phorepoc-log partition and sends `[rig] report kind=<clean|fault|watchdog|no-log>
  boot=<id> bytes=<n> log=<path>`, `[rig] fault <line>`, the last 20 lines as `[rig] | ...` and `[rig] report end` as
  PHL1 lines under their own boot id. They become `rig.report` and `rig.fault` events (the replayed lines make none);
  `[disklog]` lines are tagged like `[tco]`.
