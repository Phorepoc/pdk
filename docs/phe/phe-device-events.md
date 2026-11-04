# phe device events

Show the structured events parsed from the log.

## Synopsis

```
phe device events [-b <boot>] [-d <device>] [-k <kinds>] [-n <events>] [-token <token>] [-url <url>]
```

## Description

Boot starts, faults, panics, upload verdicts, link changes, monitor samples. -k picks kinds (comma list; a trailing dot is a prefix).

## Options

- `-b <boot>`: Only this boot (default: all).
- `-d <device>`: The device id (default: the only device the service has).
- `-k <kinds>`: Only these event kinds, e.g. fault,panic or mon.
- `-n <events>`: At most this many events (default: 100).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Faults and panics of boot 1095:

```sh
phe device events -k fault,panic -b 1095
```

