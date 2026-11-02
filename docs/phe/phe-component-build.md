# phe component build

Build components into images.

## Synopsis

```
phe component build [-Ar] [-compile-only] [-arch <architecture>] [-b <board>] [-shard <i/n>] [<component>...]
```

## Description

Compiles each component from its real .mmp with the modern toolchain and writes its E32Image: x32 images into <build>/x32img, ARM images into <build>/arm.

-r also builds everything the named components link against, providers first. -A builds every registered component, providers first (ARM: every component, in registry order); a component that fails is reported and the build goes on. Run "phe component resources" first: components include the .rsg headers it generates.

## Options

- `-A`: Every registered component.
- `-arch <architecture>`: The architecture: x32 (amd64 CPU, 32-bit pointers) or arm (default: x32).
- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-compile-only`: Compile the objects, link nothing (the shards of a split build; a later -A links).
- `-r`: Recursive: also everything the components link against, providers first.
- `-shard <i/n>`: With -A: only every n-th component from the i-th (i/n, a CI matrix job's share).

## Examples

Build two components for a board:

```sh
phe component build -b myboard estor bafl
```

A component and everything below it:

```sh
phe component build -r avkon
```

Everything (a clean build):

```sh
phe component build -A -b myboard
```

An ARM image for a phone:

```sh
phe component build -b myboard -arch arm phetty
```

## Formerly

Still accepted for one release:

- `phe build`
- `phe closure  (= phe component build -r)`
- `phe all  (= phe component build -A)`
- `phe arm  (= phe component build -arch arm)`
