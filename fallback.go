package sdwire

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/gousb"
	"github.com/jphastings/sdwire/hubpower"
)

// resolveHubCachePath returns the hub-port cache path to use: the
// option-configured override if set, otherwise hubpower.DefaultCachePath().
func resolveHubCachePath(o *options) (string, error) {
	if o.hubCachePath != "" {
		return o.hubCachePath, nil
	}
	return hubpower.DefaultCachePath()
}

// waitForSDWireViaPort waits for the hub to report a device connected on
// port, lets enumeration settle, then opens the SDWire3 at (bus, path).
// It polls the hub port's status — a control request handled entirely by
// the hub — rather than re-enumerating the bus, because aggressive libusb
// enumeration opens devices mid-bring-up and has been observed on the
// bench to wedge the Realtek reader (after which the hub can cut the
// port's power protectively).
func waitForSDWireViaPort(ctx *gousb.Context, port *hubpower.Port, bus int, path []int, timeout time.Duration) (*gousb.Device, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := port.Status()
		if err == nil && st.Connected {
			break
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("no device connected on the SDWire3's hub port within %s", timeout)
		}
		sleep(500 * time.Millisecond)
	}

	sleep(enumerationSettle)
	return openSDWireAt(ctx, bus, path, timeout)
}

// openSDWireAt opens the SDWire3 enumerated at (bus, path), retrying gently
// (1s apart, up to timeout) since the hub reports a connection before the
// OS finishes enumerating the device.
func openSDWireAt(ctx *gousb.Context, bus int, path []int, timeout time.Duration) (*gousb.Device, error) {
	deadline := time.Now().Add(timeout)
	for {
		devs, _ := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
			return desc.Bus == bus && desc.Vendor == SDWire3VID && desc.Product == SDWire3PID && intsEqual(desc.Path, path)
		})
		if len(devs) > 0 {
			closeDevices(devs[1:])
			return devs[0], nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("no SDWire3 device appeared at bus %d path %v within %s", bus, path, timeout)
		}
		sleep(time.Second)
	}
}

// cacheEntryDeviceInfo reconstructs enough of a DeviceInfo from a hubpower
// cache key (an Identity() string) and its PortRef to run it through the
// existing serial/identity matching helpers in identity.go.
func cacheEntryDeviceInfo(key string, ref *hubpower.PortRef) DeviceInfo {
	serial, path, ok := splitIdentity(key)
	if !ok {
		path = append(append([]int(nil), ref.HubPath...), ref.Port)
	}
	return DeviceInfo{
		Serial:     serial,
		Generation: GenerationSDWire3,
		Bus:        ref.Bus,
		PortPath:   path,
	}
}

// readerRevivePause is how long a hub port is held off before re-powering
// when reviving a device that should be there but isn't. A crashed SD
// reader can latch up through a too-brief interruption (its capacitors
// don't drain); 5s has been verified reliable on the bench where 2s was
// marginal.
const readerRevivePause = 5 * time.Second

// enumerationSettle is how long to wait between the hub reporting a device
// connected and opening it, giving the OS time to finish enumeration.
const enumerationSettle = 3 * time.Second

// revivePortPower delivers a fresh power-on to a hub port's device. A port
// that is already powered (but whose device is absent or wedged) is
// power-cycled with readerRevivePause of dark time; an unpowered port is
// simply switched on.
func revivePortPower(port *hubpower.Port) error {
	if st, err := port.Status(); err == nil && st.Powered {
		if err := port.SetPower(false); err != nil {
			return err
		}
		sleep(readerRevivePause)
	}
	return port.SetPower(true)
}

// powerOnAndWait powers on (or, if needed, power-cycles) the hub port
// described by ref and waits for an SDWire3 to reappear at its location.
func powerOnAndWait(ctx *gousb.Context, o *options, ref *hubpower.PortRef) (*gousb.Device, error) {
	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer port.Close()

	if err := revivePortPower(port); err != nil {
		return nil, err
	}

	devPath := append(append([]int(nil), ref.HubPath...), ref.Port)
	return waitForSDWireViaPort(ctx, port, ref.Bus, devPath, o.hostWaitTimeout)
}

// tryCacheFallback consults the hubpower on-disk cache for a previously
// seen (and possibly now powered-off) SDWire3 matching the given
// selection, powers its hub port back on, and waits for it to reappear.
// Cache entries whose hub is present but no longer matches the recorded
// vendor/product (hubpower.ErrHubMismatch — a stale entry) are removed.
func tryCacheFallback(ctx *gousb.Context, o *options, matches func(key string, ref *hubpower.PortRef) bool) (*gousb.Device, DeviceInfo, error) {
	cachePath, err := resolveHubCachePath(o)
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	cache, err := hubpower.LoadCache(cachePath)
	if err != nil {
		return nil, DeviceInfo{}, err
	}

	lastErr := errors.New("no cached hub port produced a matching SDWire device")
	for key, ref := range cache.All() {
		if !matches(key, ref) {
			continue
		}

		dev, err := powerOnAndWait(ctx, o, ref)
		if err != nil {
			if errors.Is(err, hubpower.ErrHubMismatch) {
				cache.Delete(key)
				if saveErr := cache.Save(cachePath); saveErr != nil {
					o.warnFunc(fmt.Sprintf("removing stale hub cache entry %q: %v", key, saveErr))
				}
			}
			lastErr = err
			continue
		}
		return dev, *describeDevice(dev), nil
	}
	return nil, DeviceInfo{}, lastErr
}

// statusToMode maps a hub port's live power/connection status to the
// SwitchMode it implies for an SDWire3: an unpowered port means the reader
// is off the bus entirely and the mux has handed the SD card to the
// target; a powered, connected port means the reader has (re-)enumerated
// on the host; a powered but not-yet-connected port is a brief
// transitional state (e.g. mid re-enumeration), reported honestly as
// ModeUnknown rather than guessed at.
func statusToMode(st hubpower.PortStatus) SwitchMode {
	switch {
	case !st.Powered:
		return ModeTarget
	case st.Connected:
		return ModeHost
	default:
		return ModeUnknown
	}
}

// selectCacheEntry picks a single cache entry out of entries: selector ==
// "" requires exactly one entry (returning an error listing every cached
// identity otherwise); a non-empty selector is matched using the same
// rules as NewWithIdentity (suffixed identity or location form) and, if
// that doesn't match, NewWithSerial (bare serial) — whichever succeeds is
// used, and if both fail the identity-match error is returned.
func selectCacheEntry(entries map[string]*hubpower.PortRef, selector string) (string, *hubpower.PortRef, error) {
	keys := make([]string, 0, len(entries))
	infos := make([]DeviceInfo, 0, len(entries))
	for key, ref := range entries {
		keys = append(keys, key)
		infos = append(infos, cacheEntryDeviceInfo(key, ref))
	}

	if selector == "" {
		if len(keys) == 1 {
			return keys[0], entries[keys[0]], nil
		}
		identities := make([]string, len(infos))
		for i, info := range infos {
			identities[i] = info.Identity()
		}
		return "", nil, fmt.Errorf("multiple cached SDWire devices; specify one with a selector: %s", strings.Join(identities, ", "))
	}

	idx, err := selectByIdentity(infos, selector)
	if err != nil {
		if idx2, err2 := selectBySerial(infos, selector); err2 == nil {
			idx, err = idx2, nil
		}
	}
	if err != nil {
		return "", nil, err
	}
	return keys[idx], entries[keys[idx]], nil
}

// CachedPortState reports an SDWire3's mode purely from the on-disk hub
// cache and a hub port-status read, WITHOUT powering anything on or off —
// safe for a device that is currently powered off (in target mode). This
// is the read-only counterpart to New's cache-revive fallback: it never
// causes an SDWire3 sitting in target mode to switch to host mode as a
// side effect of being asked about it.
//
// selector may be "" (which requires exactly one cached entry, erroring
// and listing candidates otherwise), or a serial / port-suffixed identity
// / location matched with the same rules as NewWithSerial / NewWithIdentity.
//
// Alongside the mode, CachedPortState returns the matched cache entry's
// identity (its DeviceInfo.Identity() form) — useful for callers (e.g. the
// sdwire CLI) that need to display which device they read state for. The
// returned error is nil unless the cache itself couldn't be read, no entry
// matched selector, or opening the hub / reading its port status failed
// (which is expected for a genuinely stale or unreachable cache entry).
func CachedPortState(selector string, opts ...Option) (SwitchMode, string, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	cachePath, err := resolveHubCachePath(o)
	if err != nil {
		return ModeUnknown, "", err
	}
	cache, err := hubpower.LoadCache(cachePath)
	if err != nil {
		return ModeUnknown, "", err
	}

	entries := cache.All()
	if len(entries) == 0 {
		return ModeUnknown, "", errors.New("hub-port cache is empty; no SDWire device has been seen yet")
	}

	key, ref, err := selectCacheEntry(entries, selector)
	if err != nil {
		return ModeUnknown, "", err
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		return ModeUnknown, key, err
	}
	defer port.Close()

	st, err := port.Status()
	if err != nil {
		return ModeUnknown, key, err
	}

	return statusToMode(st), key, nil
}
