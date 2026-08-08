package main

import (
	"strings"

	"github.com/jphastings/sdwire"
	"github.com/jphastings/sdwire/internal/blockdev"
)

// Indirections over the SDK's device-touching entry points, swapped out in
// tests so command logic can be exercised without real hardware.
var (
	sdwireListDevices     = sdwire.ListDevices
	sdwireNewWithIdentity = sdwire.NewWithIdentity
	sdwireNewWithSerial   = sdwire.NewWithSerial
	sdwireNew             = sdwire.New
	sdwireCachedPortState = sdwire.CachedPortState
	blockdevFind          = blockdev.Find
)

// selection describes how a -s/--serial value (or a config's
// default_device) should be turned into a device to connect to.
type selection struct {
	// selector is what to hand to connectSelector. Meaningless if !named.
	selector string
	// powerCfg is the matched device's "power:" config, if any.
	powerCfg map[string]any
	// deviceName is the config key that was matched, or "" if selector
	// came from a literal (non-config-name) value.
	deviceName string
	// named is false when neither -s nor default_device supplied a value,
	// meaning the caller must fall back to enumerating attached devices.
	named bool
}

// resolveSelection decides what -s/--serial (or, if empty, cfg's
// default_device) refers to: a configured device name (whose location is
// preferred over its serial, being the more specific selector — see the
// config file's "location" field doc), or a literal serial/identity/
// location to use as-is. It never touches hardware.
func resolveSelection(serialFlag string, cfg *Config) selection {
	name := serialFlag
	if name == "" && cfg != nil {
		name = cfg.DefaultDevice
	}
	if name == "" {
		return selection{}
	}

	if cfg != nil {
		if dev, ok := cfg.Devices[name]; ok {
			selector := dev.Location
			if selector == "" {
				selector = dev.Serial
			}
			return selection{selector: selector, powerCfg: dev.Power, deviceName: name, named: true}
		}
	}

	return selection{selector: name, named: true}
}

// connectSelector connects to the device identified by selector: a plain
// serial, a port-suffixed identity ("<serial>.<path...>"), or a location
// ("<bus>-<path...>"). Location and suffixed-identity forms are routed
// through NewWithIdentity; a bare serial (no "." or "-") is routed through
// NewWithSerial instead, since NewWithIdentity's location-form parsing
// would otherwise misread a bare numeric serial as a bus number — see its
// doc comment.
func connectSelector(selector string, opts ...sdwire.Option) (*sdwire.SDWire, error) {
	if strings.ContainsAny(selector, ".-") {
		return sdwireNewWithIdentity(selector, opts...)
	}
	return sdwireNewWithSerial(selector, opts...)
}

// identitiesOf returns info.Identity() for each device, for error messages
// listing candidates.
func identitiesOf(infos []*sdwire.DeviceInfo) []string {
	ids := make([]string, len(infos))
	for i, info := range infos {
		ids[i] = info.Identity()
	}
	return ids
}

// openResult bundles a connected device with the config context it was
// resolved from.
type openResult struct {
	sw         *sdwire.SDWire
	powerCfg   map[string]any
	deviceName string
	selector   string
}

// openSelected resolves serialFlag/cfg.DefaultDevice to a device and
// connects to it: via the matched config device's selector, the literal
// -s/--serial value, or — if neither is set — the sole attached SDWire
// device (erroring, and listing candidates, if more than one is attached).
//
// revive controls whether the SDK is allowed to power on a cached-but-not-
// currently-attached SDWire3 to satisfy the connection: when false,
// sdwire.WithoutRevive() is passed so a device that's genuinely off the
// bus (e.g. an SDWire3 sitting in target mode) surfaces as a not-found
// error instead of being silently switched back to host mode. It also
// governs the zero-attached-devices case for the unnamed (no -s, no
// default_device) path: with revive, sdwireNew is called directly so the
// SDK's own cache fallback gets a chance to revive the sole cached device;
// without it, this is the same "no SDWire devices found" error as always.
func openSelected(serialFlag string, cfg *Config, revive bool, opts ...sdwire.Option) (*openResult, error) {
	sel := resolveSelection(serialFlag, cfg)
	selector := sel.selector

	if !revive {
		opts = append(opts, sdwire.WithoutRevive())
	}

	if !sel.named {
		infos, err := sdwireListDevices()
		if err != nil {
			return nil, opErrf("listing devices: %w", err)
		}
		switch len(infos) {
		case 0:
			if revive {
				sw, err := sdwireNew(opts...)
				if err != nil {
					return nil, opErrf("connecting to device: %w", err)
				}
				return &openResult{sw: sw, powerCfg: sel.powerCfg, deviceName: sel.deviceName, selector: sw.Info().Identity()}, nil
			}
			return nil, opErrf("no SDWire devices found: %w", sdwire.ErrNoDeviceFound)
		case 1:
			selector = infos[0].Identity()
		default:
			return nil, opErrf("multiple SDWire devices attached; specify one with -s/--serial: %s", strings.Join(identitiesOf(infos), ", "))
		}
	}

	sw, err := connectSelector(selector, opts...)
	if err != nil {
		return nil, opErrf("connecting to device %q: %w", selector, err)
	}

	return &openResult{sw: sw, powerCfg: sel.powerCfg, deviceName: sel.deviceName, selector: selector}, nil
}

// blockdevRefFor builds the blockdev.Ref identifying an SDWire's own reader
// device from its DeviceInfo, mirroring the SDK's internal (unexported)
// flash.go:blockdevRef.
func blockdevRefFor(info sdwire.DeviceInfo) blockdev.Ref {
	vendor, product := uint16(sdwire.SDWireCVID), uint16(sdwire.SDWireCPID)
	if info.Generation == sdwire.GenerationSDWire3 {
		vendor, product = uint16(sdwire.SDWire3VID), uint16(sdwire.SDWire3PID)
	}
	return blockdev.Ref{
		Vendor:   vendor,
		Product:  product,
		Bus:      info.Bus,
		PortPath: info.PortPath,
	}
}

// resolveBlockDev returns the block device path for info's reader, or ""
// if it can't currently be found (e.g. the device is in target mode).
func resolveBlockDev(info sdwire.DeviceInfo) string {
	path, err := blockdevFind(blockdevRefFor(info))
	if err != nil {
		return ""
	}
	return path
}
