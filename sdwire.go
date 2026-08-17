// Package sdwire provides a Go SDK for controlling SDWireC and SDWire3 devices.
// SDWire devices are USB-controlled SD card multiplexers that allow switching
// an SD card between a Device Under Test (DUT) and a Test System (TS).
//
// This is a fork of github.com/fcjr/sdwire with SDWire3 support and fixes.
package sdwire

import (
	"errors"
	"fmt"

	"github.com/google/gousb"
	"github.com/jphastings/sdwire/hubpower"
)

const (
	SDWireCVID         = 0x04E8
	SDWireCPID         = 0x6001
	SDWireCProductName = "sd-wire"

	SDWire3VID = 0x0BDA
	SDWire3PID = 0x0316
)

// DeviceGeneration represents the generation/type of SDWire device.
type DeviceGeneration int

const (
	// GenerationSDWireC represents the original SDWireC device using FTDI control.
	GenerationSDWireC DeviceGeneration = iota
	// GenerationSDWire3 represents the SDWire3 device using kernel driver attach/detach.
	GenerationSDWire3
)

// String returns a human-readable description of the device generation.
func (g DeviceGeneration) String() string {
	switch g {
	case GenerationSDWireC:
		return "SDWireC"
	case GenerationSDWire3:
		return "SDWire3"
	default:
		return "Unknown"
	}
}

// SwitchMode represents the SD card connection mode.
type SwitchMode int

const (
	// ModeTarget connects the SD card to the target device being tested.
	ModeTarget SwitchMode = iota
	// ModeHost connects the SD card to the host computer for flashing/access.
	ModeHost
)

// ModeUnknown indicates a DeviceController could not determine which side
// the SD card is currently connected to (for example, a hub port that is
// powered but whose device has not finished re-enumerating yet).
const ModeUnknown SwitchMode = -1

// String returns a human-readable description of the switch mode.
func (m SwitchMode) String() string {
	switch m {
	case ModeTarget:
		return "Target"
	case ModeHost:
		return "Host"
	case ModeUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// DeviceController defines the interface for controlling different SDWire device generations.
type DeviceController interface {
	// SetMode switches the SD card between the target device and the host computer.
	SetMode(mode SwitchMode) error
	// Mode reads back which side the SD card is currently connected to, where the
	// underlying mechanism allows an honest readback.
	Mode() (SwitchMode, error)
	// Close releases any USB device handles the controller owns.
	Close() error
}

// SDWire represents a connected SDWire device that can switch an SD card
// between a target device and host computer.
type SDWire struct {
	ctx        *gousb.Context
	info       DeviceInfo
	controller DeviceController
	powerFunc  PowerFunc
}

// DeviceInfo contains identifying information about an SDWire device.
type DeviceInfo struct {
	Serial       string
	Product      string
	Manufacturer string
	Generation   DeviceGeneration
	// Bus is the USB bus the device was enumerated on.
	Bus int
	// PortPath is the physical path of parent hub ports leading to the
	// device, as reported by gousb's DeviceDesc.Path.
	PortPath []int
}

func matchesSDWire(desc *gousb.DeviceDesc) bool {
	return (desc.Vendor == SDWireCVID && desc.Product == SDWireCPID) ||
		(desc.Vendor == SDWire3VID && desc.Product == SDWire3PID)
}

func generationFor(desc *gousb.DeviceDesc) DeviceGeneration {
	if desc.Vendor == SDWire3VID && desc.Product == SDWire3PID {
		return GenerationSDWire3
	}
	return GenerationSDWireC
}

func describeDevice(dev *gousb.Device) *DeviceInfo {
	serial, err := dev.SerialNumber()
	if err != nil {
		serial = "unknown"
	}
	product, err := dev.Product()
	if err != nil {
		product = "unknown"
	}
	manufacturer, err := dev.Manufacturer()
	if err != nil {
		manufacturer = "unknown"
	}

	desc := dev.Desc
	return &DeviceInfo{
		Serial:       serial,
		Product:      product,
		Manufacturer: manufacturer,
		Generation:   generationFor(desc),
		Bus:          desc.Bus,
		PortPath:     append([]int(nil), desc.Path...),
	}
}

func closeDevices(devs []*gousb.Device) {
	for _, dev := range devs {
		dev.Close()
	}
}

func newController(ctx *gousb.Context, dev *gousb.Device, info DeviceInfo, o *options) (DeviceController, error) {
	switch info.Generation {
	case GenerationSDWireC:
		return &sdwireCController{device: dev}, nil
	case GenerationSDWire3:
		if o.legacySDWire3 {
			return &sdwire3LegacyController{device: dev}, nil
		}
		return newSDWire3Controller(ctx, dev, info, o), nil
	default:
		return nil, fmt.Errorf("unsupported device generation: %v", info.Generation)
	}
}

// ListDevices discovers all connected SDWire devices and returns their information.
// This is useful for device enumeration before connecting to a specific device.
func ListDevices() ([]*DeviceInfo, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	devs, err := ctx.OpenDevices(matchesSDWire)
	defer closeDevices(devs)
	if err != nil {
		return nil, fmt.Errorf("failed to find USB devices: %w", err)
	}

	devices := make([]*DeviceInfo, 0, len(devs))
	for _, dev := range devs {
		devices = append(devices, describeDevice(dev))
	}
	return devices, nil
}

// DeviceState is an SDWire and the mode its card is currently switched to.
type DeviceState struct {
	Info DeviceInfo
	Mode SwitchMode
	// Attached reports whether the device was enumerated on USB. False
	// means it is known only from the hub-port cache: its remembered port
	// is either unpowered (an SDWire3 in target mode) or powered with
	// nothing enumerated on it — an empty socket, or a reader that has
	// stopped answering and been torn off the bus by the OS.
	Attached bool
}

// ListDeviceStates returns every SDWire this host knows about, with the mode
// each one's card is switched to: the devices currently enumerated on USB,
// plus every hub-port cache entry that is not currently producing one.
//
// It never powers a port on or off — an SDWire3 sitting in target mode stays
// there — so unlike the New-family constructors it is safe to call just to
// see what is around. Cache-derived entries are a claim about a remembered
// location rather than a device seen now: see DeviceState.Attached.
func ListDeviceStates(opts ...Option) ([]DeviceState, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	devs, err := ctx.OpenDevices(matchesSDWire)
	defer closeDevices(devs)
	if err != nil {
		return nil, fmt.Errorf("failed to find USB devices: %w", err)
	}

	states := make([]DeviceState, 0, len(devs))
	attached := make(map[string]bool, len(devs))
	for _, dev := range devs {
		info := *describeDevice(dev)
		attached[info.Location()] = true
		states = append(states, DeviceState{Info: info, Mode: attachedMode(dev, info, o), Attached: true})
	}

	cached, err := cachedDeviceStates(ctx, o, attached)
	if err != nil {
		o.warnFunc(fmt.Sprintf("reading hub-port cache: %v", err))
		return states, nil
	}
	return append(states, cached...), nil
}

// attachedMode reports the mode of a device that is currently enumerated.
// An SDWire3 leaves host mode only by losing power, which drops it off the
// bus, so an enumerated one has its card on the host by construction — but
// not under WithLegacySDWire3Switching, whose mechanism leaves the device
// enumerated in both modes with no honest readback. An SDWireC stays
// enumerated either way and is asked directly.
func attachedMode(dev *gousb.Device, info DeviceInfo, o *options) SwitchMode {
	if info.Generation == GenerationSDWire3 {
		if o.legacySDWire3 {
			return ModeUnknown
		}
		return ModeHost
	}

	mode, err := (&sdwireCController{device: dev}).Mode()
	if err != nil {
		o.warnFunc(fmt.Sprintf("reading mode of %s: %v", info.Identity(), err))
		return ModeUnknown
	}
	return mode
}

type deviceMatch struct {
	dev  *gousb.Device
	info DeviceInfo
}

func infosOf(candidates []deviceMatch) []DeviceInfo {
	infos := make([]DeviceInfo, len(candidates))
	for i, c := range candidates {
		infos[i] = c.info
	}
	return infos
}

// selection describes how a New-family constructor picks a device: pick
// chooses among currently attached, enumerated devices; matchesCacheEntry
// decides whether a hubpower cache entry (for a device that may currently
// be powered off) is worth trying as a fallback.
type selection struct {
	pick              func(candidates []deviceMatch) (int, error)
	matchesCacheEntry func(key string, ref *hubpower.PortRef) bool
}

// connect enumerates attached SDWire devices, hands them to sel.pick to
// choose one, and closes every device (and the gousb.Context) that isn't
// the chosen one. If no attached device matches, it falls back to
// powering on any hubpower-cached hub port matching sel.matchesCacheEntry
// and waiting for a device to reappear there. The returned SDWire owns the
// context for the rest of its lifetime and closes it in Close().
func connect(sel selection, opts []Option) (*SDWire, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	ctx := gousb.NewContext()

	devs, err := ctx.OpenDevices(matchesSDWire)
	if err != nil {
		closeDevices(devs)
		ctx.Close()
		return nil, fmt.Errorf("failed to find USB devices: %w", err)
	}

	candidates := make([]deviceMatch, len(devs))
	for i, dev := range devs {
		candidates[i] = deviceMatch{dev: dev, info: *describeDevice(dev)}
	}

	idx, pickErr := sel.pick(candidates)
	if pickErr != nil {
		closeDevices(devs)
		// Only an outright "nothing matched" is worth trying the cache
		// fallback for; an ambiguous-match error means real candidates
		// exist and should be reported, not silently resolved by picking
		// whatever the cache happens to revive. WithoutRevive skips the
		// fallback entirely, for callers that must not have the side
		// effect of powering a device on (e.g. a read-only state check).
		if errors.Is(pickErr, ErrNoDeviceFound) && !o.withoutRevive {
			dev, info, fbErr := tryCacheFallback(ctx, o, sel.matchesCacheEntry)
			if fbErr == nil {
				return finishConnect(ctx, dev, info, o)
			}
			o.warnFunc(fmt.Sprintf("hub-cache revive failed: %v", fbErr))
		}
		ctx.Close()
		return nil, pickErr
	}

	chosen := candidates[idx]
	for i, c := range candidates {
		if i != idx {
			c.dev.Close()
		}
	}

	return finishConnect(ctx, chosen.dev, chosen.info, o)
}

func finishConnect(ctx *gousb.Context, dev *gousb.Device, info DeviceInfo, o *options) (*SDWire, error) {
	controller, err := newController(ctx, dev, info, o)
	if err != nil {
		dev.Close()
		ctx.Close()
		return nil, err
	}

	return &SDWire{
		ctx:        ctx,
		info:       info,
		controller: controller,
		powerFunc:  o.powerFunc,
	}, nil
}

// New connects to the first available SDWire device.
// This is a convenience function for single-device setups.
// The returned SDWire must be closed with Close() when done.
func New(opts ...Option) (*SDWire, error) {
	return connect(selection{
		pick: func(candidates []deviceMatch) (int, error) {
			if len(candidates) == 0 {
				return 0, fmt.Errorf("no SDWire devices found: %w", ErrNoDeviceFound)
			}
			return 0, nil
		},
		matchesCacheEntry: func(string, *hubpower.PortRef) bool { return true },
	}, opts)
}

// NewWithSerial connects to a specific SDWire device by its serial number.
// serial may be a plain USB serial number, or the suffixed form returned by
// DeviceInfo.Identity() (e.g. "20120501030900000.1.1.3") to disambiguate
// devices that share a serial number, such as SDWire3s.
// If a plain serial matches more than one attached device, an error is
// returned listing the Identity() of each candidate.
// The returned SDWire must be closed with Close() when done.
func NewWithSerial(serial string, opts ...Option) (*SDWire, error) {
	return connect(selection{
		pick: func(candidates []deviceMatch) (int, error) {
			return selectBySerial(infosOf(candidates), serial)
		},
		matchesCacheEntry: func(key string, ref *hubpower.PortRef) bool {
			_, err := selectBySerial([]DeviceInfo{cacheEntryDeviceInfo(key, ref)}, serial)
			return err == nil
		},
	}, opts)
}

// NewWithIdentity connects to a specific SDWire device using either the
// suffixed form returned by DeviceInfo.Identity() (e.g.
// "20120501030900000.1.1.3") or the form returned by DeviceInfo.Location()
// (e.g. "1-1.1.3"). Use NewWithSerial instead to match on a bare serial
// number. The returned SDWire must be closed with Close() when done.
func NewWithIdentity(id string, opts ...Option) (*SDWire, error) {
	return connect(selection{
		pick: func(candidates []deviceMatch) (int, error) {
			return selectByIdentity(infosOf(candidates), id)
		},
		matchesCacheEntry: func(key string, ref *hubpower.PortRef) bool {
			_, err := selectByIdentity([]DeviceInfo{cacheEntryDeviceInfo(key, ref)}, id)
			return err == nil
		},
	}, opts)
}

// Close releases the SDWire's controller (and any USB device handle(s) it
// holds) and its gousb.Context. Always call this when done with the device.
func (s *SDWire) Close() error {
	var errs []error
	if s.controller != nil {
		if err := s.controller.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.ctx != nil {
		if err := s.ctx.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// GetSerial returns the device's USB serial number.
func (s *SDWire) GetSerial() string {
	return s.info.Serial
}

// GetProduct returns the device's USB product name.
func (s *SDWire) GetProduct() string {
	return s.info.Product
}

// GetManufacturer returns the device's USB manufacturer name.
func (s *SDWire) GetManufacturer() string {
	return s.info.Manufacturer
}

// Info returns the DeviceInfo this SDWire was connected with, including its
// USB topology (Bus, PortPath) and the Identity()/Location() helpers derived
// from them. Useful for callers (e.g. the sdwire CLI) that connected via a
// partial selector — a bare serial, or a configured device name — and need
// the fully-resolved identity of the device they ended up with.
func (s *SDWire) Info() DeviceInfo {
	return s.info
}

// String returns a formatted string with device information.
func (s *SDWire) String() string {
	return fmt.Sprintf("%s\t[%s::%s]", s.info.Serial, s.info.Product, s.info.Manufacturer)
}

// SetMode switches the SD card between the target device and the host
// computer.
//
// For SDWire3 devices (the default; see WithLegacySDWire3Switching for the
// legacy kernel-driver-based mechanism), this works by cutting or restoring
// VBUS on the SDWire3's upstream USB hub port, rather than by any command
// understood by the SDWire3 itself:
//   - ModeTarget cuts power to the SDWire3 reader. Losing power drops it
//     off the bus entirely, and the now-unpowered mux passes the SD card
//     through to the target — typically within about a second.
//   - ModeHost restores power. The reader re-enumerates at its USB
//     power-on default (card connected to the host); the resulting block
//     device typically appears roughly 6 seconds later.
//
// Because a target board normally only probes its SD slot at boot or on a
// card-detect edge, it will not notice a card that arrived via ModeTarget
// until it is rebooted or power-cycled — see (*SDWire).PowerCycle.
//
// SDWireC devices switch instantly via FTDI CBUS bits and have no such
// caveat.
//
// Before switching to ModeTarget, any volumes mounted from this SDWire's
// reader are unmounted (see Unmount in internal/blockdev; on macOS a
// politely-dissented unmount is retried with force, since the data is
// flushed either way and the card is leaving the host regardless). Pass
// WithoutUnmount to skip this. If the reader's block device cannot be
// located at all, the switch proceeds — there is nothing mounted to lose —
// but an actual failed unmount aborts the switch rather than yanking a
// mounted filesystem away.
func (s *SDWire) SetMode(mode SwitchMode, opts ...ModeOption) error {
	mo := modeOptions{}
	for _, opt := range opts {
		opt(&mo)
	}

	if mode == ModeTarget && !mo.skipUnmount {
		if devPath, err := blockdevFind(blockdevRef(s.info)); err == nil {
			if uerr := blockdevUnmount(devPath); uerr != nil {
				return fmt.Errorf("unmounting %s before switching to target: %w (pass WithoutUnmount to skip)", devPath, uerr)
			}
		}
	}

	return s.controller.SetMode(mode)
}

// modeOptions holds per-SetMode-call configuration.
type modeOptions struct {
	skipUnmount bool
}

// ModeOption customizes a single SetMode call.
type ModeOption func(*modeOptions)

// WithoutUnmount skips the automatic unmount of the reader's mounted
// volumes that SetMode(ModeTarget) performs by default.
func WithoutUnmount() ModeOption {
	return func(mo *modeOptions) {
		mo.skipUnmount = true
	}
}

// Mode reads back which side the SD card is currently connected to. Not
// all controllers can answer honestly — see the relevant
// DeviceController's Mode doc comment (in particular,
// WithLegacySDWire3Switching's controller cannot).
func (s *SDWire) Mode() (SwitchMode, error) {
	return s.controller.Mode()
}
