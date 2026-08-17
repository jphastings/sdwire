---
# SDW-q3z3
title: libusb asserts and aborts the process when list races a device that is still enumerating
status: todo
type: bug
priority: normal
created_at: 2026-08-17T03:08:01Z
updated_at: 2026-08-17T03:08:01Z
---

Running `sdwire list` immediately after a hub-port power event — most
easily reproduced right after `sdwire revive` — can abort the whole process
inside libusb's macOS backend:

```
Assertion failed: (cached_device->address <= UINT8_MAX), function process_new_device, file darwin_usb.c, line 1499.
SIGABRT: abort
signal arrived during cgo execution
...
github.com/google/gousb._Cfunc_libusb_init
github.com/jphastings/sdwire.ListDeviceStates
```

Observed once on 2026-08-17 with libusb 1.0.30 and gousb v1.1.3, seconds
after a successful `sdwire revive -s 1-1.1.3`. Re-running `list` twice
immediately afterwards worked fine, so it is a race against a device that
is still enumerating, not a persistent state.

It fires in `libusb_init` — before any of this project's code runs — so
there is nothing to check or guard: a failed C `assert` calls `abort`, which
Go cannot recover from. The user sees a 100-line Go crash dump for what is
really "try again in a second", which reads as a far more serious failure
than it is.

Worth noting this is the same window the SDK already treats as dangerous:
`waitForSDWireViaPort` polls hub port status rather than re-enumerating,
precisely because "aggressive libusb enumeration opens devices mid-bring-up
and has been observed on the bench to wedge the Realtek reader". That
mitigation protects the SDK's own wait loop; it can't protect a separate
process the operator runs a second later.

## Options

- **Upstream.** Check whether current libusb still carries the assertion,
  and whether it is already reported. Per the repo's contribution rules, an
  upstream patch does not get opened without JP's say-so — write it here
  and let him decide.
- **Pin the diagnosis, not the crash.** The dump can't be prevented, but the
  README's troubleshooting section can name the assertion so the next person
  finds "re-run it" in seconds rather than filing a bug against this tool.
- **Reduce the exposure.** `revive` already waits `enumerationSettle` before
  opening the device it revived; it could also hold that settle *before
  returning*, so a shell one-liner like `sdwire revive && sdwire list` isn't
  racing the tail of enumeration. That narrows the window without pretending
  to close it.

## Todo

- [ ] Confirm whether libusb still asserts here, and whether it's reported upstream
- [ ] README troubleshooting entry naming the assertion and the "just re-run it" fix
- [ ] Consider settling after `revive` returns, so `revive && list` doesn't race
