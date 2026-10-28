# ph image release

Make the board's release files.

## Synopsis

```
ph image release [-b <board>] [-o <folder>]
```

## Description

Runs the board's RELEASE recipe (board.conf) on this machine, after every component is built: it makes the images to publish into <out> (default <build>/release). The recipe may start containers and call ph itself; CI runs the same command.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-o <folder>`: The folder for the release files (default <build>/release).

## Examples

Everything a release of the board publishes:

```sh
ph image release -b <board>
```

