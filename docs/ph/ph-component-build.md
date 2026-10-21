# ph component build

Build components into images.

## Synopsis

```
ph component build [-Ar] [-arch <architecture>] [-b <board>] [<component>...]
```

## Description

Compiles each component from its real .mmp with the modern toolchain and writes its E32Image: x32 images into <build>/x32img, ARM images into <build>/arm.

-r also builds everything the named components link against, providers first. -A builds every registered component, providers first (ARM: every component, in registry order); a component that fails is reported and the build goes on. Run "ph component resources" first: components include the .rsg headers it generates.

## Options

- `-A`: Every registered component.
- `-arch <architecture>`: The architecture: x32 (amd64 CPU, 32-bit pointers) or arm (default: x32).
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-r`: Recursive: also everything the components link against, providers first.

## Examples

Build two components for the PC:

```sh
ph component build -b pc estor bafl
```

A component and everything below it:

```sh
ph component build -r avkon
```

Everything (a clean build):

```sh
ph component build -A -b pc
```

An ARM image for the PinePhone:

```sh
ph component build -b pinephone -arch arm phetty
```

## Formerly

Still accepted for one release:

- `ph build`
- `ph closure  (= ph component build -r)`
- `ph all  (= ph component build -A)`
- `ph arm  (= ph component build -arch arm)`
