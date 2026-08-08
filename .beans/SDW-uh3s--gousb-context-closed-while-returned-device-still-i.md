---
# SDW-uh3s
title: gousb Context closed while returned device still in use
status: completed
type: bug
priority: normal
created_at: 2026-08-08T10:05:24Z
updated_at: 2026-08-08T15:20:31Z
parent: SDW-xlg1
---

`New`/`NewWithSerial` (sdwire.go:167–168) `defer ctx.Close()` the `gousb.Context` and then return an `*SDWire` that keeps using a device opened from that context — use-after-close of libusb state. Non-matching devices are also left half-managed on some paths.

Fix: the `SDWire` owns its context; `Close()` closes the device then the context. Audit `ListDevices` for the same pattern (it closes everything it opened, but reads descriptors after open errors in loops — verify).

Small and self-contained: a good candidate to also prepare as an upstream patch to `fcjr/sdwire` for JP to submit if desired (never PR it ourselves).

- [x] Context lifetime owned by `SDWire`, closed in `Close()`
- [x] Error-path device closing audited
- [x] Regression covered by a test where feasible (may need a fake/injected context boundary) — direct test infeasible without faking libusb; all connect paths now funnel through one connect() helper whose selection logic is unit-tested, and context ownership is structural there

## Summary of Changes

SDWire now owns its gousb.Context: connect() (shared by New/NewWithSerial/NewWithIdentity) opens the context, closes every non-matching device, and on any error path closes all devices and the context; Close() closes device then context (errors.Join). ListDevices closes opened devices even when OpenDevices also returns an error (gousb can return both).

### Upstream patch essence (for JP to submit to fcjr/sdwire if desired — never PR ourselves)

The fix is entangled with the jphastings fork's reorg, but the minimal upstream diff is: add ctx *gousb.Context to the SDWire struct; in New/NewWithSerial remove defer ctx.Close(), store ctx in the returned SDWire (closing it only on error-return paths); in Close() call s.device.Close() then s.ctx.Close(). Plus: in ListDevices, close devs returned alongside a non-nil OpenDevices error.
