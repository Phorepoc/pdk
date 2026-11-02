# phe image sis

Package components as an installable SIS file.

## Synopsis

```
phe image sis [-arch <architecture>] [-b <board>] <component>...
```

## Description

Builds a SIS package (the Symbian installer format) from the components' images and resources, to install on a running device instead of rebuilding the ROM.

## Options

- `-arch <architecture>`: The architecture of the images to package: x32 or arm (default: x32).
- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Package the developer terminal:

```sh
phe image sis phettyd pheshell pheterm
```

An ARM package (for a phone):

```sh
phe image sis -arch arm pheffmpeg
```

## Formerly

Still accepted for one release:

- `phe sis`
