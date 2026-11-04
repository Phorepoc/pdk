# phe device boots

List the device's recent boots.

## Synopsis

```
phe device boots [-d <device>] [-n <boots>] [-token <token>] [-url <url>]
```

## Description

Recent boots, newest first: id, start time, slot, outcome, number of lines and faults.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-n <boots>`: At most this many boots (default: 20).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The last five boots:

```sh
phe device boots -n 5
```

