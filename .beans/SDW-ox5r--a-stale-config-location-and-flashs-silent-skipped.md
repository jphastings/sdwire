---
# SDW-ox5r
title: A stale config location, and flash's silent skipped power cycle, together fake a dead board
status: todo
type: bug
priority: high
created_at: 2026-08-12T10:16:28Z
updated_at: 2026-08-12T10:16:28Z
---

Found during a long GoSD bench session on 2026-08-12. Two independent
problems combined to cost roughly two hours and produce a false "the Pi 3B
doesn't boot" conclusion — twice. The board was fine; the real cause was an
unseated USB-C cable, but these two behaviours hid that.

## 1. A config pinned to `location:` breaks when the device changes port

`~/.config/sdwire/config.yaml` had, for the named device `bench`:

```yaml
serial: "20120501030900000"
location: "1-1.1.3"
```

After a physical replug the mux enumerated at `1-1.1.4`. From then on every
name-based or bare invocation failed:

```
Error: reading device state: location 1-1.1.3 matches multiple SDWire
devices; specify one of: unknown.1.1.3, 20120501030900000.1.1.3
```

...while `sdwire list` cheerfully showed the device present and healthy at
`20120501030900000.1.1.4`, and `-s 20120501030900000.1.1.4` worked fine.

A serial is stable across ports and reboots; a USB location is a property of
which socket the thing is plugged into. When a config carries both, the
stable identifier should win, or at least be tried first. Today the stale
location is authoritative and the good serial sitting one line above it is
never reached.

The ghost entries at the old location (`unknown.1.1.3`) survived a replug and
several minutes; they were not cleared by retrying, and there is no cache
file to remove — `~/.config/sdwire/` holds only `config.yaml`.

Worth deciding: should `location:` even be honoured when `serial:` is present
and matches exactly one device? It is genuinely useful for telling two
identical muxes apart, but that case is already served by their serials.

## 2. `sdwire flash` skips the power cycle with only a debug-level notice

This is the one that actually misleads. `cmd/sdwire/flash.go`:

```go
if res.powerCfg != nil {
    ... SetTargetPower(powerFunc)
} else {
    debugf(cmd, flags, "no power plugin configured; target will not be power-cycled after flashing")
}
```

At normal verbosity that branch is **silent**. The flash writes 272MiB,
prints a tidy progress bar to 100%, exits 0 — and the target is never
power-cycled, so it never boots what was just written. The operator sees a
successful flash and a board that does nothing, which is indistinguishable
from a dead board, a bad image, or bad wiring. That is precisely the wrong
place to be quiet.

The SDK layer is not at fault and should not change: `SDWire.TargetPower` and
`PowerCycle` are documented no-ops when no `PowerFunc` is set, which is
reasonable for a library whose caller may legitimately have no power control.
It is the CLI that should be loud about it.

**`sdwire power` already gets this right** — it refuses outright with
`explainMissingPowerConfig`, which even prints the YAML to add. That is the
standard `flash` should meet. Reusing that text as a warning would be most of
the work.

Suggested: warn at normal verbosity, in terms of the consequence rather than
the configuration — something like "no power control configured for this
device: the card was written but the target was NOT power-cycled, so it is
still running whatever it was before. Power-cycle it yourself, or add a power
section (see `sdwire power --help`)." Consider `--require-power` for scripted
use where a silent skip is never acceptable.

## 3. The ambiguity error doesn't name its input

"location 1-1.1.3 matches multiple SDWire devices" never says *where that
location came from* — that it was read out of the config's `location:` key
for the named device, rather than typed on the command line. That one clause
would have pointed straight at the stale config instead of sending someone
looking for a second physical mux.

## Suggested fix order

1. Prefer `serial` over a non-matching `location` (fixes the reported
   breakage; arguably a one-line config change for the user, but the tool
   should not need it).
2. Make `flash`'s skipped power cycle visible at normal verbosity.
3. Name the selector's origin in the ambiguity error.

(2) is the one worth doing even if the others are declined: it is the
difference between a five-second diagnosis and an hour of suspecting the
hardware.
