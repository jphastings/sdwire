---
# SDW-ox5r
title: A stale config location, and flash's silent skipped power cycle, together fake a dead board
status: completed
type: bug
priority: high
created_at: 2026-08-12T10:16:28Z
updated_at: 2026-08-12T14:56:45Z
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


## Summary of Changes

All three, plus the cache ghosts behind the ambiguity.

**1. Stale `location:` no longer fatal.** `resolveSelection` keeps a
configured device's serial as `sel.fallback` when it also has a location.
`openSelected` still tries the location first (it is the more specific
selector, and identical Realtek serials need it), but on failure retries by
serial and warns:

> warning: config device "bench": no SDWire device is at its configured
> location "1-1.1.3"; connected by serial instead, now at location "1-1.1.4"
> — update or remove that location: line in your config

The cached-hub-port path (`state`, `switch dut` on a powered-off SDWire3)
gets the same fallback via the new `cachedPortStateFor`. When both
selectors fail the error names the config device and both attempts, and
wraps both underlying errors so `errors.Is(…, ErrNoDeviceFound)` still
works for callers.

**2. `flash` is loud about a skipped power cycle.** The debug-only `else`
branch is gone. After a successful flash with no power plugin it prints, to
stderr, consequence first and configuration second — reusing `sdwire
power`'s YAML snippet (now factored out as `powerConfigSnippet`) with the
device's *live* location filled in. `--require-power` refuses upfront
instead, before anything is written.

**3. The ambiguity error names its input.** Selectors read from config
carry an `origin` (`from the "location" of config device "bench"`), which
is appended to connect and state errors. The reported failure now reads:

> Error: reading device state (from the "location" of config device
> "bench"): location 1-1.1.3 matches multiple SDWire devices; specify one
> of: unknown.1.1.3, 20120501030900000.1.1.3

**4. The ghost entries.** `hubpower.Cache.Put` now evicts any entry under a
different key naming the same physical hub port. That pair — `unknown.1.1.3`
and `20120501030900000.1.1.3`, verified to hold identical `PortRef`s in the
bench's real cache — is the same device recorded before and after its serial
could be read; only one device can be in a port, so the older key is stale by
definition. This is what made the location lookup ambiguous.

README: the flash section documents the warning and `--require-power`; the
config section documents the location→serial fallback; the SDWire3 state
semantics section now says where the hub-port cache file actually lives
(`<user cache dir>/sdwire/hubports.json`, not the config directory) and that
deleting it is safe — the bean's "there is no cache file to remove".

Not done: entries already in an existing cache aren't retroactively deduped,
and a serial fallback against the *cache* is still ambiguous for a device
that has been seen at several ports over its life (three such entries exist
on the bench). The live-device path — the reported bug — resolves before
reaching the cache, so this only bites a powered-off SDWire3 with a stale
config location. Deleting hubports.json clears it.
