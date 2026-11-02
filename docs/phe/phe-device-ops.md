# phe device ops

List the device's operations.

## Synopsis

```
phe device ops [-d <device>] [-token <token>] [-url <url>]
```

## Description

Every operation the device's driver offers ("phe device op" runs one), with its capability and description.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

What can the phone do?

```sh
phe device ops
```

## Formerly

Still accepted for one release:

- `phe ops`
