# ph device status

Show the service's and the devices' health.

## Synopsis

```
ph device status [-d <device>] [-token <token>] [-url <url>]
```

## Description

The PhoneLab service's state and, per device: the link, the current boot, the slot, CPU and free RAM.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Is the phone connected, and which boot is it on?

```sh
ph device status
```

## Formerly

Still accepted for one release:

- `ph health`
