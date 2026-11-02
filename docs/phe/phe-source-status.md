# phe source status

Show where the source, the OS tree, the adaptations and the build output are.

## Synopsis

```
phe source status [-H] [-b <board>]
```

## Description

Prints the locations phe uses (from the PHOREPOC_* settings, "phe help environment") and whether each exists.

## Options

- `-H`: Script mode: no header, fields separated by a tab.
- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Where a board build would look:

```sh
phe source status -b myboard
```

