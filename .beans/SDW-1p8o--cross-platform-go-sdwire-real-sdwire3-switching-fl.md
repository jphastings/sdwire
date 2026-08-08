---
# SDW-1p8o
title: 'Cross-platform Go sdwire: real SDWire3 switching, flash helper, power plugins, CLI'
status: completed
type: epic
priority: normal
created_at: 2026-08-08T10:05:23Z
updated_at: 2026-08-08T19:20:00Z
---

Make this Go port (forked/renamed to `github.com/jphastings/sdwire`) a complete, cross-platform replacement for the Python `sdwire` CLI, plus the pieces the bench actually needs: working SDWire3 target switching, a flash-cycle helper, per-device DUT power hooks, a Meross plug plugin, and a cobra/viper CLI.

## Background (diagnosed 2026-08-08 on the bench)

- The SDWire3 (single Realtek `0bda:0316` reader chip, no FTDI) **never releases the SD card to the target while its USB link is up**. The mechanism the Python CLI uses for SDWire3 — kernel-driver detach + USB reset, which `sdwire.go`'s `sdwire3Controller` ports faithfully — does not move the mux on macOS (verified empirically: the target side saw nothing even after a full re-probe) and is reported broken on WSL/Windows too: <https://github.com/Badger-Embedded/sdwire-cli/issues/27> (that author's workaround: physically unplug the USB-C cable).
- **Cutting VBUS on the SDWire3's upstream hub port** (what `uhubctl` does) hands the card to the target within ~1 s; repowering returns it to the host — host is the power-on default — with the block device back in ~6 s. Verified end-to-end through a CalDigit TS4 dock hub.
- The Python CLI's `state` for SDWire3 is not a hardware readback (it reports kernel-driver attachment); an honest replacement should report port power / device presence.
- Two semantics the helpers must encode:
  - Targets only initialise a card on a card-detect edge or at boot → after a handover the DUT must be (re)booted or otherwise re-probe.
  - Small DUT boards ride through ~2 s mains cuts on PSU capacitance → power-cycle helpers must guarantee a **minimum dark time** (default ≥8 s).
- SDWireC (FTDI `04e8:6001`, product "sd-wire") switching via CBUS bitmode **works** and stays as-is.

## Decisions

- Fork & rename the module to `github.com/jphastings/sdwire`; small bugfixes may be offered upstream to `fcjr/sdwire` as patches for JP to submit (never PR third-party repos without JP's say-so).
- Windows support is **best-effort**: native hub port-power works only where the hub is accessible via WinUSB/UsbDk; otherwise fail with a clear, actionable error.

## Children

Ordering: VBUS switching and the reorg/power-hooks come first; flash helper and Meross plugin build on them; the CLI lands last.

## Summary of Changes

The fork (github.com/jphastings/sdwire) is now a complete Go replacement for the Python sdwire CLI plus the bench helpers, all committed as conventional-commit chunks on main:

- **Reorg + latent bugs (SDW-xlg1, SDW-uh3s, SDW-hjkk)**: multi-file package, SDWire-owned gousb context, serial+port-path identity (Python-compatible display form) with unambiguous selection, per-device PowerFunc + PowerCycle with ≥8s default dark time.
- **SDWire3 VBUS switching (SDW-v0mc)**: hubpower/ package (port power, honest GetPortStatus readback, per-port/ganged detection, on-disk port cache keyed by identity); constructors revive powered-off devices via the cache; legacy detach+reset behind an option. Bench-verified both directions incl. fresh-process revive.
- **FlashAndBoot (SDW-5eo5)**: identity-tied block-device detection (macOS ioreg — system_profiler is broken on macOS 26 — Linux sysfs, Windows WMI), ambiguity refusal, sector-aligned raw writes (bench-found alignment + fsync quirks), dark-time top-up. Bench-verified to the sudo boundary; write engine byte-exact on a real raw device.
- **Meross plugin (SDW-q62d)**: local-API client (signed envelopes, SETACK-or-error, metering with unit conversion), credentials README.
- **CLI (SDW-1wub)**: byte-compatible list/state/switch (verified against a captured Python v0.3.1 run), flash/power/disk/--json, viper config binding devices to power plugins, goreleaser + per-OS CI. Read-only commands never mutate hardware; power is USB-free.
- **Safe unmount (SDW-or85)**: SetMode(ModeTarget) unmounts the reader's volumes by default (bench-verified), WithoutUnmount/--no-unmount to skip.

Bench-hardening discovered and fixed along the way: the Realtek reader wedges if re-powered too quickly (now ≥5s dark + hub-port-status polling instead of full-bus enumeration during bring-up, one power-cycle retry); state readback made side-effect free (a state query must never revive a powered-off device).

**Residuals** (tracked in standalone SDW-dhkl, needs JP): live Meross on/off (account key + which of 192.168.1.112/.114 is the bench plug), one sudo sdwire flash run with target boot, and a Mac reboot to clear the stale IOKit entry that blocks the old sdwire-reboot tool.
