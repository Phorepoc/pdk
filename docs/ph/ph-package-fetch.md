# ph package fetch

Download approved releases into the package cache.

## Synopsis

```
ph package fetch [-no-default] [-index <source>] [-o <directory>] <id>[@<tag>]...
```

## Description

Downloads each item's newest approved release (or the tagged one) into the package cache and checks its SHA-256 against the index; a file that does not match is not kept. A file already in the cache with the right hash counts as downloaded, so a cache prefilled from an SD card or USB stick installs offline.

The cache: -o, else $PHOREPOC_PACKAGE_CACHE, else the user's cache directory (phorepoc/packages). Prints each file's path.

## Options

- `-index <source>`: Read this index (a source: URL or file) before the default ones; repeatable.
- `-no-default`: Do not read the Phorepoc index.
- `-o <directory>`: The cache directory.

## Examples

The newest codec pack:

```sh
ph package fetch codecpack
```

A particular release, into a folder:

```sh
ph package fetch -o sd/packages tls@v1.0
```

