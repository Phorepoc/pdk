# phe image addon

Pack built components as an add-on archive for an existing image build.

## Synopsis

```
phe image addon [-arch <architecture>] [-b <board>] [-name <name>] [-o <file>] <component>...
```

## Description

Packs the components' built files -- the binary, its registration, the registry's rom_data -- into <name>-<arch>-addon.tar.gz, laid out by image path (sys/bin/..., resource/..., private/...) with a manifest (ADDON.txt: the architecture and the pdk and core commits it was built against). An image build adds it with "phe image rom -O addon=<archive>"; the build must be of the same architecture. The first component names the archive unless -name is given.

## Options

- `-arch <architecture>`: The architecture of the images to pack: x32 or arm (default: x32).
- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-name <name>`: The add-on's name (default: the first component).
- `-o <file>`: The archive file, in the build (default: <build>/addon/<name>-<arch>-addon.tar.gz).

## Examples

Pack Catalogue for ARM images:

```sh
phe image addon -arch arm catalogue
```

