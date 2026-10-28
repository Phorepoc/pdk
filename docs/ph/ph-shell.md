# ph shell

Open a shell in the toolchain container.

## Synopsis

```
ph shell [-b <board>]
```

## Description

An interactive shell in the toolchain image, with the same mounts and settings as every build command: the pdk at /pdk, core at /core, the source at /symbian, the build output at /build, the adaptations at /adapt/<n>.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Look around a board build:

```sh
ph shell -b myboard
```

