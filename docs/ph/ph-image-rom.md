# ph image rom

Build the ROM.

## Synopsis

```
ph image rom [-O <option>]... [-b <board>] [-s <boot>]
```

## Description

Builds the core images (euser, efsrv, hal, efile, estart), stages every built component with its resources, icons, central repository files and data, and lays them out with rombuild into <build>/rom.

The boot (-s): ssm is the phone's real start-up through the System State Manager; bootui is the older bring-up starter; any other name is an application bootui starts in the foreground.

Settings of the recipe are given with -O name=value ("ph help image-options" lists them).

## Options

- `-O <option>`: Set an image option, name=value. Can be given more than once.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-s <boot>`: The boot: ssm, bootui, or an application name (default: ssm).

## Examples

A board's image, as the device boots:

```sh
ph image rom -b myboard
```

With networking, keeping the core images of the last build:

```sh
ph image rom -b myboard -O net=1 -O fast=1
```

Booting straight into Notes for a test:

```sh
ph image rom -b myboard -s notes
```

## Formerly

Still accepted for one release:

- `ph rom`
