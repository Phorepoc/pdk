# ph device report

Show how a boot went.

## Synopsis

```
ph device report [-d <device>] [-s <file>] [-token <token>] [-url <url>] [<boot>]
```

## Description

A boot's faults, panics and test lines, with symbols for fault addresses when -s names a symbol file. Default: the current boot.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-s <file>`: A symbol file for fault addresses.
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The current boot:

```sh
ph device report
```

## Formerly

Still accepted for one release:

- `ph report`
