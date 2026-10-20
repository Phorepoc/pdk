# ph device input

Press keys, type, tap and swipe on the device.

## Synopsis

```
ph device input [-d <device>] [-token <token>] [-url <url>] <script>
```

## Description

Runs a script of input steps, separated by ';' or new lines: key <name>, text <string>, tap <x> <y>, long <x> <y>, swipe <x1> <y1> <x2> <y2>, wait <ms>, shot.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).

## Examples

Open the menu and the first application:

```sh
ph device input "key menu; wait 500; tap 60 120"
```

## Formerly

Still accepted for one release:

- `ph input`
