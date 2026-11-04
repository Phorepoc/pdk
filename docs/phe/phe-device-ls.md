# phe device ls

List a directory on the device.

## Synopsis

```
phe device ls [-d <device>] [-token <token>] [-url <url>] [<path>]
```

## Description

The device's file service (the nokia driver, or a phlink image that offers fs_list). Default path: /.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The phone's C: drive:

```sh
phe device ls C:/
```

