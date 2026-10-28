# ph component list

List the registered components.

## Synopsis

```
ph component list [-H] [-b <board>] [-o <fields>] [<component>...]
```

## Description

One line per component of the registry (core's plus the adaptations'), or of the named ones. The fields: name, the repository that registers it (core or an adaptation), its kind (component, test, ssm = only in a System State Manager boot, nga = only with the NGA window server) and its .mmp.

## Options

- `-H`: Script mode: no header, fields separated by a tab.
- `-b <board>`: Build for this board ("ph board list"): core plus the board's adaptation repositories. Without it, the adaptations in PHOREPOC_ADAPTATIONS.
- `-o <fields>`: The fields to show, comma-separated: name, repo, kind, mmp (default: name,repo,kind,mmp).

## Examples

Every component a board's build knows:

```sh
ph component list -b myboard
```

Only the names, for a script:

```sh
ph component list -H -o name
```

