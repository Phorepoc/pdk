# ph device log

Show stored log lines.

## Synopsis

```
ph device log [-b <boot>] [-d <device>] [-g <text>] [-n <lines>] [-token <token>] [-url <url>]
```

## Description

The device's log as the service stored it: the newest lines, or those of one boot, optionally filtered.

## Options

- `-b <boot>`: Only this boot (default: all).
- `-d <device>`: The device id (default: the only device the service has).
- `-g <text>`: Only lines containing this text (case-insensitive).
- `-n <lines>`: At most this many lines (default: 200).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The last panics of boot 1095:

```sh
ph device log -b 1095 -g panic
```

## Formerly

Still accepted for one release:

- `ph log`
