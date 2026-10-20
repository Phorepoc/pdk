# ph package info

Show an item: description, compatibility, approved releases.

## Synopsis

```
ph package info [-no-default] [-index <source>] <id>
```

## Description

Prints everything the index says about the item: name, licence, homepage, which devices it installs on, what it requires, its description, and every approved release with its file, size and SHA-256.

## Options

- `-index <source>`: Read this index (a source: URL or file) before the default ones; repeatable.
- `-no-default`: Do not read the Phorepoc index.

## Examples

About the codec pack:

```sh
ph package info codecpack
```

