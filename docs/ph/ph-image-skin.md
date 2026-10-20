# ph image skin

Build the default skin package.

## Synopsis

```
ph image skin [-b <board>]
```

## Description

Compiles the S60 default skin from its descriptor with the release's skin compiler: series60skin.skn plus its icon file series60skin.mif (the SVG artwork encoded as the platform's NVG and SVG-T binary formats), into <build>/skin-gen.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Rebuild the skin:

```sh
ph image skin
```

## Formerly

Still accepted for one release:

- `ph skin`
