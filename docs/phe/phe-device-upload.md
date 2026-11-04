# phe device upload

Store a file in the service's blob store.

## Synopsis

```
phe device upload [-d <device>] [-k <kind>] [-token <token>] [-url <url>] <file>
```

## Description

Uploads a local file and prints its blob record (sha256). "device push -B <sha256>" boots an uploaded image.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-k <kind>`: The blob kind (default: image).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Upload an image to a remote service:

```sh
phe device upload build/rom/phone.img
```

