---
# SDW-oy4i
title: SDWire3 switching via USB suspend instead of hub port power
status: completed
type: feature
priority: high
created_at: 2026-11-05T18:41:56Z
updated_at: 2026-11-05T20:26:32Z
---

Upstream (Badger-Embedded/sdwire-cli 0.4.0 `a10acbf`, 0.5.0 `b8e3df6`) found the real mechanism: the RTS5306E's suspend output, meant to cut SD-card power, drives the mux, so the card goes to the target whenever the reader is **USB-suspended**. Cutting VBUS (SDW-v0mc) only worked because an unpowered reader holds the same pin state. Upstream only implemented Linux (sysfs runtime PM); on macOS it still runs the old detach+reset, which never worked.

Switch modes by suspending the reader instead, so no hub power switching (and no external hub at all) is needed to switch. Hub port power stays only as optional crash recovery (`revive`, flash resume) for a reader that latches up mid-write (SDW-rmgj), when a power-switching hub happens to be present.

## Bench evidence (2026-11-05, macOS 26, SDWire3 on root port `usb-drd1-port-hs@01100000`, no hub)

Scratch IOKit probe (IOUSBDeviceInterface650), all unprivileged:

- `USBDeviceOpen` succeeds (exclusive device open, no root).
- `USBDeviceSuspend(true)` with the device configured returns `kIOReturnAborted` (0xe00002eb): the mass-storage interface driver vetoes it.
- `SetConfiguration(0)` succeeds and tears down the whole mass-storage stack; `USBDeviceSuspend(true)` then succeeds. IORegistry `IOPowerManagement.CurrentPowerState` goes 2 → 1 and **stays 1 after the process closes the device and exits**.
- Mux readback by the board's own status LED: RGB **solid bright red** in host mode, **dim green** while suspended; PWR red in both (suspend cuts card power, not controller power).
- `USBDeviceReEnumerate(0)` resumes, reconfigures and rematches the mass-storage driver; the card's block device is back in ~3–4 s.
- A libusb open (gousb `OpenDevices`) does **not** wake a suspended reader; reading a string descriptor (`SerialNumber()`) **does** (1 → 2), leaving it awake and unconfigured with no block device. Every read-only path must take serial/product/manufacturer from the IORegistry (`USB Serial Number`, `kUSBProductString`, `kUSBVendorString`) instead.

Implementation bench run (same rig, `sdwire` built from this change):

- `switch target` → CurrentPowerState 1, `kUSBCurrentConfiguration` 0, disk gone. `list` (table and `--json`), `state`, `state --json`, `-s 1-1 state` and a repeated `switch target` all leave it at 1, reporting `Target` with serial and product read from the IORegistry.
- **Resume timing matters.** `USBDeviceReEnumerate` straight out of suspend, and resume signalling (`USBDeviceSuspend(false)`) followed by a re-enumerate 0.5 s later, both bring the reader back configured but reporting no card (SCSI sense 02/3A/00, medium not present); the first such resume also hit bulk-IN timeouts and a mass-storage-driver reset. Waking it with resume signalling and re-enumerating 3 s later brings the card back, and the block device appears ~1 s after `switch host` returns (~4 s in all), automounted. That also recovered the no-card state left by the earlier attempts.
- Unrelated apps (Chrome, Spotify) and libusb's own arrival handling in any process holding a gousb context open the reader exclusively for a moment as it re-enumerates. It did not stop the 3 s sequence working.

Target boot (same rig, Radxa ROCK 4SE with a spindle-gosd card, console at 1.5 Mbaud): after `switch target` the reader stayed at CurrentPowerState 1 through `list`, `state` and three Meross power cycles. U-Boot SPL booted `from MMC2` and loaded extlinux, kernel and initramfs from `mmc@fe320000` (the RK3399 SD controller); gosd-init mounted `/dev/mmcblk1p1` and `mmcblk1p2` read-write. The spindle app then halted on its own `SCRATCH-DISK-HAS-DATA` policy, which is unrelated to the mux. After `power off` and `switch host` the card was back on the Mac in ~5 s, carrying the `LAST_FATAL_ERROR.md` the target had written to it.

Not verified: Linux (no Linux bench; mechanism ported from upstream, which verified it on hardware), and revive/flash-resume behind a hub.

Watch for: a card that is only re-enumerated, without the resume signalling and 3 s settle, can come back as "medium not present". This happened on this rig at cold plug-in and after a card swap, and it persisted across a physical replug until the settled sequence ran.

## Todo

- [x] `internal/usbsuspend`: Suspend / Resume / StateOf / Strings keyed by bus + port path (darwin IOKit via cgo, Linux sysfs + usbfs, ErrUnsupported elsewhere)
- [x] SDWire3 controller switches by suspend where supported, holding no reader handle; an unresolvable hub only limits `Revive`
- [x] Discovery, `list`, `state` and `connect` never wake a suspended reader (registry/sysfs strings; SDWire3 not opened)
- [x] Delete `WithLegacySDWire3Switching` and its controller
- [x] README, repo skill and JP's bench skill copy describe suspend switching, with hub port power as crash recovery only
- [x] `go build`, `go vet`, `gofmt`, `go test` clean
- [x] Bench (macOS, root port): `switch target` suspends, `list`/`state` leave it suspended, `switch host` brings the disk back
- [x] A real target boots from the card while the reader is suspended

## Summary of Changes

- New `internal/usbsuspend`: `Suspend`, `Resume`, `StateOf`, `Strings` keyed by bus + port path. macOS uses IOKit via cgo (unconfigure + suspend; resume signalling, a 3 s settle, then re-enumerate) and reads state and strings from the IORegistry. Linux follows upstream (sysfs autosuspend attrs, usbfs driver detach/attach + reset, sysfs reads), with the sysfs logic in untagged, fake-tested code. Other platforms return `ErrUnsupported`.
- The SDWire3 controller switches by suspend where supported, skips a reader already in the requested state, waits for the state to settle, and reads `Mode` from that state. It holds no reader handle; an unresolvable hub (root port) only limits `Revive`. Windows keeps hub-port power switching unchanged.
- Discovery describes SDWire3s from the registry/sysfs without opening them, so `list`, `state` and `connect` never wake a suspended reader; `ListDeviceStates` reads each attached SDWire3's live state.
- Deleted `WithLegacySDWire3Switching` and its controller.
- `blockdev.PackLocationID` and `blockdev.SysfsName` exported for reuse.
- README, CLI help, the repo skill and JP's bench skill copy describe suspend switching, with hub port power as crash recovery.
