# phe device list

List the service's devices.

## Synopsis

```
phe device list [-d <device>] [-token <token>] [-url <url>]
```

## Description

Every device the PhoneLab service has: id, driver, link state, current boot.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Which devices does the service have?

```sh
phe device list
```

