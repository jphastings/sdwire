---
name: sdwire
description: Flash SD cards, boot-cycle, and power off a target board via the sdwire CLI. Use when flashing an image to real hardware, power cycling the device under test, editing files on the target's SD card, or running hardware bring-up / boot tests on a bench with an SDWire SD mux.
---

# SDWire bench control

The bench pairs an SDWire SD-card mux (SDWireC or SDWire3) with an optional
smart plug for target-board power, both driven by the `sdwire` CLI
(github.com/jphastings/sdwire). Config lives in
`~/.config/sdwire/config.yaml`, which binds devices to their power plugins —
never copy credentials out of it. With several muxes attached, add
`-s <serial|location|config-name>` to every command.

All commands are safe from any starting state. `sdwire state` is read-only
and never disturbs the card — check it freely.

## Flows

Boot / power-cycle the target (repeat freely for boot-loop tests; needs a
power plugin configured, which the CLI will explain if missing):

```sh
sdwire switch dut && sdwire power cycle
```

Flash an image and boot it — one call does power off → card to host →
write → card to target → power on:

```sh
sudo "$(which sdwire)" flash path/to/sdcard.img
```

(Raw disk writes need elevation, and sudo's `secure_path` usually excludes
Go's bin directory — hence the explicit path.)

Power off and edit the card on this machine — volumes automount on macOS;
`sdwire disk` prints the whole-disk device path for scripts:

```sh
sdwire power off && sdwire switch host
```

## Rules

- Be patient: re-powering an SDWire3's reader can take up to a minute by
  design (readers can wedge if re-powered too fast; the CLI waits and
  retries). Don't interrupt and rapid-retry by hand.
- `switch dut` auto-unmounts the card's volumes first; close anything
  holding files open on them. No manual unmounting in either direction.
- `power cycle` holds ≥8s dark deliberately (small boards ride through
  shorter cuts on PSU capacitance). Targets only notice a new card at
  boot — hence the boot flow ends in a power cycle.
- `power off` is an abrupt cut: fine for appliance-style boards, but
  quiesce first if a writable data partition may be mid-write.
- An SDWire3 must sit behind an external USB hub with per-port power
  switching (see the project README); a root-port or ganged-hub setup
  produces a clear error/warning rather than working.
- `--json` on list/state/disk for scripting.

## Troubleshooting: target silent after flash+boot

Flashing can succeed while no target can boot through the mux. Run the
control experiment before blaming the board, image, or wiring: flash a
known-good board+image combo (one that has booted from a directly-inserted
card before); if that is silent too, suspect the rig itself. `sdwire state`
reporting `Target` proves the mux really handed the card over, so look
downstream (card seating, target power, board). A lone `\x00` on serial at
power transitions is line bounce, not data.

## Serial console

The mux gives no visibility into the target — pair it with a USB serial
adapter on a *different* hub port (so captures survive the mux's power
cycling), and start the capture before booting to record from the first
byte.
