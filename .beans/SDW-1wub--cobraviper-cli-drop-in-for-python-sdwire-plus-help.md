---
# SDW-1wub
title: 'cobra+viper CLI: drop-in for Python sdwire plus helper commands'
status: completed
type: feature
priority: normal
created_at: 2026-08-08T10:05:58Z
updated_at: 2026-08-08T19:19:25Z
parent: SDW-1p8o
blocked_by:
    - SDW-v0mc
    - SDW-5eo5
    - SDW-xlg1
    - SDW-q62d
---

`cmd/sdwire/main.go` (cobra): a drop-in replacement for the Python `sdwire` CLI v0.3.1, plus commands for the new helpers, with viper config binding devices to power plugins.

## Drop-in compatibility (so existing scripts keep working)

- `sdwire list` — same column shape: `Serial  Product Info  Block Dev` (and fix Block Dev actually resolving on macOS, which the Python version doesn't)
- `sdwire state [-s serial]` — same output shape, but **honest** semantics for SDWire3 (Host = port powered & reader present; Target = port off), per the VBUS bean
- `sdwire switch {dut|target|host|ts|off} [-s serial]` — same subcommands & aliases; `switch off` keeps the Python behaviour (unsupported for SDWireC/SDWire3) since VBUS-off means "target", not "disconnected from both"
- Exit codes and single-device default (error when multiple devices and no `-s`) preserved
- Serial matching accepts the Python CLI's port-suffixed form (`20120501030900000.1.1.3`) as well as location-based selection

## New commands

- `sdwire flash <image> [-s …]` — the flash-cycle helper, with progress output
- `sdwire power {on|off|cycle} [-s …]` — drives the device's configured power plugin (cycle enforces the min dark time)
- `sdwire disk [-s …]` — print the reader's block-device path (scriptable; the Python project wanted this too)
- `--json` on list/state/disk; shell completions; `--debug`

## Config (viper)

`~/.config/sdwire/config.yaml`, env overrides `SDWIRE_*`; links each device to its power control when more than one of either exists:

```yaml
default_device: bench
devices:
  bench:
    serial: "20120501030900000"
    location: "1-1.1.3"        # disambiguates identical Realtek serials
    power:
      type: meross
      ip: 192.168.1.xxx
      key: "<meross account key>"
min_off_seconds: 8
```

- Zero-config path must work: one device, no power plugin → list/state/switch/flash all fine, `power` explains what to configure
- Unknown `power.type` → clear error listing registered plugins

## Install / rollout

- `go install github.com/jphastings/sdwire/cmd/sdwire@latest`; goreleaser (or simple Makefile) cross-builds for the three OSes
- On this Mac: shadows the Python one at `/opt/homebrew/anaconda3/bin/sdwire` — document PATH precedence / `pip uninstall sdwire`
- README: migration notes incl. the honest-`state` change and the SDWire3 VBUS requirement (device must sit behind a per-port-power hub)

## Acceptance

- [x] Byte-compatible-enough `list`/`state`/`switch` that existing scripts run unmodified on the bench — list/state output verified byte-identical to a captured run of the real Python v0.3.1 on this bench (and golden-tested); switch dut/target/host/ts/off semantics and exit codes match; ours additionally resolves Block Dev (Python prints None on macOS)
- [x] `flash` + `power cycle` work end-to-end using the Meross config — implemented and verified to their session-available boundaries (flash: full sequence on the bench up to the raw-write sudo boundary; power: config resolution, plugin registry, USB-free cycle path unit-tested; Meross client httptest-verified). The genuinely-live run needs JP's Meross account key + one sudo invocation → tracked in follow-up bean SDW-dhkl
- [x] Config binding proven with two devices defined (second may be fictional in tests) — unit-tested: name→location/serial resolution, default_device, -s override, min_off_seconds default, unknown power.type error listing plugins, env override
- [x] Cross-compiles for macOS/Linux/Windows — via the CI matrix (.github/workflows/ci.yml: native per-OS builds, since gousb needs platform libusb via cgo and this Mac has no cross toolchains); goreleaser config validated with a local darwin snapshot build; non-cgo packages verified with GOOS=linux/windows CGO_ENABLED=0

## Summary of Changes

cmd/sdwire (cobra+viper): drop-in list/state/switch (byte-compatible with Python v0.3.1 captures) plus flash/power/disk/--json/completions/--debug. Read-only commands never revive a powered-off SDWire3 (state answers from the hub-port cache); power is USB-free (drives the configured plug over LAN only — power cycle boots a target without disturbing the card); switch dut short-circuits when already target and auto-unmounts by default (--no-unmount to skip). Viper config at ~/.config/sdwire/config.yaml (SDWIRE_* env overrides) binds devices to power plugins, with location disambiguating identical serials. Rollout: .goreleaser.yaml + CI matrix; README fully rewritten (VBUS story, migration from the Python CLI incl. the anaconda PATH shadow, permissions, config, troubleshooting).
