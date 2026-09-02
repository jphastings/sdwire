---
# SDW-ziqh
title: An off-bus SDWire3 still reads as absent when its serial has several remembered ports
status: in-progress
type: bug
priority: high
created_at: 2026-09-02T04:53:33Z
updated_at: 2026-09-02T04:59:50Z
---

Second bench report of the SDW-rmgj family, 2026-09-02 (raw write-up kept
outside git as bench-report-2026-09-02-sdwire3-off-bus.md). Two episodes
where the SDWire3 vanished from USB and every command the operator ran
(`state`, `list`, `power on`, `power cycle`) reported it absent. A physical
move to a different TS4 port "fixed" the first; the second followed a
`flash`. Neither needs a hardware explanation: both are the mux's upstream
hub port being unpowered, which is how SDWire3 target mode is implemented.

## What was actually wrong

1. **The installed CLI was v0.2.0**, which predates `9522f04` (`list` shows
   cached-but-absent devices with a State column, `revive`) and `2571325`.
   `go install …@latest` resolves to v0.2.0 because nothing has been tagged
   since. Both episodes would have been a `Target` line in `list`.

2. **The bench skill led the operator astray.** It says `state` is safe to
   check freely and prescribes a control experiment for "target silent",
   but never says that after `flash`/`switch dut` an SDWire3 is off the bus
   by design, that an empty `list` is not a fault, that `switch host` is the
   recovery, or that `power` never touches the mux.

3. **On HEAD, `state` still cannot answer for a device configured by
   serial once that serial has several cache entries.** The bench cache has
   four (`.1.1.1.3`, `.1.1.3`, `.1.1.4`, `.2.4.4`): every physical replug
   adds one and nothing ever removes them. `selectCacheEntry` reports the
   ambiguity honestly (SDW-rmgj) but still errors, and `switch host`
   brute-forces the ports in random map order at up to `hostWaitTimeout`
   per miss. The cache records *where* a device has been but not *when*,
   so it has no way to prefer the port the device was last seen at — which
   is the only sensible guess when it isn't on the bus.

4. **Dark ports persist.** `PORT_POWER` is hub state: a port switched off
   for target mode stays off across unplug/replug and across CLI processes.
   Episode 1 is almost certainly the mux replugged into the port a previous
   session left dark. Not fixable in software without powering ports that
   may belong to another mux deliberately in target mode (a data-loss
   hazard), so the fix is to make it *legible*: `list` shows the port as
   `Target`, and the skill says `switch host` re-powers it.

5. **Side finding: moving the whole dock to the other Mac port changes the
   bus number** (libusb derives it from the IOKit locationID's top byte) of
   every cached entry. Until the mux is next seen live on the dock, `list`
   silently dropped them and `state`/`switch host` failed with "no hub
   found at bus 1 path [1 1]". Verified on the bench: TS4 hubs now on bus 0,
   cache says bus 1. The bus field is trustworthy; the drop was silent.

## Todo

- [x] Cache entries record when the device was last seen at that port
- [x] A serial matching several remembered ports resolves to the most recently seen one (`state`, `switch dut`, `revive` by serial), and `switch host`/`flash` power that port first
- [x] A bare serial that matches nothing says "serial", not "location"
- [x] `list` warns, once, when remembered ports are skipped because their hub can't be opened
- [x] Bench skill: off-bus after flash is normal; `list` State column; `switch host`/`revive` recover; `power` never touches the mux; never a Mac root port
- [x] README: most-recently-seen rule
- [ ] Tag a release so `go install …@latest` carries the fix


## Summary of Changes

**Cache entries now say when, not just where.** `hubpower.PortRef` gains
`LastSeen`, stamped by `Cache.Put` on every live sighting (`New*`
constructors, `Revive`). Entries from before the field existed load with a
zero time and are simply the least preferred.

**A serial seen at several ports resolves to the most recent one.**
`selectCacheEntry` — the one function behind `CachedPortState` (`state`,
`switch dut`'s already-in-target check) and `Revive` by serial — orders
entries by recency and, when a bare serial matches several, takes the
strictly most recently seen. A tie, or entries with no sighting recorded,
still get the SDW-rmgj ambiguity error naming each port and the cache file.
`tryCacheFallback` (`switch host`, `flash`, the SDK constructors) powers
ports on in the same order, so the common case costs one port, not up to
`hostWaitTimeout` per socket the device has ever lived in.

**A bare serial that matches nothing says so.** `parseLocation` accepts an
all-digit serial as a bus number, which is why v0.2.0 printed "no SDWire
device matching location 20120501030900000" for a device configured by
serial. The serial lookup's error is now returned for any selector that
isn't location-shaped.

**`list` says when it drops entries.** `cachedDeviceStates` skipped, in
silence, every entry whose hub couldn't be opened. It now warns once with
the count and the cache path. Verified on the bench in exactly that state:
the TS4 had been moved to the other Mac port (bus 1 → 0), so all four
entries were unreachable and HEAD's `list` had shown only the live row.

**Skill.** Both `skills/sdwire/SKILL.md` and JP's bench-specific copy at
`~/.claude/skills/sdwire/SKILL.md` gain a "When the mux disappears" section:
off-bus after `flash`/`switch dut` is by design, read `list`'s State
column, `Target` → `switch host`, `Unknown` → `revive -s <location>`,
`power` never touches the mux, never a root port, and only then the
control experiment. The opening claim about `state` now points at `list`.

**README.** The most-recently-seen rule under SDWire3 state semantics; the
skipped-entry warning under `list`.

**Bench verification.** The most-recently-seen path could not be exercised
live: the mux is currently on a Mac root port, so it resolves as a live
device before the cache is consulted, and no hub port can stamp it.
Covered by unit tests at `selectCacheEntry` and `CachedPortState` level.
The warning path was exercised live (above). No command wrote to the cache
(`last_seen` absent from the bench file afterwards).

**Not done, deliberately.** Re-powering every other remembered port on
`switch host` (report item 8) would switch a second same-serial mux
sitting in target mode back to host — a data-loss hazard for a running
target — so the dark-port illusion is made legible instead. `power`
noticing an off-bus mux (item 3), `power state` (item 5) and a `doctor`
command (item 6) are unbuilt; the skill fix covers the operator-side
confusion behind item 3. `list` warns about unreachable entries rather
than listing them as rows (item 4).
