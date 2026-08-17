package sdwire

import (
	"errors"
	"fmt"
	"slices"
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

// cachedDeviceStates reports the state of every hub-port cache entry whose
// port is not currently producing an enumerated SDWire (those are already
// covered by USB enumeration, and are passed in by location via attached).
// Ports are read with a status request only; nothing is powered on or off.
//
// An entry is skipped when its hub can't be opened — the topology has
// changed under it, so it says nothing useful about a device — or when its
// port has something connected, which by elimination is not an SDWire.
func cachedDeviceStates(ctx *gousb.Context, o *options, attached map[string]bool) ([]DeviceState, error) {
	cachePath, err := resolveHubCachePath(o)
	if err != nil {
		return nil, err
	}
	cache, err := hubpower.LoadCache(cachePath)
	if err != nil {
		return nil, err
	}

	var states []DeviceState
	for key, ref := range cache.All() {
		info := cacheEntryDeviceInfo(key, ref)
		if attached[info.Location()] {
			continue
		}

		st, err := readPortStatus(ctx, ref)
		if err != nil || st.Connected {
			continue
		}
		states = append(states, DeviceState{Info: info, Mode: statusToMode(st)})
	}

	// Cache iteration order is random; callers (and their golden output)
	// need a stable one.
	slices.SortFunc(states, func(a, b DeviceState) int {
		return strings.Compare(a.Info.Identity(), b.Info.Identity())
	})
	return states, nil
}

func readPortStatus(ctx *gousb.Context, ref *hubpower.PortRef) (hubpower.PortStatus, error) {
	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		return hubpower.PortStatus{}, err
	}
	defer port.Close()
	return port.Status()
}

// Revive power-cycles the hub port an SDWire3 is (or was) attached to and
// waits for it to re-enumerate: the software equivalent of unplugging the
// device and plugging it back in.
//
// It exists for a reader that has stopped answering and been torn off the
// bus by the OS — a state a USB port reset does not clear, and which leaves
// the device invisible to ListDevices, so no ordinary selector can reach
// it. The port is held dark for readerRevivePause rather than merely reset,
// since a crashed reader can latch up through a shorter interruption.
//
// selector may be:
//   - a location ("1-1.1.3"), resolved from live USB topology — the hub is
//     still there even when the device on it isn't, so this works with no
//     cache entry at all, or an ambiguous one;
//   - a serial or port-suffixed identity, matched against the hub-port cache;
//   - "", which requires the cache to hold exactly one entry.
//
// The revived device's info is returned, and its hub port re-cached under
// the identity it enumerated with.
func Revive(selector string, opts ...Option) (DeviceInfo, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	ref, err := revivePortRef(ctx, o, selector)
	if err != nil {
		return DeviceInfo{}, err
	}

	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		return DeviceInfo{}, err
	}
	defer port.Close()

	devPath := append(append([]int(nil), ref.HubPath...), ref.Port)
	if err := unmountBeforeRevive(ref.Bus, devPath); err != nil {
		return DeviceInfo{}, err
	}

	if err := revivePortPower(port); err != nil {
		return DeviceInfo{}, err
	}

	dev, err := waitForSDWireViaPort(ctx, port, ref.Bus, devPath, o.hostWaitTimeout)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("reviving the device on %s: %w", formatLocation(ref.Bus, devPath), err)
	}
	defer dev.Close()

	info := *describeDevice(dev)
	if err := cachePortRef(o, info.Identity(), ref); err != nil {
		o.warnFunc(fmt.Sprintf("caching hub port for %s: %v", info.Identity(), err))
	}
	return info, nil
}

// unmountBeforeRevive unmounts any volumes mounted from the reader at
// (bus, devPath) before its power is cut, on the same terms as
// SetMode(ModeTarget): a reader that can't be found has nothing mounted to
// lose and the revive proceeds, but a failed unmount aborts rather than
// yanking a mounted filesystem away. The usual case — a device wedged off
// the bus — has no block device to find.
func unmountBeforeRevive(bus int, devPath []int) error {
	ref := blockdevRef(DeviceInfo{Generation: GenerationSDWire3, Bus: bus, PortPath: devPath})
	path, err := blockdevFind(ref)
	if err != nil {
		return nil
	}
	if err := blockdevUnmount(path); err != nil {
		return fmt.Errorf("unmounting %s before cutting power to its hub port: %w", path, err)
	}
	return nil
}

// revivePortPath reports the bus and device port path a Revive selector
// names, when it is a location form ("1-1.1.3"). A bare Realtek serial is
// all digits, so it parses as a location with no port path — and only a
// path names a port to power-cycle, so those go to the cache instead.
func revivePortPath(selector string) (bus int, path []int, ok bool) {
	bus, path, ok = parseLocation(selector)
	return bus, path, ok && len(path) > 0
}

// revivePortRef resolves a Revive selector to the hub port to power-cycle.
// A location goes to live USB topology rather than the cache, which is what
// lets a revive work when the cache is empty, stale, or ambiguous — the
// cases a wedged device is most likely to leave behind.
func revivePortRef(ctx *gousb.Context, o *options, selector string) (*hubpower.PortRef, error) {
	if bus, path, ok := revivePortPath(selector); ok {
		return hubpower.ResolveParent(ctx, bus, path)
	}

	cachePath, err := resolveHubCachePath(o)
	if err != nil {
		return nil, err
	}
	cache, err := hubpower.LoadCache(cachePath)
	if err != nil {
		return nil, err
	}
	_, ref, err := selectCacheEntry(cache.All(), selector, cachePath)
	return ref, err
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
// that doesn't match, NewWithSerial (bare serial).
//
// When the serial matches several entries, that ambiguity is reported in
// preference to the identity lookup's "not found": a serial with several
// remembered ports is the real problem, and — since every SDWire3 ships
// with the same hardcoded Realtek serial — it names either one device that
// has been moved between sockets or two devices that genuinely share a
// serial. Neither is visible in "no SDWire device matching location ...".
func selectCacheEntry(entries map[string]*hubpower.PortRef, selector, cachePath string) (string, *hubpower.PortRef, error) {
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
		idx2, serialErr := selectBySerial(infos, selector)
		switch {
		case serialErr == nil:
			idx, err = idx2, nil
		case !errors.Is(serialErr, ErrNoDeviceFound):
			err = fmt.Errorf("%w. These are remembered hub ports, not devices seen now: "+
				"pick one, or delete %s to forget them all", serialErr, cachePath)
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

	key, ref, err := selectCacheEntry(entries, selector, cachePath)
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
