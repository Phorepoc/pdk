# phe device rm

Delete a file on the device.

## Synopsis

```
phe device rm [-d <device>] [-token <token>] [-url <url>] <device-path>
```

## Description

Deletes one file through the service (only where the device's file service allows).

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Remove a test file:

```sh
phe device rm E:/hello.txt
```

