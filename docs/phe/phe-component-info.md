# phe component info

Show a component's registry entry.

## Synopsis

```
phe component info [-b <board>] <component>
```

## Description

Prints the component's whole registry entry: its .mmp and every build setting (dropped and added sources, defines, flags, ROM name ...).

## Options

- `-b <board>`: Build for this board ("phe board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

How Avkon is built:

```sh
phe component info avkon
```

