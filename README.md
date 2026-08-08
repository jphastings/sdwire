# sdwire

[![Go Reference](https://pkg.go.dev/badge/github.com/jphastings/sdwire.svg)](https://pkg.go.dev/github.com/jphastings/sdwire)
[![Go Report Card](https://goreportcard.com/badge/github.com/jphastings/sdwire)](https://goreportcard.com/report/github.com/jphastings/sdwire)

A Go SDK, and a CLI built on it, for controlling **SDWireC** and **SDWire3**
devices — USB-controlled SD card multiplexers that switch a single SD card
between a host computer (for flashing images) and a target device under test
(for booting them), without physically re-seating the card.

This is a fork of [github.com/fcjr/sdwire](https://github.com/fcjr/sdwire),
rewritten with SDWire3 support, an honest `Mode()` readback, a hub-power
fallback for SDWire3s that are switched off, and this CLI.

## The SDWire3 VBUS story

Unlike SDWireC (which switches instantly via FTDI CBUS bits), an SDWire3
never hands the SD card to the target while its own USB link stays up — the
kernel-driver detach/reset trick that older tooling uses for it does not
actually move the mux. What *does* work is depriving the SDWire3 of power
altogether: cutting `PORT_POWER` on its **upstream USB hub port** drops it
off the bus entirely, and the now-unpowered mux passes the card straight
through to the target (observed in about a second); restoring port power
re-enumerates the reader at its power-on default — card connected to the
host — with the resulting block device typically appearing some 6 seconds
later. That means two things in practice: an SDWire3 **must sit behind an
external USB hub that supports independent per-port power switching** (a
device plugged into a root port, or behind a ganged-power hub, can't be
fully switched), and because a target board normally only probes its SD
slot at boot or on a card-detect edge, **the target must be rebooted or
power-cycled after switching to target mode** before it will notice the
card arrived.

## Install

### CLI

```bash
go install github.com/jphastings/sdwire/cmd/sdwire@latest
```

Or download a prebuilt binary from the
[releases page](https://github.com/jphastings/sdwire/releases) (built via
GoReleaser/CI for darwin, linux, and windows, amd64 and arm64).

### SDK

```bash
go get github.com/jphastings/sdwire
```

## CLI usage

`sdwire` is a drop-in replacement for the `list`, `state`, and `switch`
commands of the [Python `sdwire-cli`](https://github.com/Badger-Embedded/sdwire-cli)
(see [Migrating from the Python CLI](#migrating-from-the-python-cli)
below), plus new commands for flashing, power control, and scripting.

Every command accepts `-s/--serial` to select which attached device to
operate on: a plain USB serial, the port-suffixed identity form
(`20120501030900000.1.1.3`), a USB location (`1-1.1.3`), or a device name
from your [config file](#config-file). With no `-s` and no `default_device`
configured, commands that need a single device use the sole attached
device, or error out listing every attached device's identity if more than
one is found.

### `sdwire list`

List every attached SDWire device: its identity, USB product info, and the
reader's resolved block device path (or `None` if it can't currently be
found — e.g. the device is switched to target mode).

```
$ sdwire list
Serial                        Product Info		Block Dev
20120501030900000.1.1.3       [0bda::0316]		/dev/disk4
```

### `sdwire state`

Print which side the selected device's SD card is currently connected to:
`Host`, `Target`, or `Unknown`. When the device is attached and powered on,
this is an honest live readback via the device — see
[SDWire3 state semantics](#sdwire3-state-semantics) below for how a
powered-off (target mode) SDWire3 is handled instead.

```
$ sdwire state
Serial                        	State
20120501030900000.1.1.3       	Host
```

### `sdwire switch {dut|target|host|ts|off}`

Switch the selected device's SD card. `dut`/`target` connect it to the
target board; `host`/`ts` connect it to this computer. `off` is rejected
with an explanation: SDWireC has no third state, and for SDWire3 "powering
the port off" is literally how target mode is implemented — it does not
mean disconnected from both sides. Prints nothing and exits `0` on success.

```bash
sdwire switch host              # connect the card to this computer
sdwire switch target -s bench   # connect the card to the "bench" device's target
```

### `sdwire flash <image>`

Write an image to the selected device's SD card and boot the target from
it: powers the target off (if a [power plugin](#power-plugins) is
configured for the device), switches to host mode, raw-writes the image
with progress on stderr, switches back to target mode, then powers the
target back on. **Raw disk writes need elevated privileges** — run with
`sudo` on macOS/Linux, or as Administrator on Windows.

```bash
sudo sdwire flash ./ubuntu-24.04-preinstalled.img.xz -s bench
```

```
flashed 1234 / 3800 MiB (32%)
```

### `sdwire power {on|off|cycle}`

Drive the [power plugin](#power-plugins) configured for the selected
device. `on`/`off` set target power directly; `cycle` powers off, waits at
least `min_off_seconds`, then powers back on. This never touches the
SDWire's USB connection — it's the normal way to boot a target whose SD
card is already switched to it. If the device has no power plugin
configured, this prints a ready-to-copy YAML snippet for your config file
and exits `1`.

### `sdwire disk`

Print just the selected device's resolved block device path — nothing
else — for use in scripts:

```bash
sudo dd if=image.img of=$(sdwire disk -s bench) bs=4M status=progress
```

Exits non-zero if the block device can't currently be found.

### `--json`

`list`, `state`, and `disk` accept `--json` for machine-readable output:

```bash
$ sdwire list --json
[
  {
    "serial": "20120501030900000",
    "identity": "20120501030900000.1.1.3",
    "location": "1-1.1.3",
    "product": "USB3.0-CRW",
    "generation": "SDWire3",
    "block_dev": "/dev/disk4"
  }
]

$ sdwire state --json
{ "identity": "20120501030900000.1.1.3", "state": "Host" }

$ sdwire disk --json
{ "block_dev": "/dev/disk4" }
```

### `--debug` and warnings

SDK warnings (for example, an SDWire3 sitting behind a hub that only
switches port power in a ganged fashion, affecting sibling ports) always
print to stderr, whether or not `--debug` is set. `--debug` adds further
diagnostics — the resolved config path, which device a selector matched,
and so on.

### Shell completion

```bash
sdwire completion bash|zsh|fish|powershell
```

See `sdwire completion --help` (and each shell's subcommand `--help`) for
how to load the generated script.

### Exit codes

`0` on success, `1` for an operational failure (device not found, config
error, flash failure, ...), `2` for a CLI usage error (bad flags, unknown
subcommand or argument).

## Config file

Path: `~/.config/sdwire/config.yaml` on every OS (not
`os.UserConfigDir()`'s platform-specific location — macOS's `~/Library/
Application Support` in particular — this project deliberately uses one
fixed XDG-style path everywhere). Override with `--config <path>` or the
`SDWIRE_CONFIG` environment variable. Individual keys are also overridable
via `SDWIRE_`-prefixed environment variables (e.g. `SDWIRE_DEFAULT_DEVICE`,
with `.` replaced by `_` for nested keys).

```yaml
default_device: bench
devices:
  bench:
    serial: "20120501030900000"        # or the port-suffixed identity form
    location: "1-1.1.3"                # optional; disambiguates identical Realtek serials
    power:
      type: meross
      ip: 192.168.1.112
      key: "<meross account key>"
      # channel: 0                     # optional
min_off_seconds: 8
```

- `default_device` is used whenever `-s/--serial` isn't given.
- Each entry under `devices` names a device you can pass to `-s`; its
  `location` is preferred over `serial` when selecting the device (more
  specific — needed because every Realtek SDWire3 reader shares the same
  hardcoded USB serial number), but either alone is enough.
- `power` configures a power plugin for that device — see below. A device
  with no `power` section works fine for `list`/`state`/`switch`/`flash`;
  only `sdwire power` requires one.
- `min_off_seconds` (default `8`) is the minimum dark time `sdwire power
  cycle` and `sdwire flash` hold target power off for.

No config file at all is a fully supported setup: with exactly one SDWire
attached, `list`, `state`, `switch`, `flash`, and `disk` all work without
any configuration; `power` will explain what to add.

### Power plugins

The CLI ships one registered power plugin type, `meross` (for Meross smart
plugs — see [`power/meross/README.md`](power/meross/README.md) for how to
get your Meross account key). The SDK itself (`sdwire.PowerFunc`) is
plugin-agnostic; this registry, and the config wiring around it, lives in
the CLI. `sdwire power on|off|cycle` with an unrecognized `power.type`
lists the registered types in its error.

## Migrating from the Python CLI

`sdwire list`, `sdwire state`, and `sdwire switch` match the
[Python `sdwire-cli`](https://github.com/Badger-Embedded/sdwire-cli) v0.3.1's
output byte-for-byte, so scripts built against it should work unchanged
once this binary is what `sdwire` on your `PATH` resolves to. `flash`,
`power`, `disk`, `--json`, and `--debug` are new.

**PATH precedence.** If you already have the Python CLI installed (e.g. via
`pip`), check which one `PATH` finds first:

```bash
which -a sdwire
```

`go install` puts this binary at `$(go env GOPATH)/bin/sdwire` (typically
`~/go/bin/sdwire`); make sure that directory precedes wherever the Python
version lives (often an Anaconda/Miniconda `bin` directory, or a `pip`
`--user` install path) in `PATH`, or `pip uninstall sdwire` the Python one.

#### SDWire3 state semantics

For SDWire3, a device currently in target mode is, physically, **powered
off** — it isn't enumerable on USB at all. `sdwire state` never powers a
device on to answer: when there's nothing live to read, it falls back to
the on-disk hub-port cache and a direct hub port-status read, which is
enough to report `Target` honestly without side effects. `sdwire switch
dut`/`target` does the same check before doing any work, so switching a
device that's already in target mode is also a no-op. Only commands that
need the SD card to actually move data — `switch host`/`ts` and `flash` —
revive a powered-off SDWire3 via the cache, since restoring power is
inherent to what those commands do.

## Permissions

**Flashing** needs raw block device access: run `sdwire flash` (or any
direct write to the path from `sdwire disk`) with `sudo` on macOS/Linux, or
as Administrator on Windows.

**Linux udev rules.** Create `/etc/udev/rules.d/99-sdwire.rules`:

```
# SDWireC (FTDI)
SUBSYSTEM=="usb", ATTR{idVendor}=="04e8", ATTR{idProduct}=="6001", MODE="0666"
# SDWire3 (Realtek reader)
SUBSYSTEM=="usb", ATTR{idVendor}=="0bda", ATTR{idProduct}=="0316", MODE="0666"
# SDWire3 switching also needs write access to its upstream hub, since the
# card is handed to the target by cutting that hub port's power (VBUS).
# Either run as root, or grant access to hub devices:
SUBSYSTEM=="usb", ATTR{bDeviceClass}=="09", MODE="0666"
```

Then reload rules and add your user to `plugdev`:

```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
sudo usermod -a -G plugdev $USER
```

**Windows.** SDWire3 switching needs libusb access to the upstream hub, not
just the reader — install [UsbDk](https://github.com/daynix/UsbDk), or bind
the hub to WinUSB with [Zadig](https://zadig.akeo.ie/).

## SDK quick-start

```go
package main

import (
	"context"
	"log"

	"github.com/jphastings/sdwire"
	"github.com/jphastings/sdwire/power/meross"
)

func main() {
	powerFunc, err := meross.New("192.168.1.112", "your-meross-account-key")
	if err != nil {
		log.Fatal(err)
	}

	dev, err := sdwire.New(
		sdwire.WithTargetPower(powerFunc),
		sdwire.WithWarningHandler(func(msg string) { log.Println("warning:", msg) }),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()

	if err := dev.FlashAndBoot(context.Background(), "./image.img",
		sdwire.WithFlashProgress(func(written, total int64) {
			log.Printf("flashed %d / %d bytes", written, total)
		}),
	); err != nil {
		log.Fatal(err)
	}

	mode, err := dev.Mode()
	if err != nil {
		log.Fatal(err)
	}
	log.Println("now in mode:", mode)
}
```

Other entry points: `ListDevices()` enumerates every attached device;
`NewWithSerial`/`NewWithIdentity` connect to a specific one (by bare
serial, or by the port-suffixed identity/location forms respectively —
identical to what the CLI's `-s` flag accepts); `SetMode`/`Mode` switch and
read back the card's side; `PowerCycle` and `TargetPower` drive a
configured `PowerFunc` directly, independent of flashing;
`WithoutRevive()` disables the hub-cache power-on fallback for callers that
must not risk switching a target-mode SDWire3 back to host mode;
`CachedPortState` reads an SDWire3's mode from the on-disk hub cache
without powering anything on or off at all, for exactly that case.

## Supported operating systems

- **Linux** — tested on Ubuntu, Debian
- **macOS** — tested on macOS 10.15+ (uses `ioreg`/`diskutil` for block
  device discovery)
- **Windows** — tested on Windows 10+ (needs libusb access to the SDWire3's
  hub — see [Permissions](#permissions))

## Troubleshooting

**Device not found.** Confirm it's actually attached and enumerating:
SDWireC shows up as USB `04e8:6001`, SDWire3 as `0bda:0316`. On Linux,
check the udev rules above; on Windows, check libusb/UsbDk binding.

**SDWire3 stuck in target mode.** This is expected, not broken — see
[SDWire3 state semantics](#sdwire3-state-semantics). `sdwire switch
host`/`ts` and `sdwire flash` (and, in the SDK, `New`/`NewWithSerial`/
`NewWithIdentity` by default) revive it via the on-disk hub-port cache,
powering it back on into host mode; `state` and `power` never do.

**"device is attached to a root hub port" / a `hubpower.ErrRootPort`-shaped
error.** SDWire3 switching works by cutting power to the device's
*upstream hub port*; a device plugged directly into a computer's built-in
(root) USB port has no controllable parent to do that through — see
[the VBUS story](#the-sdwire3-vbus-story) above. Attach it behind an
external hub with independent per-port power switching instead.

## Contributing

Contributions are welcome — please open an issue first for anything more
than a small fix.

## License

MIT License — see [LICENSE](LICENSE).

## Related projects

- [fcjr/sdwire](https://github.com/fcjr/sdwire) — the original Go library
  this project is forked from
- [Badger-Embedded/sdwire-cli](https://github.com/Badger-Embedded/sdwire-cli) —
  the Python CLI this project's `list`/`state`/`switch` commands are
  compatible with
- [fcjr/ykush3](https://github.com/fcjr/ykush3) — a Go library for YKUSH3
  USB switches

---

Forked and extended by [@jphastings](https://github.com/jphastings).
Originally made with ❤️ at the [Recurse Center](https://www.recurse.com/).
