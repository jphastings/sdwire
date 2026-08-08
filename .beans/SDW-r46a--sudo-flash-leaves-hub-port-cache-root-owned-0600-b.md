---
# SDW-r46a
title: sudo flash leaves hub-port cache root-owned 0600, breaking unprivileged wedge revival
status: completed
type: bug
priority: normal
created_at: 2026-08-08T21:47:05Z
updated_at: 2026-08-08T21:53:40Z
---

Reported from a live bench session (2026-08-08): after `sudo sdwire flash`, the SDWire's reader wedged off the bus and the next *unprivileged* sdwire command errored fast instead of entering the patient hub-port revival path — the reader stayed dark until a manual replug.

## Root cause (verified in code)

1. `flash` is documented to run under sudo (cmd/sdwire/flash.go — raw disk writes need it), and every connect caches the resolved hub port: `newSDWire3Controller` → `cachePortRef` → `Cache.Save` (controller_sdwire3.go:68,86-97).
2. `Cache.Save` writes via `os.CreateTemp` (hubpower/cache.go:106), which hardcodes mode 0600, then renames into place. Under sudo the result is `hubports.json` owned root:0600 in the *user's* cache dir (sudo preserved $HOME here).
3. `LoadCache` (hubpower/cache.go:36-41) only forgives `IsNotExist`; EACCES is a hard error.
4. So `connect`'s no-device fallback (`tryCacheFallback`, called at sdwire.go:242-248) fails at `LoadCache` before ever reaching `revivePortPower`/`waitForSDWireViaPort` — the 5s-dark-time power-cycle + patient re-enumeration wait never runs, exactly when a wedged reader needs it. `CachedPortState` (fallback.go:243) breaks the same way, so `sdwire state` errors too.

## Also broken by the same pattern

- First-ever run under sudo: `MkdirAll` (cache.go:102) creates `~/Library/Caches/sdwire` root-owned, so later unprivileged `Save` can't even create its temp file (caching then silently warns forever, controller_sdwire3.go:68-70).
- If sudo *resets* $HOME (config-dependent), root runs write a separate cache under /var/root and the user-path cache silently goes stale. Secondary; not what was observed.

## Fix sketch

Save-side is the real fix — the revival path needs the *data*, so a merely tolerant load (treat EACCES as empty) would still leave nothing to revive:

- [x] `Cache.Save`: chmod the temp file 0644 before rename; when running as root with `SUDO_UID`/`SUDO_GID` set, chown temp file (and the cache dir if just created) back to the invoking user.
- [x] Consider resolving the cache path via the sudo-invoking user's home when root, so HOME-resetting sudo configs share one cache.
- [x] Load-side hardening: wrap EACCES in an actionable message naming the path and the likely `sudo` cause (a fast clear error beats a fast baffling one if a stale root-owned file predates the fix).
- [x] Behavioral tests: Save produces a world-readable file; a cache written by another uid remains loadable end-to-end where testable.

Immediate bench remediation (outside the repo): `sudo rm ~/Library/Caches/sdwire/hubports.json` if a root-owned copy still exists — as of filing, that dir is empty again, so next unprivileged run will rebuild it.

## Summary of Changes

All in `hubpower/cache.go` (+ tests in `cache_test.go`):

- `Cache.Save` chmods its temp file to 0644 before renaming into place, and best-effort chowns it (via new `restoreSudoOwnership`) back to the `SUDO_UID`/`SUDO_GID` user when running as root under sudo. Directory components that `MkdirAll` is about to create are recorded (`missingDirComponents`) and chowned the same way, so a first-ever sudo run no longer leaves a root-owned cache dir.
- `DefaultCachePath` resolves the sudo *invoker's* platform cache dir (`~/Library/Caches` on darwin, `~/.cache` elsewhere; via `os/user.LookupId`) when running as root under sudo, so HOME-resetting sudo configs share one cache with unprivileged runs. Falls back to `os.UserCacheDir()` as before; no-op on Windows.
- `LoadCache` wraps permission errors with an actionable message naming the path and the likely earlier-sudo-run cause. (Deliberately still an error, not treated as empty: an "empty" cache would silently disable revival, and the unreadable file is fixable.)
- New behavioral tests: `Save` leaves the other-read bit set; `LoadCache` on an unreadable file names the path and gives the sudo hint (skipped on Windows / as root). Actual chown/uid switching isn't testable unprivileged.
