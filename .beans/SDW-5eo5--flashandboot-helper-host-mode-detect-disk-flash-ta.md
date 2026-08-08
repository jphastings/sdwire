---
# SDW-5eo5
title: 'FlashAndBoot helper: host mode, detect disk, flash, target mode, power'
status: todo
type: feature
priority: normal
created_at: 2026-08-08T10:05:58Z
updated_at: 2026-08-08T10:06:47Z
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

- [ ] Full cycle works on the bench (macOS) with a real image, card boots the target
- [ ] Wrong-disk protection demonstrated (second reader attached → refuses)
- [ ] Linux and Windows detection paths implemented and unit-tested against recorded fixtures
