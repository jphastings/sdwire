---
# SDW-1p8o
title: 'Cross-platform Go sdwire: real SDWire3 switching, flash helper, power plugins, CLI'
status: todo
type: epic
created_at: 2026-08-08T10:05:23Z
updated_at: 2026-08-08T10:05:23Z
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
