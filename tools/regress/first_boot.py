#!/usr/bin/env python3
# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0
"""first_boot.py -- regression test: the x86 image's home screen on a FRESHLY FORMATTED C:.

Boots the image in QEMU twice from the same blank disk (first boot = the configuration install runs; second boot =
it is already installed) and, for each boot, fails unless

  * the home screen reaches the foreground (aifw's state manager logs "aState: EAiFwForeground", in builds with
    the home screen's logging on; otherwise a screendump every 15 s decides), and
  * a screendump taken a few seconds later shows the home screen's widgets (clock/date/profile boxes in the
    top part, the softkey bar at the bottom) and not just the skinned background.

The screen is only checked while the phone is awake: SysAp switches the lights off after ~3.5 minutes of
inactivity and the home screen then goes to the background (the dump is black or a bare wallpaper).

  python3 first_boot.py --iso ISO --disk BLANK.img --out DIR [--smp N] [--mem MB] [--settle S] [--boots 2]

The disk is copied for the run; BLANK.img is never booted itself. Exit 0 = all boots pass. Pure python 3
(no PIL): the dump is a PPM; DIR gets bootN.ppm, bootN.png and bootN.log (timestamped serial log).
"""
import argparse
import os
import shutil
import socket
import struct
import subprocess
import sys
import threading
import time
import zlib

MARKER = b"aState: EAiFwForeground"


def read_ppm(path):
    with open(path, "rb") as f:
        data = f.read()
    # "P6\n<w> <h>\n255\n" (QEMU writes exactly that)
    parts = data.split(b"\n", 3)
    w, h = (int(x) for x in parts[1].split())
    return w, h, parts[3]


def shot(mon, out, n):
    """screendump through the QEMU monitor -> bootN.ppm + bootN.png; returns (w, h, rgb)."""
    s = socket.socket(socket.AF_UNIX)
    for _ in range(20):
        try:
            s.connect(mon)
            break
        except OSError:
            time.sleep(0.5)
    ppm = os.path.join(out, "boot%d.ppm" % n)
    s.sendall(("screendump %s\n" % ppm).encode())
    for _ in range(20):
        time.sleep(0.5)
        if os.path.exists(ppm) and os.path.getsize(ppm) > 1000:
            time.sleep(0.5)
            break
    w, h, rgb = read_ppm(ppm)
    write_png(os.path.join(out, "boot%d.png" % n), w, h, rgb)
    return w, h, rgb


def write_png(path, w, h, rgb):
    raw = b"".join(b"\x00" + rgb[y * w * 3:(y + 1) * w * 3] for y in range(h))

    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)

    with open(path, "wb") as f:
        f.write(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) +
                chunk(b"IDAT", zlib.compress(raw, 6)) + chunk(b"IEND", b""))


def analyse(w, h, rgb):
    """(widget_fraction, softkey_fraction). Regions are fractions of the screen, so the check does not depend
    on the 2x pixel doubling."""
    def px(x, y):
        i = (y * w + x) * 3
        return rgb[i], rgb[i + 1], rgb[i + 2]
    # widget boxes: translucent dark grey-blue (about 140,150,160) on a wallpaper that is light everywhere
    x0, x1, y0, y1 = int(w * .05), int(w * .95), int(h * .07), int(h * .21)
    n = g = 0
    for y in range(y0, y1, 4):
        for x in range(x0, x1, 4):
            r, gg, b = px(x, y)
            n += 1
            if (r + gg + b) / 3 < 175 and abs(r - b) < 40:
                g += 1
    # softkey bar: teal (about 99,158,156), the bottom ~8 %
    y2, y3 = int(h * .91), int(h * .99)
    m = s = 0
    for y in range(y2, y3, 4):
        for x in range(0, w, 4):
            r, gg, b = px(x, y)
            m += 1
            if r < 140 and gg > 130 and gg > r + 30 and b > r + 30:
                s += 1
    return g / n, s / m


def boot(args, n, disk):
    out = args.out
    log = open(os.path.join(out, "boot%d.log" % n), "wb")
    mon = os.path.join(out, "mon%d.sock" % n)
    if os.path.exists(mon):
        os.unlink(mon)
    cmd = ["qemu-system-x86_64", "-boot", "d", "-cdrom", args.iso, "-drive", "file=%s,format=raw,if=ide,index=0" % disk,
           "-m", str(args.mem), "-smp", str(args.smp), "-display", "none", "-serial", "stdio", "-no-reboot",
           "-monitor", "unix:%s,server,nowait" % mon]
    t0 = time.time()
    p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    fg = []

    def reader():
        for line in p.stdout:
            log.write(b"%7.1f " % (time.time() - t0) + line)
            if MARKER in line and not fg:
                fg.append(time.time() - t0)

    threading.Thread(target=reader, daemon=True).start()
    result = {"fg": None}
    try:
        # The marker is an aifw RD-log line, which only builds with the home screen's logging switched on print.
        # So the screen decides too: from 40 s on it is dumped every 15 s, and the home screen's widgets count as
        # the foreground. This must happen before SysAp switches the lights off (~3.5 min of guest time).
        next_poll, wf, sf = 40.0, 0.0, 0.0
        while not fg and time.time() - t0 < args.timeout and p.poll() is None:
            time.sleep(0.5)
            if time.time() - t0 >= next_poll and not fg:
                next_poll += 15
                w, h, rgb = shot(mon, out, n)
                wf, sf = analyse(w, h, rgb)
                if wf >= 0.15 and sf >= 0.3:
                    result.update(fg=time.time() - t0, widgets=wf, softkeys=sf, by="screen")
                    return result, None
        if not fg:
            if p.poll() is None:
                shot(mon, out, n)              # what the screen shows instead
            return result, "no home screen within %ds (no EAiFwForeground, no widgets on screen; see boot%d.png)" % (args.timeout, n)
        result["fg"] = fg[0]
        while time.time() - t0 < fg[0] + args.settle:
            time.sleep(0.5)
        w, h, rgb = shot(mon, out, n)
        wf, sf = analyse(w, h, rgb)
        result["widgets"], result["softkeys"] = wf, sf
        if wf < 0.15 or sf < 0.3:
            return result, ("screen has no home-screen widgets (widget boxes %.0f%%, softkey bar %.0f%%; see boot%d.png)"
                            % (wf * 100, sf * 100, n))
        return result, None
    finally:
        p.kill()
        time.sleep(1)
        log.flush()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--iso", required=True)
    ap.add_argument("--disk", required=True, help="a freshly formatted C: image (copied, never booted itself)")
    ap.add_argument("--out", required=True)
    ap.add_argument("--smp", type=int, default=2)
    ap.add_argument("--mem", type=int, default=1024)
    ap.add_argument("--settle", type=float, default=25, help="seconds from EAiFwForeground to the screendump")
    ap.add_argument("--timeout", type=float, default=420, help="wall-clock seconds to wait for EAiFwForeground (QEMU without KVM is slow, more so when the host is busy)")
    ap.add_argument("--boots", type=int, default=2)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    disk = os.path.join(args.out, "c-drive.img")
    shutil.copyfile(args.disk, disk)
    failed = 0
    for n in range(1, args.boots + 1):
        res, err = boot(args, n, disk)
        what = "first boot (blank C:)" if n == 1 else "boot %d (same disk)" % n
        if err:
            failed += 1
            print("FAIL %s: %s" % (what, err))
        else:
            print("PASS %s: home screen at %.0fs, widget boxes %.0f%%, softkey bar %.0f%%" %
                  (what, res["fg"], res["widgets"] * 100, res["softkeys"] * 100))
    print("first_boot: %s" % ("FAIL" if failed else "PASS"))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
