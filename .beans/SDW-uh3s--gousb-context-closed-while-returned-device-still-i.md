---
# SDW-uh3s
title: gousb Context closed while returned device still in use
status: todo
type: bug
priority: normal
created_at: 2026-08-08T10:05:24Z
updated_at: 2026-08-08T10:05:58Z
parent: SDW-xlg1
---

`New`/`NewWithSerial` (sdwire.go:167–168) `defer ctx.Close()` the `gousb.Context` and then return an `*SDWire` that keeps using a device opened from that context — use-after-close of libusb state. Non-matching devices are also left half-managed on some paths.

Fix: the `SDWire` owns its context; `Close()` closes the device then the context. Audit `ListDevices` for the same pattern (it closes everything it opened, but reads descriptors after open errors in loops — verify).

Small and self-contained: a good candidate to also prepare as an upstream patch to `fcjr/sdwire` for JP to submit if desired (never PR it ourselves).

- [ ] Context lifetime owned by `SDWire`, closed in `Close()`
- [ ] Error-path device closing audited
- [ ] Regression covered by a test where feasible (may need a fake/injected context boundary)
