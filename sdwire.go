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

// String returns a human-readable description of the switch mode.
func (m SwitchMode) String() string {
	switch m {
	case ModeTarget:
		return "Target"
	case ModeHost:
		return "Host"
	default:
		return "Unknown"
	}
}

// DeviceController defines the interface for controlling different SDWire device generations.
type DeviceController interface {
	SetMode(mode SwitchMode) error
}

// SDWire represents a connected SDWire device that can switch an SD card
// between a target device and host computer.
type SDWire struct {
	ctx        *gousb.Context
	device     *gousb.Device
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

// Option customizes a newly constructed SDWire.
type Option func(*SDWire)

// WithTargetPower configures the PowerFunc used to control power to the
// target board (the Device Under Test) attached via this SDWire.
func WithTargetPower(fn PowerFunc) Option {
	return func(s *SDWire) {
		s.powerFunc = fn
	}
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

func newController(dev *gousb.Device, gen DeviceGeneration) (DeviceController, error) {
	switch gen {
	case GenerationSDWireC:
		return &sdwireCController{device: dev}, nil
	case GenerationSDWire3:
		return &sdwire3Controller{device: dev}, nil
	default:
		return nil, fmt.Errorf("unsupported device generation: %v", gen)
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

// connect enumerates attached SDWire devices, hands them to selectDevice to
// pick one, and closes every device (and the gousb.Context) that isn't the
// chosen one. The returned SDWire owns the context for the rest of its
// lifetime and closes it in Close().
func connect(selectDevice func(candidates []deviceMatch) (int, error), opts []Option) (*SDWire, error) {
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

	idx, err := selectDevice(candidates)
	if err != nil {
		closeDevices(devs)
		ctx.Close()
		return nil, err
	}

	chosen := candidates[idx]
	for i, c := range candidates {
		if i != idx {
			c.dev.Close()
		}
	}

	controller, err := newController(chosen.dev, chosen.info.Generation)
	if err != nil {
		chosen.dev.Close()
		ctx.Close()
		return nil, err
	}

	sd := &SDWire{
		ctx:        ctx,
		device:     chosen.dev,
		info:       chosen.info,
		controller: controller,
	}
	for _, opt := range opts {
		opt(sd)
	}
	return sd, nil
}

// New connects to the first available SDWire device.
// This is a convenience function for single-device setups.
// The returned SDWire must be closed with Close() when done.
func New(opts ...Option) (*SDWire, error) {
	return connect(func(candidates []deviceMatch) (int, error) {
		if len(candidates) == 0 {
			return 0, fmt.Errorf("no SDWire devices found")
		}
		return 0, nil
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
	return connect(func(candidates []deviceMatch) (int, error) {
		return selectBySerial(infosOf(candidates), serial)
	}, opts)
}

// NewWithIdentity connects to a specific SDWire device using either the
// suffixed form returned by DeviceInfo.Identity() (e.g.
// "20120501030900000.1.1.3") or the form returned by DeviceInfo.Location()
// (e.g. "1-1.1.3"). Use NewWithSerial instead to match on a bare serial
// number. The returned SDWire must be closed with Close() when done.
func NewWithIdentity(id string, opts ...Option) (*SDWire, error) {
	return connect(func(candidates []deviceMatch) (int, error) {
		return selectByIdentity(infosOf(candidates), id)
	}, opts)
}

// Close releases the USB device and its gousb.Context. Always call this
// when done with the device.
func (s *SDWire) Close() error {
	var errs []error
	if s.device != nil {
		if err := s.device.Close(); err != nil {
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

// String returns a formatted string with device information.
func (s *SDWire) String() string {
	return fmt.Sprintf("%s\t[%s::%s]", s.info.Serial, s.info.Product, s.info.Manufacturer)
}

// SetMode switches the SD card to the specified mode.
func (s *SDWire) SetMode(mode SwitchMode) error {
	return s.controller.SetMode(mode)
}
