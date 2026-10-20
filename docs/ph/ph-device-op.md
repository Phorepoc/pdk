# ph device op

Run any device operation.

## Synopsis

```
ph device op [-d <device>] [-token <token>] [-url <url>] <operation> [<key>=<value>...]
```

## Description

Runs one operation ("ph device ops" lists them) with its arguments. Values that look like numbers or true/false are sent typed; 0x... stays a string (the service parses addresses).

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Read a register:

```sh
ph device op peek addr=0x01c20000 n=4
```

## Formerly

Still accepted for one release:

- `ph op`
