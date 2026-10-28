# ph component check

Check that the registry's paths exist.

## Synopsis

```
ph component check [-b <board>]
```

## Description

Every .mmp, added source file and include path or forced include each registered component names (core's registry and the adaptations') must exist in the merged OS tree or the Symbian source. Lists the ones that do not; exits non-zero when there are any.

## Options

- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.

## Examples

Check a board's registry:

```sh
ph component check -b myboard
```

