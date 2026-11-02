# phe image icons

Build every component's icon files (.mif).

## Synopsis

```
phe image icons [-a] [-b <board>]
```

## Description

Runs mifconv for each icon file the registered components declare in their bld.inf (and the extra folders core's registry/mif_dirs.txt lists), into <build>/icon-gen. An icon whose artwork is not in the open release gets a transparent placeholder. Needs the include tree ("phe source export").

## Options

- `-a`: Rebuild every icon file, not only the missing ones.
- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Only the icon files not built yet:

```sh
phe image icons
```

All of them again:

```sh
phe image icons -a
```

## Formerly

Still accepted for one release:

- `phe icons`
