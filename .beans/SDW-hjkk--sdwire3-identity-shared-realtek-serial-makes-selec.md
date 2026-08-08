---
# SDW-hjkk
title: 'SDWire3 identity: shared Realtek serial makes selection ambiguous'
status: completed
type: bug
priority: normal
created_at: 2026-08-08T10:05:24Z
updated_at: 2026-08-08T19:19:30Z
parent: SDW-xlg1
---

All Realtek-based SDWire3s report the same USB serial (`20120501030900000`), so `NewWithSerial` can silently pick the wrong device and `ListDevices` returns indistinguishable entries when more than one is attached. The Python CLI works around exactly this by appending the USB port path to the serial (`20120501030900000.1.1.3`).

Fix: make device identity serial **plus bus/port path** — expose location in `DeviceInfo`, add location-aware selection (`NewWithIdentity` or extend `NewWithSerial` to accept the suffixed form), and use the same identity as the key for the VBUS bean's hub/port cache and the CLI's config binding.

- [x] `DeviceInfo` carries bus + port path (and a Python-compatible display string)
- [x] Selection unambiguous with two identical-serial devices attached (unit-tested; ambiguous plain serial errors listing candidate identities)
- [x] Shared identity used by hub-power cache and CLI config — the on-disk hub-port cache is keyed by Identity() (verified live: ~/Library/Caches/sdwire/hubports.json key 20120501030900000.1.1.3), and the CLI config's per-device serial/location fields select via the same matching rules

## Summary of Changes

DeviceInfo carries Bus/PortPath with Python-compatible Identity() and sysfs-style Location(); NewWithSerial accepts the suffixed form and errors (listing candidates) on ambiguous plain serials; NewWithIdentity adds location selection. The same identity keys the hubpower on-disk cache and the CLI's config binding. Selection logic unit-tested with two identical-serial devices.
