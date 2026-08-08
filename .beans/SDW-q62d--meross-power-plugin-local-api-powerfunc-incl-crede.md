---
# SDW-q62d
title: Meross power plugin (local API PowerFunc, incl. credential docs)
status: todo
type: feature
priority: normal
created_at: 2026-08-08T10:05:58Z
updated_at: 2026-08-08T10:06:47Z
parent: SDW-1p8o
blocked_by:
    - SDW-xlg1
---

`power/meross` package: a `PowerFunc` provider that drives Meross smart plugs (MSS315 and family) over their **local** HTTP API — no cloud at runtime.

## API sketch

- `meross.New(ip, key string, opts ...Option) (sdwire.PowerFunc, error)` — the simple path
- Richer client for verification: `State()` (relay on/off), `Model()`, and where supported (MSS315 has metering) `Electricity()` returning live V/A/W — useful to *prove* a load actually dropped. Caveat to document: readings lag several seconds; trust trend, not instant values.

## Protocol (verified working against an MSS315)

- POST `http://<ip>/config`, JSON body with `header` + `payload`
- Header: `messageId` = random 32-hex, `timestamp` = unix seconds, `sign` = `md5(messageId + key + timestamp)`, `method` = `SET`/`GET`, `namespace`, `payloadVersion: 1`
- Switch: namespace `Appliance.Control.ToggleX`, payload `{"togglex":{"channel":0,"onoff":0|1}}`; success ⇔ response `header.method == "SETACK"`
- State/model: GET `Appliance.System.All` (digest.togglex, hardware.type, MAC); metering: GET `Appliance.Control.Electricity`
- The plug's own electronics stay powered with the relay open, so it remains controllable

## Credentials — document this in the package README

- The `key` is the **Meross account key** (shared by all plugs on the account), not per-device:
  1. Easiest: log in once with the MerossIot library (<https://github.com/AlbertoGeniola/MerossIot>) — the HTTP client object exposes the account `key`; a five-line Python snippet in the README
  2. Home Assistant users: the `meross_lan` integration's diagnostics show the device key
  3. Very old firmware accepts an empty key — worth one try before fetching credentials
- The plug's IP should be DHCP-reserved; config takes a static IP (no discovery in v1 — note mDNS/`Meross` UDP discovery as a possible follow-up)

## Reminders

- Relay-off ≠ board-off instantly: PSU ride-through is why callers (PowerCycle/flash helper) enforce minimum dark time — this plugin just switches honestly and fast
- Timeouts: LAN calls with ~5 s timeout; SETACK missing ⇒ hard error (never assume the cycle happened)

## Acceptance

- [ ] On/off/state verified against the bench MSS315
- [ ] `Electricity()` works and its lag documented
- [ ] README covers key retrieval (MerossIot route with snippet) and IP reservation
- [ ] Plugs into a device via `SetTargetPower` and drives a real `PowerCycle`
