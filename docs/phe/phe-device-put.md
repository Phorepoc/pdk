# phe device put

Copy a file to the device.

## Synopsis

```
phe device put [-d <device>] [-token <token>] [-url <url>] <file> <device-path>
```

## Description

Uploads a local file through the service. The device's file service decides where writing is allowed.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Send a test file:

```sh
phe device put hello.txt E:/hello.txt
```

