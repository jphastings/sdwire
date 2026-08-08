---
# SDW-xlg1
title: Reorganise package; per-device PowerFunc hooks and PowerCycle
status: todo
type: feature
priority: normal
created_at: 2026-08-08T10:05:23Z
updated_at: 2026-08-08T10:05:58Z
parent: SDW-1p8o
---

Reorganise the (currently single-file) package for clarity and extend it so each SDWire device can carry a custom target-power function that helpers invoke automatically.

## Power hooks

- `type PowerFunc func(shouldBeOn bool) error` — supplied per device: `dev.SetTargetPower(fn)` and/or a `WithTargetPower(fn)` option at construction. `nil` → helpers skip power steps.
- `(*SDWire).PowerCycle(minOff time.Duration)` helper: off → guaranteed dark time → on. Default minimum dark time ≥8 s (small-board PSUs ride through ~2 s cuts on output capacitance — measured on the bench; make it configurable, never shorter-by-default).
- Helpers that use the hook: PowerCycle, the flash-cycle helper (separate bean), and any future boot/reboot conveniences.

## Proposed layout (guide, not gospel — keep it as small as clarity allows)

- `sdwire.go` — public types (`SDWire`, `DeviceInfo`, `SwitchMode`, `PowerFunc`), discovery (`ListDevices`, `New`, `NewWithSerial`/`NewWithIdentity`)
- `controller_sdwirec.go` / `controller_sdwire3.go` — the two `DeviceController`s
- `hubpower/` — hub port-power primitives from the VBUS bean (Set/ClearPortFeature, GetPortStatus, parent-hub resolution, cache)
- `power/` — just the `PowerFunc` contract docs; vendor plugins as subpackages (`power/meross`, …)
- `internal/blockdev/` — per-OS block-device location (used by the flash helper)
- `cmd/sdwire/` — the CLI (separate bean)
- Unit tests for the pure logic (path/port math, hub characteristics parsing, config); hardware paths stay thin and obviously-correct.

## Fix while in here (has its own bug beans; may be subsumed)

- `New`/`NewWithSerial` close the `gousb.Context` (deferred) while the returned device is still in use — the `SDWire` must own the context and close it in `Close()`.
- Device identity must include bus + port path, not just serial (all Realtek SDWire3s share serial `20120501030900000`; the Python CLI appends the port path for this reason).

## Acceptance

- [ ] `PowerFunc` settable per device; helpers call it automatically; nil is a clean no-op
- [ ] `PowerCycle` enforces minimum dark time (default ≥8 s)
- [ ] Package split lands with godoc-clean public API; existing behaviour (SDWireC switching, listing) unchanged
- [ ] Both latent bugs fixed with tests where feasible
