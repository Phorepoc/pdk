# ph board list

List the boards ph builds for.

## Synopsis

```
ph board list [-H]
```

## Description

A board is core plus its adaptation repositories, checked out next to core. The STATE column says whether those checkouts are there.

## Options

- `-H`: Script mode: no header, fields separated by a tab.

## Examples

The boards and whether they can be built here:

```sh
ph board list
```

