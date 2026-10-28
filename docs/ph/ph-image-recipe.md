# ph image recipe

Run an image recipe of the board's repositories.

## Synopsis

```
ph image recipe [-O <option>]... [-b <board>] <name> [<arg>...]
```

## Description

Runs recipes/<name>.sh from the board's repositories, in the toolchain container: the board first, then its SoC and layers (docs/layering.md). Boards and SoC adaptations bring their own image steps this way -- a kernel, a disk image, an appliance -- and "ph board info" lists them.

Settings of the recipes are given with -O name=value ("ph help image-options" lists them).

## Options

- `-O <option>`: Set an image option, name=value. Can be given more than once.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

The board's kernel and boot image, after "ph image rom":

```sh
ph image recipe kernel -b <board>
```

