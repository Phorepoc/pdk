# phe image zdata

Collect the Z: data the packages export.

## Synopsis

```
phe image zdata [-b <board>]
```

## Description

Gathers the data files (configuration, databases, localisation) the packages' bld.inf files export to the ROM drive Z:, into <build>/zdata-gen.

## Options

- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Refresh the Z: data:

```sh
phe image zdata
```

## Formerly

Still accepted for one release:

- `phe zdata`
