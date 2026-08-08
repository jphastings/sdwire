// Package hubpower controls USB hub downstream port power (VBUS) over
// gousb. It exists because the SDWire3 SD card multiplexer only hands the
// SD card to its target while its own upstream USB link is down: cutting
// power to the SDWire3's hub port drops it off the bus and releases the
// card to the target, and restoring power brings it back to the host.
package hubpower

import (
	"errors"
	"fmt"

	"github.com/google/gousb"
)

// ErrRootPort indicates a device is attached directly to a root hub port.
// Root hub ports have no controllable parent hub port: libusb often cannot
// control root hubs at all, and on macOS they are not real, addressable
// hubs. Attach the device behind an external USB hub to control its power.
var ErrRootPort = errors.New("device is attached to a root hub port; attach it behind an external USB hub to control its power")

// ErrHubMismatch indicates the hub found at a PortRef's recorded location
// no longer has the vendor/product recorded when the reference was cached
// — the USB topology changed, or a different hub is now attached there.
var ErrHubMismatch = errors.New("hub at cached location has a different vendor/product than expected")

// PortRef identifies a single downstream port on a USB hub. It is
// JSON-serializable so it can be persisted (see Cache) while the device
// attached to it is powered off, and therefore invisible to USB
// enumeration.
type PortRef struct {
	// Bus is the USB bus the hub is enumerated on.
	Bus int `json:"bus"`
	// HubPath is the hub's own USB port path (gousb DeviceDesc.Path).
	HubPath []int `json:"hub_path"`
	// Port is the 1-based downstream port number on the hub.
	Port int `json:"port"`
	// HubVendor and HubProduct identify the hub itself, so a stale
	// PortRef (e.g. after a different hub is plugged into the same
	// physical location) can be detected instead of silently
	// mis-controlling whatever is there now.
	HubVendor  uint16 `json:"hub_vendor"`
	HubProduct uint16 `json:"hub_product"`
}

// parentPathAndPort splits a device's USB port path into its parent hub's
// path and the (1-based) port number the device is attached to. A path of
// length 0 or 1 places the device directly on a root hub port, which has no
// controllable parent.
func parentPathAndPort(devPath []int) (parentPath []int, port int, err error) {
	if len(devPath) <= 1 {
		return nil, 0, ErrRootPort
	}
	return devPath[:len(devPath)-1], devPath[len(devPath)-1], nil
}

// ResolveParent finds the upstream hub and downstream port that a device at
// (bus, devPath) is attached to.
func ResolveParent(ctx *gousb.Context, bus int, devPath []int) (*PortRef, error) {
	parentPath, port, err := parentPathAndPort(devPath)
	if err != nil {
		return nil, fmt.Errorf("resolving parent hub for device at bus %d path %v: %w", bus, devPath, err)
	}

	devs, err := ctx.OpenDevices(isHubAt(bus, parentPath))
	defer closeAll(devs)
	if len(devs) == 0 {
		if err != nil {
			return nil, fmt.Errorf("finding parent hub for device at bus %d path %v: %w", bus, devPath, err)
		}
		return nil, fmt.Errorf("no hub found at bus %d path %v (parent of device path %v)", bus, parentPath, devPath)
	}

	hub := devs[0]
	return &PortRef{
		Bus:        bus,
		HubPath:    append([]int(nil), parentPath...),
		Port:       port,
		HubVendor:  uint16(hub.Desc.Vendor),
		HubProduct: uint16(hub.Desc.Product),
	}, nil
}

// isHubAt returns an OpenDevices predicate matching hub-class devices at a
// specific bus and USB port path.
func isHubAt(bus int, path []int) func(*gousb.DeviceDesc) bool {
	return func(desc *gousb.DeviceDesc) bool {
		return desc.Bus == bus && desc.Class == gousb.ClassHub && intsEqual(desc.Path, path)
	}
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func closeAll(devs []*gousb.Device) {
	for _, d := range devs {
		d.Close()
	}
}

// Port controls and queries power for a single downstream port on an
// already-open USB hub. A Port owns the hub's USB device handle; call
// Close when done with it.
type Port struct {
	dev       *gousb.Device
	ref       PortRef
	switching PowerSwitchingMode
}

// Open opens the hub identified by ref and returns a Port for the specific
// downstream port ref describes. It errors if no hub is found at ref's
// location, or if the hub found there no longer matches ref's recorded
// vendor/product (wrapping ErrHubMismatch — a stale cache entry).
func Open(ctx *gousb.Context, ref *PortRef) (*Port, error) {
	devs, err := ctx.OpenDevices(isHubAt(ref.Bus, ref.HubPath))
	if len(devs) == 0 {
		if err != nil {
			return nil, fmt.Errorf("opening hub at bus %d path %v: %w", ref.Bus, ref.HubPath, err)
		}
		return nil, fmt.Errorf("no hub found at bus %d path %v", ref.Bus, ref.HubPath)
	}

	hub := devs[0]
	closeAll(devs[1:]) // defensive: more than one hub matched bus+path+class

	if uint16(hub.Desc.Vendor) != ref.HubVendor || uint16(hub.Desc.Product) != ref.HubProduct {
		hub.Close()
		return nil, fmt.Errorf("hub at bus %d path %v is now %s:%s, expected %04x:%04x: %w",
			ref.Bus, ref.HubPath, hub.Desc.Vendor, hub.Desc.Product, ref.HubVendor, ref.HubProduct, ErrHubMismatch)
	}

	switching, err := hubPowerSwitching(hub)
	if err != nil {
		hub.Close()
		return nil, err
	}

	return &Port{dev: hub, ref: *ref, switching: switching}, nil
}

// Ref returns the PortRef this Port was opened from.
func (p *Port) Ref() *PortRef {
	ref := p.ref
	return &ref
}

// PerPortPower reports whether the hub switches this port's power
// independently of its sibling ports. When false, SetPower either affects
// every port on the hub together (ganged switching) or has no effect at
// all (no switching).
func (p *Port) PerPortPower() bool {
	return p.switching == PowerSwitchingPerPort
}

// Close closes the underlying hub USB device handle.
func (p *Port) Close() error {
	return p.dev.Close()
}
