# phe source fetch

Fetch the pinned Symbian source.

## Synopsis

```
phe source fetch [<package>...]
```

## Description

Clones or updates the Symbian packages core's sources.lock pins, at those commits, into <core>/symbian (symbian-2010/ and symbian-rcl3/). Without operands, every package in the lock. Runs core's fetch.sh on the host, not in the container.

## Examples

Fetch everything the build needs:

```sh
phe source fetch
```

## Formerly

Still accepted for one release:

- `phe fetch`
