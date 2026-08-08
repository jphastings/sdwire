package sdwire

import (
	"errors"
	"fmt"
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

// waitForSDWireAt polls, at 250ms intervals, for an SDWire3 to enumerate at
// a given bus and USB port path, returning its opened Device. It gives up
// once timeout has elapsed.
func waitForSDWireAt(ctx *gousb.Context, bus int, path []int, timeout time.Duration) (*gousb.Device, error) {
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
		sleep(250 * time.Millisecond)
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

// powerOnAndWait powers on the hub port described by ref and waits for an
// SDWire3 to reappear at its location.
func powerOnAndWait(ctx *gousb.Context, o *options, ref *hubpower.PortRef) (*gousb.Device, error) {
	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer port.Close()

	if err := port.SetPower(true); err != nil {
		return nil, err
	}

	devPath := append(append([]int(nil), ref.HubPath...), ref.Port)
	return waitForSDWireAt(ctx, ref.Bus, devPath, o.hostWaitTimeout)
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
