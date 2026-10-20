# ph device diff

Show the log lines one boot has and another has not.

## Synopsis

```
ph device diff [-d <device>] [-token <token>] [-url <url>] <boot> [<boot>]
```

## Description

Compares two boots' logs (the second defaults to the current boot): what changed after a push.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

What the current boot does differently from boot 1094:

```sh
ph device diff 1094
```

## Formerly

Still accepted for one release:

- `ph diff`
