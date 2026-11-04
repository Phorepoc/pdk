# phe device pull

Copy a file from the device.

## Synopsis

```
phe device pull [-d <device>] [-token <token>] [-url <url>] <device-path> [<file>]
```

## Description

Downloads a file through the service. Default local name: the file's own name in the current directory.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Fetch a log file:

```sh
phe device pull C:/data/boot.log
```

