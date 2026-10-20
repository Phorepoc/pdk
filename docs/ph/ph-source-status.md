# ph source status

Show where the source, the OS tree, the adaptations and the build output are.

## Synopsis

```
ph source status [-H] [-b <board>]
```

## Description

Prints the locations ph uses (from the PHOREPOC_* settings, "ph help environment") and whether each exists.

## Options

- `-H`: Script mode: no header, fields separated by a tab.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Where a PinePhone build would look:

```sh
ph source status -b pinephone
```

