# ph image pinephone

Build the PinePhone image (kernel + ROM, for U-Boot).

## Synopsis

```
ph image pinephone [-b <board>]
```

## Description

Builds the PinePhone image from the ARM components ("ph component build -A -arch arm -b pinephone" first): the core images the kernel boots, the kernel with the A64 drivers linked in, the bootstrap and the ROM from the shared recipe, packaged for U-Boot: <build>/pinephone/symbian.img and boot.scr.

The SD card to write with balenaEtcher is made from them on the host (it needs docker): O=<build>/pinephone RELEASE=1 bash tools/phorepoc-arm/make_pinephone_sd.sh, run in adaptation-pinephone's os/.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

The image for the ARM components just built:

```sh
ph image pinephone -b pinephone
```

