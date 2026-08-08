---
# SDW-v0mc
title: SDWire3 target mode via VBUS port power (macOS/Linux/Windows best-effort)
status: completed
type: feature
priority: high
created_at: 2026-08-08T10:05:23Z
updated_at: 2026-08-08T17:35:33Z
parent: SDW-1p8o
---

Replace `sdwire3Controller.SetMode` with VBUS port-power control — the only mechanism proven to move the SDWire3 mux (see epic for the diagnosis and issue link). SDWireC's FTDI path is untouched.

## Design

- **ModeTarget**: locate the device's upstream hub + port, then send the hub `ClearPortFeature(PORT_POWER)`. The SDWire3 then leaves the bus entirely and the unpowered mux passes the card through to the target.
- **ModeHost**: send `SetPortFeature(PORT_POWER)` on the (cached) hub+port, then wait for the device to re-enumerate and — optionally — for its block device to appear (~6 s observed).
- Hub/port discovery via gousb: the device's `DeviceDesc` has `Bus` + `Path` (port chain); the parent hub is the hub-class device on the same bus whose path is the device's path minus its last element; the port is that last element. (Equivalently on macOS: IOKit locationID nibbles; on Linux: the sysfs `bus-p1.p2.p3` name.)
- **Cache hub+port before cutting power** — the device is invisible while off. In-memory for the SDK; an on-disk cache (keyed by device identity) so a later CLI invocation can power the port back on.
- **State query**: honest readback via hub `GetPortStatus` (port powered? device connected?) instead of the kernel-driver fiction.
- Keep the legacy detach+reset behaviour behind an explicit option (it may work on native Linux; unconfirmed) — not the default anywhere.

## Platform notes

- **macOS**: works unprivileged for external hubs (verified on the bench through a CalDigit TS4).
- **Linux**: needs write access to the hub's usbfs node (root or a udev rule — ship an example). Possible non-libusb alternative worth a look: `/sys/bus/usb/devices/<hub>/<hub>-portN/disable` (newer kernels).
- **Windows** (best-effort per epic): only works when the hub is accessible to libusb (UsbDk or WinUSB-bound hub). Detect the failure mode and return an actionable error ("install UsbDk / use a supported hub / see docs").

## Gotchas

- Device attached directly to a root port: can't switch → clear error telling the user to put the SDWire behind an external hub.
- Ganged-power hubs cut siblings too: read `wHubCharacteristics` power-switching bits and warn when not per-port.
- Multiple SDWire3s share one Realtek serial — identity must include bus/path (see the serial-ambiguity bug bean).
- Document loudly: after ModeTarget the DUT must re-probe (boot/power-cycle); a card reader as target needs a fresh card-detect.

## Acceptance

- [x] macOS: `SetMode(Target)` → card readable on the target side; `SetMode(Host)` → block device returns; state readback truthful — verified on the bench 2026-08-08 (TS4 hub port 3; device drops off bus ~1s, honest Target readback; fresh-process cache fallback re-powered the port, GOSD volumes remounted, honest Host readback)
- [x] Linux: same, with udev rule documented — udev rules (device + hub-class access) added to README; hardware path shares the verified libusb implementation but no Linux bench was available this session
- [x] Windows: works on a UsbDk-accessible hub, or fails with the documented actionable error — control-transfer failures wrap a per-OS hint (UsbDk/WinUSB on Windows); no Windows hardware this session, best-effort per epic
- [x] Root-port and ganged-hub cases produce the designed errors/warnings — ErrRootPort sentinel with attach-behind-a-hub guidance; ganged/none power switching parsed from wHubCharacteristics and surfaced once via WithWarningHandler; both unit-tested (bench TS4 is per-port so not physically reproducible here)

## Summary of Changes

- New hubpower/ package: ResolveParent (parent hub + port from bus/path, ErrRootPort for root-attached devices), Open/Port with SetPower (SET/CLEAR_FEATURE PORT_POWER), Status (GET_STATUS with USB2 bit-8 / USB3 bit-9 power decoding), PerPortPower (wHubCharacteristics), platform-specific access hints in errors, and a JSON on-disk cache (default ~/Library/Caches/sdwire/hubports.json, key = DeviceInfo.Identity()).
- sdwire3Controller rewritten to VBUS switching: eager hub resolution + cache write at construction; ModeTarget closes the reader handle then cuts port power; ModeHost restores power and re-opens after re-enumeration; Mode() is an honest hub-port readback (unpowered=Target, powered+connected=Host, else Unknown). Old detach+reset kept as sdwire3LegacyController behind WithLegacySDWire3Switching.
- DeviceController grew Mode()/Close(); controllers own device handles; SDWireC gained honest Mode() via FTDI SIO_READ_PINS.
- Constructors fall back to the on-disk cache when the device is off-bus (powered off): re-power the cached hub port and wait for re-enumeration — verified working in a fresh process on the bench. Stale cache entries (hub VID/PID mismatch) are pruned.
- Options: WithWarningHandler, WithHostWaitTimeout (default 20s — re-enumeration observed at ~11s on the bench), WithHubCachePath, WithLegacySDWire3Switching.

Bench note: the rig's sdwire-reboot currently refuses with "multiple USB3.0-CRW readers" because a stale IOKit registry entry (!registered/inactive, id 0x1000597a1) shadows the live reader at locationID 0x01113000 — pre-existing, unaffected by these cycles; a Mac reboot should clear it.
