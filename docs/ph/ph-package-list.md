# ph package list

List the installable items of the indexes.

## Synopsis

```
ph package list [-no-default] [-a <architecture>] [-b <board>] [-c <category>] [-index <source>] [<word>...]
```

## Description

One line per item of the indexes: id, category, the newest approved release, the architectures and a summary. Words filter on the id, name and summary.

The indexes are read in order: those given with -index, then $PHOREPOC_INDEXES, then the Phorepoc index (https://phorepoc.github.io/index/index.json); an id listed by an earlier index wins.

## Options

- `-a <architecture>`: Only items for this architecture: arm or x32.
- `-b <board>`: Only items for this board.
- `-c <category>`: Only this category: app, game, driver, library, codec, theme, tool, system.
- `-index <source>`: Read this index (a source: URL or file) before the default ones; repeatable.
- `-no-default`: Do not read the Phorepoc index.

## Examples

Everything for the PinePhone:

```sh
ph package list -a arm -b pinephone
```

Games, from a second index too:

```sh
ph package list -c game -index https://example.org/index.json
```

