# phe device history

Show the operations run on the device.

## Synopsis

```
phe device history [-d <device>] [-n <operations>] [-token <token>] [-url <url>]
```

## Description

The service's record of every operation (by the CLI, the API, MCP or the web page): when, who, which, how long, and its error.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-n <operations>`: At most this many operations (default: 20).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

The last ten operations:

```sh
phe device history -n 10
```

