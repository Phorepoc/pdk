# phe device blobs

List the service's stored blobs.

## Synopsis

```
phe device blobs [-d <device>] [-k <kind>] [-n <blobs>] [-token <token>] [-url <url>]
```

## Description

Uploaded images and captured screenshots, by their sha256: what "device push" and "device shot" stored.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-k <kind>`: Only this kind (image, screenshot ...).
- `-n <blobs>`: At most this many blobs (default: 50).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The uploaded images:

```sh
phe device blobs -k image
```

