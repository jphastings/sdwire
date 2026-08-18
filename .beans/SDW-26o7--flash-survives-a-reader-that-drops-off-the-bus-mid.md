---
# SDW-26o7
title: Flash survives a reader that drops off the bus mid-write
status: completed
type: feature
priority: high
created_at: 2026-08-18T11:43:12Z
updated_at: 2026-08-18T11:50:20Z
---

A wedged reader currently kills a flash outright: writeImage returns on the first write error, leaving a half-written card and a command that failed — the "indistinguishable from a dead board" trap of SDW-ox5r, now with 80% of an image on the card.

The reader's stall itself is not fixable in software (see SDW-rmgj for the mechanism: the bulk-OUT endpoint stalls, macOS's port reset fails, IOKit tears the device down). What is fixable is the consequence.

## 1. Resume across a revive

Every piece already exists:

- writeImage tracks bytes written and writes sector-aligned chunks, so the resume offset is exact and aligned by construction
- revivePortPower + waitForSDWireViaPort recover the device, verified at ~6.5s on the bench
- waitForBlockDevice + blockdevRef(s.info) re-find the reader *by USB location*, so a resume cannot land on a different card

So: on a write error that looks like the device going away, power-cycle its hub port, wait for the block device to return at the same location, unmount whatever macOS auto-mounted, reopen, seek to the last completed offset, and carry on. Bounded retries, and a warning per retry — never silent, per the SDW-ox5r lesson.

## 2. Instrument the writes

Whether pacing the writes would make the wedge rarer is a hypothesis, not a finding. The evidence is suggestive: in the 19:31 trace the WRITE(10) counts per interval run 267, 267, 272, 268, 275, 269, 274 — steady — then a read burst, then 404, 295, 163, then the stall. Throughput spiking then collapsing is what a reader whose write buffer has backed up looks like.

Rather than guess a chunk size, emit per-chunk write timings under --debug so a normal flash collects the data.

## Todo

- [x] Controller-level revive for SDWire3, reusing the hub port the controller already holds
- [x] Resume writeImage from an offset, reporting absolute progress
- [x] Retry loop: detect device-lost errors, revive, re-find, unmount, resume
- [x] Per-chunk write timing hook, printed by the CLI under --debug
- [x] README + docs

## Summary of Changes

**Controller-level revive.** `sdwire3Controller.Revive` power-cycles the
hub port the controller already holds open and waits for the reader to come
back, without changing which side the card is on — a powered SDWire3 is on
the host either way. It is offered through a new optional `deviceReviver`
interface rather than added to `DeviceController`, so generations with no
power control simply don't have one and callers can tell.

`SetMode(ModeHost)` would have recovered such a device eventually, but only
after waiting out `hostWaitTimeout` against an already-powered port before
reaching its power cycle. Recovery goes straight to the part that works.

**Resumable writes.** `writeImage` takes a resume offset, seeks both image
and device to it, and returns the absolute bytes written *alongside* any
error — that return is what makes continuation possible, since the caller
resumes from exactly where the call stopped. Progress stays absolute across
a resume, so the bar never jumps backwards.

**The retry loop.** `writeImageResuming` power-cycles, waits for the block
device at the same USB location (so a resume cannot land on a different
card), unmounts whatever the OS auto-mounted from the half-written card,
and continues. Bounded by `WithWriteRetries` (default 3), with a warning
naming the offset and attempt each time — a flash that needed three power
cycles must not look like a clean one.

Retries are gated on `deviceLost`: ENXIO/ENODEV/EIO, the shape of a reader
torn off the bus. A full card, an unreadable image or a cancelled context
fails immediately, since no amount of power-cycling fixes those.

**Instrumentation, not a guess.** `WithWriteTiming` reports each chunk's
offset, size and write duration; the CLI prints it under `--debug` with
throughput. Deliberately no pacing change yet — whether smaller chunks or
inter-chunk pauses would help is still a hypothesis, and this is what will
settle it from a normal flash rather than a synthetic one.

**Testing.** A `rawDevice` seam behind `openRawDevice` lets a fake device
fail mid-write the way a real one does, which a temp file cannot. The
resume test pins the offsets written across the failure, and was
mutation-checked: forcing `resumeFrom = 0` makes it fail with
`wrote at offsets [0 1024 1024 2048 3072]`, catching both the wasted
rewrite and the corruption that a naive restart would cause.

Not done: the reader still stalls; this only stops it costing the flash.
Whether pacing reduces the stalls is deferred until the timing data exists,
and a verify-after-flash pass (read back and compare) is worth considering
now that a flash can span several power cycles.
