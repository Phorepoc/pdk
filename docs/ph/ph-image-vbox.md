# ph image vbox

Package the ISO as a VirtualBox appliance.

## Synopsis

```
ph image vbox [-b <board>] [-c <CPUs>] [-d <MB>] [-m <MB>] [-n <name>] [-o <file>]
```

## Description

Writes <build>/x86/<name>.ova from the ISO "ph image kernel" made: a PC with the ISO in its DVD drive and a blank, formatted C: disk (a streamOptimized VMDK on the IDE primary master), an Intel PRO/1000 network adapter on NAT and AC'97 audio. It needs neither VirtualBox nor root: the disk is converted with qemu-img and the archive is a tar, the OVF descriptor first.

Import it with VirtualBox (File, Import Appliance) and start it: the ISO comes in its DVD drive, which boots first (the descriptor carries VirtualBox's own machine section: VMSVGA display, NAT, AC'97, the clock in UTC). The kernel's SVGA driver sets the phone-shaped 720x1280 screen by itself.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-c <CPUs>`: Number of virtual CPUs (default: 2).
- `-d <MB>`: Size of the C: disk in MB (1 GiB, as the PinePhone's C:) (default: 1024).
- `-m <MB>`: MB of RAM (the ROM lives in RAM) (default: 1024).
- `-n <name>`: The virtual machine's name, and the file's (default: Phorepoc).
- `-o <file>`: Write the appliance to this file (in the container: under /build).

## Examples

The appliance for the last kernel:

```sh
ph image vbox -b pc
```

A 2 GB RAM machine with a 1 GB C: disk:

```sh
ph image vbox -b pc -m 2048 -d 1024
```

