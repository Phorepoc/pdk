# ph component stale

List images linked before a provider of theirs existed.

## Synopsis

```
ph component stale [-b <board>]
```

## Description

An image whose imports went to stubs because a provider was not built yet has to be linked again once it is. This lists those images, with the providers now present.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

What needs a relink after a partial build:

```sh
ph component stale
```

## Formerly

Still accepted for one release:

- `ph stale`
