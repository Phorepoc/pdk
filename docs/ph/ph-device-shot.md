# ph device shot

Take a screenshot.

## Synopsis

```
ph device shot [-d <device>] [-o <file>] [-token <token>] [-url <url>] [-w <layer>]
```

## Description

Saves the device's screen as PNG. -w picks the layer: 0 = what the OS draws, 1 = the panel layer, 2 = both as seen.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-o <file>`: The output file (default: shot.png).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).
- `-w <layer>`: The layer: 0, 1 or 2 (default: 2).

## Examples

The OS screen into home.png:

```sh
ph device shot -w 0 -o home.png
```

## Formerly

Still accepted for one release:

- `ph shot`
