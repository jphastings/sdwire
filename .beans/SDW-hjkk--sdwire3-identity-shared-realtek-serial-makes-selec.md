---
# SDW-hjkk
title: 'SDWire3 identity: shared Realtek serial makes selection ambiguous'
status: todo
type: bug
priority: normal
created_at: 2026-08-08T10:05:24Z
updated_at: 2026-08-08T10:05:58Z
parent: SDW-xlg1
---

All Realtek-based SDWire3s report the same USB serial (`20120501030900000`), so `NewWithSerial` can silently pick the wrong device and `ListDevices` returns indistinguishable entries when more than one is attached. The Python CLI works around exactly this by appending the USB port path to the serial (`20120501030900000.1.1.3`).

Fix: make device identity serial **plus bus/port path** — expose location in `DeviceInfo`, add location-aware selection (`NewWithIdentity` or extend `NewWithSerial` to accept the suffixed form), and use the same identity as the key for the VBUS bean's hub/port cache and the CLI's config binding.

- [ ] `DeviceInfo` carries bus + port path (and a Python-compatible display string)
- [ ] Selection unambiguous with two identical-serial devices attached
- [ ] Shared identity used by hub-power cache and CLI config
