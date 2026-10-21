# ph image kernel

Build the x86-64 kernel and its bootable ISO.

## Synopsis

```
ph image kernel [-O <option>]... [-b <board>]
```

## Description

Builds the kernel for the PC board from the Symbian source (nkernSMP, ekern and the flexible memory model, with the x86-64 adaptation's backend and boot stub), links it with the ROM that "ph image rom -b pc" made (<build>/rom/romsect.bin), and wraps both in a GRUB ISO: <build>/x86/ekern/phorepoc-ekern.iso. Boots in QEMU, VirtualBox or on a PC; the kernel image is about the size of the ROM, so give the machine at least 512 MB.

Settings of the kernel (trace, screen, scale, dbg, kclean) are given with -O name=value ("ph help image-options" lists them).

## Options

- `-O <option>`: Set an image option, name=value. Can be given more than once.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

The kernel and ISO for the ROM just built:

```sh
ph image kernel -b pc
```

A kernel that traces its boot:

```sh
ph image kernel -b pc -O trace=0x80000002
```

