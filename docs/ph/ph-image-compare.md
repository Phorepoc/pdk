# ph image compare

Byte-compare images with another build.

## Synopsis

```
ph image compare [-b <board>] [-ref <directory>] [<a> <b>]
```

## Description

Without operands, compares the images built here with a reference build row by row (tests/compare_*.tsv): IDENTICAL, or DIFFERENT with both sizes and the first differing offsets. With two operands, compares two files or two directories. Exits non-zero when anything differs. This is the check every port of a build step to Go must pass.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-ref <directory>`: The reference build's directory (default: the OS tree).

## Examples

Two image folders:

```sh
ph image compare build/x32img ../ref/x32img
```

## Formerly

Still accepted for one release:

- `ph tests`
