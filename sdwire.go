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
	"github.com/jphastings/sdwire/internal/usbsuspend"
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
	// GenerationSDWire3 represents the SDWire3 device, switched by USB-suspending its reader.
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

// deviceReviver is implemented by controllers that can power-cycle their
// device to recover a reader that has stopped answering — currently only
// SDWire3, which is the generation with hub-port power control. It is a
// separate optional interface rather than part of DeviceController so that
// controllers with no such mechanism simply don't offer one, and callers
// can tell the difference.
type deviceReviver interface {
	Revive() error
}

// SDWire represents a connected SDWire device that can switch an SD card
// between a target device and host computer.
type SDWire struct {
	ctx        *gousb.Context
	info       DeviceInfo
	controller DeviceController
	powerFunc  PowerFunc
	warn       func(string)
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
	serial, serialErr := dev.SerialNumber()
	product, productErr := dev.Product()
	manufacturer, manufacturerErr := dev.Manufacturer()
	return infoFor(dev.Desc, orUnknown(serial, serialErr), orUnknown(product, productErr), orUnknown(manufacturer, manufacturerErr))
}

func infoFor(desc *gousb.DeviceDesc, serial, product, manufacturer string) *DeviceInfo {
	return &DeviceInfo{
		Serial:       serial,
		Product:      product,
		Manufacturer: manufacturer,
		Generation:   generationFor(desc),
		Bus:          desc.Bus,
		PortPath:     append([]int(nil), desc.Path...),
	}
}

func orUnknown(s string, err error) string {
	if err != nil || s == "" {
		return "unknown"
	}
	return s
}

func closeDevices(devs []*gousb.Device) {
	for _, dev := range devs {
		dev.Close()
	}
}

// discover finds every attached SDWire, in enumeration order. Where USB
// suspend is supported an SDWire3 is described from the OS's device
// registry and never opened, leaving its dev nil: reading its string
// descriptors wakes a suspended reader, taking the card back from the
// target, and on Linux even opening it does.
func discover(ctx *gousb.Context) ([]deviceMatch, error) {
	var order []*gousb.DeviceDesc
	unopened := map[*gousb.DeviceDesc]*DeviceInfo{}
	devs, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		if !matchesSDWire(desc) {
			return false
		}
		order = append(order, desc)
		if generationFor(desc) == GenerationSDWire3 && usbsuspend.Supported {
			serial, product, manufacturer, err := usbStrings(desc.Bus, desc.Path)
			unopened[desc] = infoFor(desc, orUnknown(serial, err), orUnknown(product, err), orUnknown(manufacturer, err))
			return false
		}
		return true
	})

	opened := make(map[*gousb.DeviceDesc]*gousb.Device, len(devs))
	for _, dev := range devs {
		opened[dev.Desc] = dev
	}
	matches := make([]deviceMatch, 0, len(order))
	for _, desc := range order {
		if dev, ok := opened[desc]; ok {
			matches = append(matches, deviceMatch{dev: dev, info: *describeDevice(dev)})
		} else if info, ok := unopened[desc]; ok {
			matches = append(matches, deviceMatch{info: *info})
		}
	}
	if err != nil {
		closeMatches(matches)
		return nil, fmt.Errorf("failed to find USB devices: %w", err)
	}
	return matches, nil
}

func closeMatches(matches []deviceMatch) {
	for _, m := range matches {
		if m.dev != nil {
			m.dev.Close()
		}
	}
}

// newController builds the controller for info's generation. dev is nil for
// an SDWire3 that discover left unopened.
func newController(ctx *gousb.Context, dev *gousb.Device, info DeviceInfo, o *options) (DeviceController, error) {
	switch info.Generation {
	case GenerationSDWireC:
		return &sdwireCController{device: dev}, nil
	case GenerationSDWire3:
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

	matches, err := discover(ctx)
	if err != nil {
		return nil, err
	}
	defer closeMatches(matches)

	devices := make([]*DeviceInfo, 0, len(matches))
	for _, m := range matches {
		devices = append(devices, &m.info)
	}
	return devices, nil
}

// DeviceState is an SDWire and the mode its card is currently switched to.
type DeviceState struct {
	Info DeviceInfo
	Mode SwitchMode
	// Attached reports whether the device was enumerated on USB. False
	// means it is known only from the hub-port cache: its remembered port
	// is either unpowered (an SDWire3 whose card is with the target) or
	// powered with nothing enumerated on it — an empty socket, or a reader
	// that has stopped answering and been torn off the bus by the OS.
	Attached bool
}

// ListDeviceStates returns every SDWire this host knows about, with the mode
// each one's card is switched to: the devices currently enumerated on USB,
// plus every hub-port cache entry that is not currently producing one.
//
// It never powers a port on or off and never talks to an SDWire3's reader —
// one sitting in target mode stays there — so unlike the New-family
// constructors it is safe to call just to see what is around. Cache-derived entries are a claim about a remembered
// location rather than a device seen now: see DeviceState.Attached.
func ListDeviceStates(opts ...Option) ([]DeviceState, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	matches, err := discover(ctx)
	if err != nil {
		return nil, err
	}
	defer closeMatches(matches)

	states := make([]DeviceState, 0, len(matches))
	attached := make(map[string]bool, len(matches))
	for _, m := range matches {
		attached[m.info.Location()] = true
		states = append(states, DeviceState{Info: m.info, Mode: attachedMode(m.dev, m.info, o), Attached: true})
	}

	cached, err := cachedDeviceStates(ctx, o, attached)
	if err != nil {
		o.warnFunc(fmt.Sprintf("reading hub-port cache: %v", err))
		return states, nil
	}
	return append(states, cached...), nil
}

// attachedMode reports the mode of a device that is currently enumerated,
// asking its controller. The exception is an SDWire3 on a platform that
// switches it by hub-port power, where an enumerated reader is a powered
// one and so on the host by construction.
func attachedMode(dev *gousb.Device, info DeviceInfo, o *options) SwitchMode {
	var ctrl DeviceController = &sdwireCController{device: dev}
	if info.Generation == GenerationSDWire3 {
		if !usbsuspend.Supported {
			return ModeHost
		}
		ctrl = &sdwire3Controller{info: info}
	}

	mode, err := ctrl.Mode()
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

	candidates, err := discover(ctx)
	if err != nil {
		ctx.Close()
		return nil, err
	}

	idx, pickErr := sel.pick(candidates)
	if pickErr != nil {
		closeMatches(candidates)
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
		if i != idx && c.dev != nil {
			c.dev.Close()
		}
	}

	return finishConnect(ctx, chosen.dev, chosen.info, o)
}

func finishConnect(ctx *gousb.Context, dev *gousb.Device, info DeviceInfo, o *options) (*SDWire, error) {
	controller, err := newController(ctx, dev, info, o)
	if err != nil {
		if dev != nil {
			dev.Close()
		}
		ctx.Close()
		return nil, err
	}

	return &SDWire{
		ctx:        ctx,
		info:       info,
		controller: controller,
		powerFunc:  o.powerFunc,
		warn:       o.warnFunc,
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

// warnf routes a warning to the handler this SDWire was constructed with,
// if any. An SDWire built without WithWarningHandler (or constructed
// directly in a test) silently drops them.
func (s *SDWire) warnf(format string, args ...any) {
	if s.warn == nil {
		return
	}
	s.warn(fmt.Sprintf(format, args...))
}

// String returns a formatted string with device information.
func (s *SDWire) String() string {
	return fmt.Sprintf("%s\t[%s::%s]", s.info.Serial, s.info.Product, s.info.Manufacturer)
}

// SetMode switches the SD card between the target device and the host
// computer.
//
// SDWire3 devices have no switching command: their reader hands the card
// to the target while it is USB-suspended (see sdwire3Controller).
//   - ModeTarget unconfigures and suspends the reader. It stays enumerated,
//     so ListDeviceStates still shows it, as Target.
//   - ModeHost resumes it and waits for it to come back configured; the
//     card's block device typically appears a few seconds later.
//
// On Windows, which has no userspace suspend control, ModeTarget instead
// cuts VBUS on the reader's upstream hub port and ModeHost restores it.
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

// Mode reads back which side the SD card is currently connected to, or
// ModeUnknown where the device is between the two (see the relevant
// DeviceController's Mode doc comment).
func (s *SDWire) Mode() (SwitchMode, error) {
	return s.controller.Mode()
}
