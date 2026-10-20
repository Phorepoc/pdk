# ph component resources

Compile components' resources (.rsc) and their headers (.rsg).

## Synopsis

```
ph component resources [-A] [-b <board>] <component>...
```

## Description

Runs the release's own resource compiler (rcomp) over each component's resource files, preprocessed as abld did. The .rsg headers go into the include tree, where other components include them: compile every component's resources (-A) before a full build.

## Options

- `-A`: Every registered component.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Every resource, before a clean build:

```sh
ph component resources -A
```

One component's after a change:

```sh
ph component resources avkon
```

## Formerly

Still accepted for one release:

- `ph rsc`
