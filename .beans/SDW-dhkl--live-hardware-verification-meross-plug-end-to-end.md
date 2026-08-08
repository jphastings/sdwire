---
# SDW-dhkl
title: 'Live-hardware verification: Meross plug end-to-end + privileged flash boot'
status: in-progress
type: task
priority: normal
created_at: 2026-08-08T19:19:00Z
updated_at: 2026-08-08T19:51:12Z
---

Residual verification for epic SDW-1p8o that needs things only JP can provide. Everything is implemented and unit/bench-verified up to these boundaries:

## Needs from JP

1. **Meross account key** — retrieve per power/meross/README.md (MerossIot snippet, or Home Assistant meross_lan diagnostics). Both bench-LAN plugs reject the empty key (error 5001).
2. **Which plug is the bench MSS315**: 192.168.1.112 (uuid ...e3ddeb) or 192.168.1.114 (uuid ...d9290d)? The other is some unrelated appliance — must not be switched. Once the key exists, `meross.NewClient(ip, key).Model()` identifies each safely (read-only).
3. A **sudo** run for the raw card write.

## Then verify

- [x] Configure ~/.config/sdwire/config.yaml (bench device: serial 20120501030900000, location 1-1.1.3, power type meross + ip + key) — done 2026-08-08; the bench plug is 192.168.1.227 (a third plug, not .112/.114!), Model() confirms mss315, mac 48:e1:e9:d9:2a:98; key lives only in the config file (chmod 600), never in the repo
- [x] `sdwire power cycle` — relay clicks, board reboots after ≥8s dark time (SDW-q62d: on/off/state + real PowerCycle) — verified live: CLI power on/cycle/off all SETACKed with honest relay readback (cycle 9.6s incl. 8s dark), and the SDK-level sdwire.New(WithTargetPower(meross.New(...))).PowerCycle(0) ran in 8.7s
- [x] `Electricity()` live readings trend sensibly across a power cycle (SDW-q62d) — relay off: 240.6V/0.000A; ~12s after power-on: 244.4V/0.051A (board PSU load visible); immediately post-cycle reads 0.000A, demonstrating the documented metering lag — trend, not instant values
- [ ] `sudo sdwire flash <known-good GoSD image>` — full cycle, target boots (SDW-5eo5/SDW-1wub end-to-end; watch via serial console). Safe path: dump the current card first (`sudo dd if=/dev/rdisk4 of=card-backup.img`) so the flash is restorable
- [ ] Unrelated but bench-blocking: reboot the Mac (or re-seat the SDWire3) to clear the stale IOKit registry entry (id 0x1000597a1) that makes sdwire-reboot refuse with "multiple USB3.0-CRW readers"

## Remaining (still JP-gated)

Only the sudo flash run and the Mac reboot for the stale IOKit entry remain; Meross verification is fully done.
