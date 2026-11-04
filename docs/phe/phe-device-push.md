# phe device push

Install an image and boot it.

## Synopsis

```
phe device push [-Bs] [-d <device>] [-m <mode>] [-t <seconds>] [-token <token>] [-url <url>] [-w <regexp>] <image>
```

## Description

Uploads the image into the device's test slot, reboots into it, waits for the new boot (and for a log line, with -w), and prints the boot's report: faults, panics, test lines. The image is a local file, uploaded to the service first, so it works against a service on another machine or in Docker.

## Options

- `-B`: The operand is the sha256 of an image already uploaded ("device upload").
- `-d <device>`: The device id (default: the only device the service has).
- `-m <mode>`: The upload mode: auto, full or delta (default: auto).
- `-s`: The operand is a path on the service's disk (default: a local file, uploaded first).
- `-t <seconds>`: Wait at most this many seconds (default: 240).
- `-token <token>`: The service's bearer token (default: $PHONELAB_TOKEN).
- `-url <url>`: The PhoneLab service url (default: $PHONELAB_URL, else http://localhost:8737).
- `-w <regexp>`: After the reboot, wait for a log line matching this regexp.

## Examples

Boot a new image and wait for the home screen:

```sh
phe device push build/rom/phone.img -w "Idle: ready"
```

## Formerly

Still accepted for one release:

- `phe push`
