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

All commands are safe from any starting state. `sdwire state` and `sdwire
list` are read-only and never disturb the card — check them freely, and
prefer `list`: it also shows a mux that is off the bus (see below).

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

- Be patient: `switch host` on an SDWire3 takes several seconds (the reader
  is woken and given time to bring the card up before re-enumerating), and
  reviving a crashed reader can take up to a minute by design (readers can
  wedge if re-powered too fast; the CLI waits and retries). Don't interrupt
  and rapid-retry by hand.
- `switch dut` auto-unmounts the card's volumes first; close anything
  holding files open on them. No manual unmounting in either direction.
- `power cycle` holds ≥8s dark deliberately (small boards ride through
  shorter cuts on PSU capacitance). Targets only notice a new card at
  boot — hence the boot flow ends in a power cycle.
- `power off` is an abrupt cut: fine for appliance-style boards, but
  quiesce first if a writable data partition may be mid-write.
- On macOS and Linux an SDWire3 switches on any USB port. A hub with
  per-port power switching is only needed for crash recovery (`revive`,
  flash resume) — and on Windows, for switching at all (see the README).
- `--json` on list/state/disk for scripting.

## When the mux "disappears"

After `flash` or `switch dut` an SDWire3 stays on the USB bus,
**suspended**: target mode USB-suspends the reader, so `sdwire list` shows
a `Target` row with no block device. That is the healthy post-flash state,
not a fault. A row that vanishes, or an empty `list`, means the reader
crashed off the bus. Read the `State` column of `sdwire list` before
touching anything:

| `list` shows | Meaning | Do |
| --- | --- | --- |
| `Host` + `/dev/diskN` | On the bus, card on this machine | Carry on |
| `Target` | Reader suspended (or its hub port unpowered); card is with the target | `sdwire switch host` brings it back |
| `Unknown` | Reader awake but unconfigured (something woke it), or a hub port powered with nothing on it: empty socket, or a reader that crashed off the bus | `sdwire switch dut`/`host` if it is listed as attached; otherwise `sdwire revive -s <location>` |

- `power on|off|cycle` drive the mains plug only and never touch the mux:
  they succeed while it is off the bus and cannot bring it back.
- Anything that resumes the reader takes the card back to the host: the
  computer waking from sleep, or another tool reading the reader's USB
  descriptors (an older `sdwire` binary's `list` does). If `state` says
  `Host`/`Unknown` where you expected `Target`, `sdwire switch dut` again.
- An SDWire3 may go straight into the computer's own port: switching only
  needs USB suspend. A root port can't be power-cycled, though, so `revive`
  and flash crash recovery need the SDWire3 behind a hub with per-port
  power switching.
- A hub port left switched off (by an interrupted revive) stays off across
  replugs and reboots. A mux plugged into such a port does not enumerate
  until something re-powers it; `switch host` does, a different socket
  merely sidesteps it.
- Only reach for the control experiment below once `list` says `Host` or
  `Target` and the target is still silent.

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
adapter on a *different* hub port (so captures survive the mux
re-enumerating or being power-cycled), and start the capture before booting to record from the first
byte.
