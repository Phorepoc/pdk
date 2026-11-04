# phe device log

Show stored log lines.

## Synopsis

```
phe device log [-f] [-a <texts>] [-b <boot>] [-d <device>] [-g <text>] [-n <lines>] [-token <token>] [-url <url>] [-x <texts>]
```

## Description

The device's log as the service stored it: the newest lines, or those of one boot, optionally filtered.

## Options

- `-a <texts>`: Only lines containing one of these texts, separated by |.
- `-b <boot>`: Only this boot (default: all; -1 = the current boot).
- `-d <device>`: The device id (default: the only device the service has).
- `-f`: Keep running and print new lines as they arrive (until interrupted).
- `-g <text>`: Only lines containing this text (case-insensitive).
- `-n <lines>`: At most this many lines (default: 200).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).
- `-x <texts>`: Drop lines containing one of these texts, separated by |.

## Examples

The last panics of boot 1095:

```sh
phe device log -b 1095 -g panic
```

## Formerly

Still accepted for one release:

- `phe log`
