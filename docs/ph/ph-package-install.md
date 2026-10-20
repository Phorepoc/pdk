# ph package install

Install an item on a device through PhoneLab.

## Synopsis

```
ph package install [-no-default] [-d <device>] [-drive <drive>] [-index <source>] [-o <directory>] [-token <token>] [-url <url>] [-wait <seconds>] <id>[@<tag>]
```

## Description

Fetches the release as "package fetch" does (SHA-256 checked), uploads it to the device's C:\Data\Installs, and runs pheinstall in a device shell (the phetty daemon), which installs it silently through the software installer. Waits for pheinstall's log line and reports the result.

Needs a device whose image carries pheinstall, phfs (files) and phettyd (shells).

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-drive <drive>`: Install to this drive (default: c).
- `-index <source>`: Read this index (a source: URL or file) before the default ones; repeatable.
- `-no-default`: Do not read the Phorepoc index.
- `-o <directory>`: The cache directory (as for fetch).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).
- `-wait <seconds>`: Give up after this many seconds (default: 180).

## Examples

Install the newest release:

```sh
ph package install hello
```

A release, to the memory card:

```sh
ph package install -drive e hello@v1.0
```

