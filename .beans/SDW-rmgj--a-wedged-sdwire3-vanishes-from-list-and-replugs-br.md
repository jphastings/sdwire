---
# SDW-rmgj
title: A wedged SDWire3 vanishes from list, and replugs break the software recovery
status: completed
type: bug
priority: high
created_at: 2026-08-17T02:52:02Z
updated_at: 2026-08-17T03:08:13Z
---

When the SDWire3's reader wedges, `sdwire list` prints an empty table and
says nothing else — indistinguishable from "no SDWire is plugged in". The
tool holds everything needed to say what actually happened, and to fix it
without touching a cable, but exposes neither. The operator's workaround is
a physical replug, and *that* is what has quietly broken the software
recovery path over time.

## What the wedge looks like

Diagnosed on the bench on 2026-08-16, from the macOS system log. The same
sequence appears four times in one evening (19:31, 19:49, 20:11, 22:32),
each time in the middle of a sustained write:

```
USB3.0-CRW@01113000 endpoint 0x01: status 0xe0005000 (pipe stalled): 65536 bytes transferred
USB3.0-CRW@01113000 endpoint 0x01: status 0xe00002ed (transaction error): 0 bytes transferred
IOUSBMassStorageDriver: USB device 0BDA031601113000 - will be reset!
AppleUSB20HubPort::resetAndCreateDevice: reset did not enable port
AppleUSBHostPort::terminateDevice: destroying 0x0bda/0316/0204 (USB3.0-CRW): reset API call
```

Endpoint 0x01 is the reader's bulk-OUT pipe. The Realtek reader stops
answering mid-write, `IOUSBMassStorageDriver` requests a port reset, the
reset fails to re-enable the port, and IOKit destroys the device object.
Nothing is on the bus at that port afterwards and macOS does not re-probe
it. A hub port-status read afterwards showed `powered=false
connected=false`: VBUS was off too, so it could not have come back by
itself.

This is the reader's firmware latching up, not anything the SDK does. The
recovery is a real power removal — which is exactly what
`revivePortPower` in `fallback.go` already implements (5s dark, then on),
and exactly what macOS does for you on a physical replug
(`AppleUSBHubPort::cableChangeOccurred: powering on`). Restoring
PORT_POWER on the affected port alone brought the reader back in under a
second, with no cable touched.

## 1. `list` cannot report a device that isn't on the bus

`ListDevices` (sdwire.go) enumerates live USB devices and never consults
the hub-port cache, so three very different situations render identically
as an empty table:

- deliberately in target mode (port unpowered — the documented, healthy case)
- wedged and torn down by the OS
- genuinely unplugged

`state` already knows how to answer this for a powered-off device, via
`CachedPortState`. `list` should use the same source: show cached devices
that aren't currently enumerated, with their port state, rather than
omitting them. The block-device column already has a "None" convention for
"nothing to find"; a state column would carry the rest.

## 2. Every replug adds a cache entry, and enough of them break recovery

Because a physical replug tends to land in whatever socket is free, the
bench's `hubports.json` accumulated three entries for one serial:

```
20120501030900000.1.1.1.3
20120501030900000.1.1.3
20120501030900000.1.1.4
```

`selectCacheEntry` (fallback.go) tries `selectByIdentity` first and, on
failure, `selectBySerial` — but when the serial matches several entries it
keeps the *identity* error and discards the ambiguity. With a config
naming only `serial:`, `sdwire state` on a powered-off device therefore
reports:

```
Error: reading device state: no SDWire device matching location 20120501030900000 found: no matching SDWire device found
```

...which names neither the real problem (three cached ports for one
serial) nor the fix. `switch host` still recovers, but by brute-forcing
every matching entry in random map order, power-cycling unrelated hub
ports on the way and burning `hostWaitTimeout` per miss.

This is the caveat SDW-ox5r explicitly left undone. It is a feedback loop:
the replug workaround for problem (1) is what creates the ambiguity that
disables the software fix for it.

## 3. There is no way to ask for a power-cycle directly

The 5s-dark power cycle only happens as a side effect of `switch host`
finding a device it can identify. A wedged reader is precisely the case
where identification is least reliable, and where the operator knows
exactly which port they mean. A `revive` (or `recover`) command that takes
a location and power-cycles it — no unique cache hit required — would turn
"unplug and replug it" into one command.

## Suggested shape

- `list` gains a state column and includes cached-but-absent devices,
  reading their state the way `state` does. A wedged device then reads as
  present-but-unpowered rather than vanishing.
- When a serial matches several cache entries, say so — name the candidate
  identities and suggest deleting the cache file, the way the live-device
  ambiguity error already names its candidates.
- `sdwire revive [-s <selector>]`: power-cycle the named port with
  `readerRevivePause` of dark time and wait for re-enumeration. Accept a
  bare location so it works when the cache is ambiguous or empty.
- README: document the wedge signature above under troubleshooting, so the
  log lines are searchable, and say that `revive` is the software
  equivalent of a replug.

## Todo

- [x] `list`: include cached-but-not-enumerated devices, with port state
- [x] Report cache-serial ambiguity honestly instead of the identity error
- [x] `sdwire revive` command, working from a location without a unique cache hit
- [x] README troubleshooting entry for the reader-wedge log signature

## Summary of Changes

All four, verified against the bench hardware — including on a reader that
wedged for real, unprompted, part-way through the work.

**1. `list` reports what it knows, not just what enumerates.** New SDK
`ListDeviceStates`, which pairs USB enumeration with a read of every
hub-port cache entry that isn't currently producing a device. It never
powers a port on or off, so asking cannot move a card. An attached SDWire3
is in host mode by construction (target mode *is* being off the bus);
under `WithLegacySDWire3Switching`, whose mechanism leaves the device
enumerated either way, it honestly reports Unknown; an SDWireC is asked
directly over FTDI. Cache entries whose port has something connected are
skipped — by elimination that isn't an SDWire — as are entries whose hub
can no longer be opened.

`list` gains a `State` column, *appended* after the existing three so the
Python CLI's column positions still parse, and `--json` gains `state` and
`attached`. `Target` means the port is unpowered; `Unknown` means powered
with nothing on it, which is the wedge.

**2. A serial matching several cached ports says so.** `selectCacheEntry`
kept the identity lookup's "no SDWire device matching location <serial>"
even when the serial lookup had found the real problem. It now prefers the
ambiguity — which names each remembered port and the cache file to delete
— and falls back to the identity error only for a genuine miss. The
identity error is still right for a true not-found, so both paths keep
their best message.

**3. `sdwire revive`.** Cuts the port, holds it dark for
`readerRevivePause`, restores it, waits for re-enumeration, and re-caches
the port under whatever identity comes back. A location selector resolves
through live USB topology rather than the cache, which is what makes it
usable when the cache is empty, stale or ambiguous — the state a wedged
device tends to leave behind. Volumes mounted from the reader are
unmounted first, on the same terms as `SetMode(ModeTarget)`: nothing found
means nothing to lose and it proceeds, but a *failed* unmount aborts
rather than yanking a mounted filesystem.

**4. README.** The wedge's macOS log signature is written out verbatim
under troubleshooting so the lines are searchable, plus `revive`'s own
section, the `State` column semantics as a table, and an honest correction
to the migration section's "byte-for-byte" claim.

**Bench verification.** The reader wedged on its own at 03:55 with the
documented signature. `sdwire revive` recovered it in 6.5s from the config's
default device, and `sdwire revive -s 1-1.1.3` again by location in 15.6s
(unmounting `hello-boot` and `hello-data` first). `list` showed `Target`
while it was down and `Host` + `/dev/disk4` after — the empty table this
bean is about never appeared.

Not done: `list` run immediately after a revive can abort inside libusb's
darwin backend (`process_new_device` assertion, `cached_device->address <=
UINT8_MAX`) when it enumerates while the device is still coming up. It's an
upstream assert, so it kills the process rather than returning an error;
re-running works. Filed separately as SDW-q3z3.
