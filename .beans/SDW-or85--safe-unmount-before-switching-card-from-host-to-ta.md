---
# SDW-or85
title: Safe unmount before switching card from host to target
status: in-progress
type: feature
priority: normal
created_at: 2026-08-08T18:50:34Z
updated_at: 2026-08-08T19:11:10Z
parent: SDW-1p8o
---

Switching the card away from the host (`SetMode(ModeTarget)`) currently yanks the block device out from under the OS: SDWire3 cuts reader power, SDWireC flips CBUS bits instantly. If any volume is still mounted, buffered writes can be lost and macOS pops "Disk Not Ejected Properly". Perform a platform-specific safe unmount before the switch, by default.

## Behaviour

- On `SetMode(ModeTarget)` when the card is (or may be) host-side: locate this SDWire's reader block device (identity-tied, `internal/blockdev`), unmount its volumes, then switch.
- Skip silently when there is nothing to do: no block device found for this reader's identity, or already in target mode. A failed *lookup* must not block the switch when nothing was mounted.
- If an actual unmount fails, fail the switch with a clear error — don't pull a mounted filesystem out. The disable option is the escape hatch.
- Default ON; can be disabled.

## Design notes

- The machinery already exists: identity-tied block-device location + per-OS `blockdev.Unmount` (`diskutil unmountDisk` / `umount` partitions / `FSCTL_LOCK_VOLUME`+`DISMOUNT_VOLUME`). `FlashAndBoot` already uses both via `checkSizesAndUnmount` (flash.go).
- Option placement: variadic options on `SetMode(mode, opts ...ModeOption)` is backwards-compatible and lets the CLI expose a per-invocation flag; a device-level option at open time is the alternative. Decide during implementation — lean SetMode-level.
- "Safe" vs force: the darwin `blockdev.Unmount` retries with force when politely dissented (loginwindow), which suits the flash path. Decide whether the switch path keeps that (data is flushed either way) or stops at polite + error.
- `FlashAndBoot`'s own unmount becomes a harmless duplicate (unmount is idempotent); optionally drop its call once SetMode handles it.
- CLI (SDW-1wub, in progress): surface a disable flag (e.g. `--no-unmount`) on the target-mode command — coordinate rather than duplicate.

## Acceptance

- [ ] `SetMode(ModeTarget)` with a mounted card unmounts before switching on the macOS bench; no "Disk Not Ejected Properly" notification
- [ ] Disable option skips the unmount entirely
- [ ] No-op paths (no device present / already target-side) neither error nor noticeably slow the switch
- [ ] Behavioural unit tests over the decision logic with fakes; Linux/Windows paths compile-checked
