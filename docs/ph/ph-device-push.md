# ph device push

Install an image and boot it.

## Synopsis

```
ph device push [-d <device>] [-m <mode>] [-t <seconds>] [-token <token>] [-url <url>] [-w <regexp>] <image>
```

## Description

Uploads the image into the device's test slot, reboots into it, waits for the new boot (and for a log line, with -w), and prints the boot's report: faults, panics, test lines.

## Options

- `-d <device>`: The device id (default: the only device the service has).
- `-m <mode>`: The upload mode: auto, full or delta (default: auto).
- `-t <seconds>`: Wait at most this many seconds (default: 240).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).
- `-w <regexp>`: After the reboot, wait for a log line matching this regexp.

## Examples

Boot a new image and wait for the home screen:

```sh
ph device push build/rom/phone.img -w "Idle: ready"
```

## Formerly

Still accepted for one release:

- `ph push`
