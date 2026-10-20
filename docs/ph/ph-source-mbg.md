# ph source mbg

Generate the icon id headers (.mbg).

## Synopsis

```
ph source mbg [-a] [-b <board>] [<package>...]
```

## Description

Components include <x.mbg>, the enumeration of the icons in x.mif, which the S60 build generated from each bld.inf's mifconv block. This writes those headers into the include tree. Run it after an export and before building components.

## Options

- `-a`: Every package of both source trees.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

All icon headers, for a clean build:

```sh
ph source mbg -a
```

## Formerly

Still accepted for one release:

- `ph mbg`
