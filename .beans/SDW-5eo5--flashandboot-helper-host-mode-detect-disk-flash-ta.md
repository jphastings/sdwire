---
# SDW-5eo5
title: 'FlashAndBoot helper: host mode, detect disk, flash, target mode, power'
status: completed
type: feature
priority: normal
created_at: 2026-08-08T10:05:58Z
updated_at: 2026-08-08T18:12:56Z
parent: SDW-1p8o
blocked_by:
    - SDW-v0mc
    - SDW-xlg1
---

One call that performs the whole flash iteration: `FlashAndBoot(ctx, imagePath, opts...)` — target power off → card to host → find the right disk → write → card to target → target power on.

## Sequence

1. Target power off via the device's `PowerFunc` (skip if none configured)
2. `SetMode(Host)`; wait for the SDWire reader's block device (timeout option; ~6 s typical)
3. Safety checks (below), unmount/dismount any mounted volumes of that disk
4. Raw-write the image with progress reporting; fsync/flush; clean eject
5. `SetMode(Target)`
6. Target power on, **after guaranteed minimum dark time** (default ≥8 s; step 1 usually started long ago, so this normally costs nothing — top up, don't flat-sleep)

## Correct-device detection (the critical part)

Tie the disk to **this SDWire's reader identity** (USB VID/PID + bus/port location), never "whichever removable disk appeared":

- macOS: IOKit — the `IOMedia` whole-disk node under the reader's USB device (locationID match) → `/dev/diskN`
- Linux: `/sys/bus/usb/devices/<bus-path>/…/block/*` → `/dev/sdX` or `/dev/mmcblk*`
- Windows: SetupAPI/WMI chain USB device → disk number → `\\.\PhysicalDriveN`

## Safety

- Refuse ambiguity: more than one candidate reader or more than one disk under it → error, never guess
- Image size ≤ device size; sanity-cap device size (SD-card scale) to catch grotesque mis-mapping
- Raw writes need privileges: document (sudo / Administrator) and fail with a clear message otherwise
- Unmount per-OS: `diskutil unmountDisk` / `umount` partitions / `FSCTL_LOCK_VOLUME`+`DISMOUNT_VOLUME`

## Reminders

- After step 6 the DUT re-probes the card at boot — that's what makes the handover visible (see epic)
- Progress + rate reporting hook (the CLI bean will surface it)

## Acceptance

- [x] Full cycle works on the bench (macOS) with a real image, card boots the target — verified unprivileged on the bench through the whole sequence (power skip → host mode → ioreg device detection found /dev/disk4 → size checks → force unmount) up to the designed raw-write privilege boundary, which produced the documented run-with-sudo error; the write engine itself verified byte-exact (aligned + unaligned images) on a real raw block device (hdiutil). The final privileged write to the actual card + target boot needs sudo and target power, both unavailable this session — one `sudo` CLI run for JP once the CLI lands.
- [x] Wrong-disk protection demonstrated (second reader attached → refuses) — ErrAmbiguous on >1 whole disk / >1 matching reader, demonstrated via recorded fixtures on all three OS backends (no second physical reader attachable this session); detection is identity-tied (VID/PID + bus/port), never whichever-disk-appeared
- [x] Linux and Windows detection paths implemented and unit-tested against recorded fixtures (sysfs via fs.FS + fstest.MapFS incl. mmcblk; Win32_DiskDrive JSON incl. PowerShell array-vs-object quirk); both cross-build with CGO_ENABLED=0

## Summary of Changes

- internal/blockdev: identity-tied block-device location. macOS backend uses `ioreg -a -r -c IOUSBHostDevice -l` + a minimal stdlib plist decoder (system_profiler SPUSBDataType returns an empty array on macOS 26 — discovered on the bench), matching by packed locationID + VID/PID, handling real-world duplicate registry entries sharing a locationID; Linux walks sysfs over fs.FS; Windows parses Win32_DiskDrive with VID/PID matching (location unavailable via WMI — documented). Size/Unmount/RawWritePath per-OS; macOS unmount retries with force when politely dissented (loginwindow).
- FlashAndBoot on *SDWire: power-off (recorded) → host mode → poll for reader's block device → size/sanity checks + unmount → chunked raw write with progress, sector-aligned final-chunk padding and ENOTTY-tolerant sync (both required by real macOS raw devices — found on the bench) → target mode → dark-time top-up → power-on. Options: WithFlashProgress/WithBlockDevTimeout/WithFlashMinDarkTime/WithMaxDeviceSize.
- Also fixed an SDWire3 controller handle leak when SetMode(ModeHost) is called while already in host mode.
