# phe device pin

Keep a boot or a blob past the retention period.

## Synopsis

```
phe device pin [-u] [-d <device>] [-token <token>] [-url <url>] boot|blob <id>
```

## Description

Pinned boots and blobs are not pruned. -u releases the pin. A boot is named by its number, a blob by its sha256.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-u`: Release the pin instead.
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Keep the good boot:

```sh
phe device pin boot 1095
```

