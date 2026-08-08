package hubpower

import (
	"encoding/binary"
	"fmt"
	"runtime"

	"github.com/google/gousb"
)

const (
	bRequestGetStatus     = 0x00 // GET_STATUS
	bRequestClearFeature  = 0x01 // CLEAR_FEATURE
	bRequestSetFeature    = 0x03 // SET_FEATURE
	bRequestGetDescriptor = 0x06 // GET_DESCRIPTOR

	featurePortPower = 8 // PORT_POWER feature selector (USB 2.0 spec table 11-17)

	descriptorTypeHub           = 0x29 // USB 2.0 hub class descriptor
	descriptorTypeSuperSpeedHub = 0x2A // USB 3.x (SuperSpeed) hub class descriptor
)

const (
	// requestTypeSetPortFeature targets SET_FEATURE/CLEAR_FEATURE at a hub's downstream-port ("other") recipient.
	requestTypeSetPortFeature = gousb.ControlOut | gousb.ControlClass | gousb.ControlOther
	// requestTypeGetPortStatus reads a hub port's status.
	requestTypeGetPortStatus = gousb.ControlIn | gousb.ControlClass | gousb.ControlOther
	// requestTypeGetHubDescriptor reads the hub's own class descriptor.
	requestTypeGetHubDescriptor = gousb.ControlIn | gousb.ControlClass | gousb.ControlDevice
)

// PowerSwitchingMode is how a hub switches power to its downstream ports,
// decoded from wHubCharacteristics bits 1:0 (USB 2.0 spec §11.23.2.1; USB
// 3.x hubs use the same encoding).
type PowerSwitchingMode int

const (
	// PowerSwitchingGanged means a single switch controls all ports together.
	PowerSwitchingGanged PowerSwitchingMode = iota
	// PowerSwitchingPerPort means each port is switched independently.
	PowerSwitchingPerPort
	// PowerSwitchingNone means ports are always powered; SetPower has no effect.
	PowerSwitchingNone
)

// parseHubCharacteristics decodes a hub's power-switching mode from its raw
// class descriptor: wHubCharacteristics is a little-endian uint16 at byte
// offset 3, and its bits 1:0 select the mode (0b01 = per-port, 0b00 =
// ganged, 0b1x = none/reserved).
func parseHubCharacteristics(desc []byte) (PowerSwitchingMode, error) {
	if len(desc) < 5 {
		return 0, fmt.Errorf("hub descriptor too short: got %d bytes, want at least 5", len(desc))
	}
	wHubCharacteristics := binary.LittleEndian.Uint16(desc[3:5])
	switch wHubCharacteristics & 0x3 {
	case 0b00:
		return PowerSwitchingGanged, nil
	case 0b01:
		return PowerSwitchingPerPort, nil
	default: // 0b10, 0b11: reserved / no power switching
		return PowerSwitchingNone, nil
	}
}

// PortStatus is a hub port's live power and connection state, decoded from
// a GET_STATUS response.
type PortStatus struct {
	// Powered reports whether VBUS is currently on for this port.
	Powered bool
	// Connected reports whether the hub currently detects a device on this port.
	Connected bool
}

// decodePortStatus decodes wPortStatus (the first two bytes of a hub's
// GET_STATUS(port) response, USB 2.0 spec §11.24.2.7.1). PORT_CONNECTION is
// always bit 0. PORT_POWER's bit position depends on the hub's own USB
// version, given by hubSpec (the hub's DeviceDesc.Spec): bit 8 for USB 2.0
// hubs, bit 9 for USB 3.x (SuperSpeed) hubs.
func decodePortStatus(buf []byte, hubSpec gousb.BCD) (PortStatus, error) {
	if len(buf) < 2 {
		return PortStatus{}, fmt.Errorf("port status too short: got %d bytes, want at least 2", len(buf))
	}
	wPortStatus := binary.LittleEndian.Uint16(buf[0:2])
	powerBit := uint(8)
	if hubSpec.Major() >= 3 {
		powerBit = 9
	}
	return PortStatus{
		Connected: wPortStatus&0x1 != 0,
		Powered:   wPortStatus&(1<<powerBit) != 0,
	}, nil
}

// hubPowerSwitching reads the hub's own class descriptor (USB 2.0 or
// SuperSpeed, depending on the hub's USB version) and decodes its
// power-switching mode.
func hubPowerSwitching(hub *gousb.Device) (PowerSwitchingMode, error) {
	descType := descriptorTypeHub
	if hub.Desc.Spec.Major() >= 3 {
		descType = descriptorTypeSuperSpeedHub
	}

	buf := make([]byte, 32)
	n, err := hub.Control(requestTypeGetHubDescriptor, bRequestGetDescriptor, uint16(descType)<<8, 0, buf)
	if err != nil {
		return 0, wrapControlError("reading hub descriptor", err)
	}
	return parseHubCharacteristics(buf[:n])
}

// SetPower turns VBUS to this port on or off.
func (p *Port) SetPower(on bool) error {
	request := uint8(bRequestClearFeature)
	if on {
		request = uint8(bRequestSetFeature)
	}
	_, err := p.dev.Control(requestTypeSetPortFeature, request, featurePortPower, uint16(p.ref.Port), nil)
	if err != nil {
		return wrapControlError(fmt.Sprintf("setting port %d power to %v", p.ref.Port, on), err)
	}
	return nil
}

// Status reads this port's live power and connection state from the hub.
func (p *Port) Status() (PortStatus, error) {
	buf := make([]byte, 4)
	_, err := p.dev.Control(requestTypeGetPortStatus, bRequestGetStatus, 0, uint16(p.ref.Port), buf)
	if err != nil {
		return PortStatus{}, wrapControlError(fmt.Sprintf("reading port %d status", p.ref.Port), err)
	}
	return decodePortStatus(buf, p.dev.Desc.Spec)
}

func wrapControlError(action string, err error) error {
	return fmt.Errorf("%s: %w (%s)", action, err, platformHubAccessHint())
}

func platformHubAccessHint() string {
	switch runtime.GOOS {
	case "linux":
		return "on Linux this needs write access to the hub's usbfs device node: run as root, or add a udev rule granting your user access"
	case "windows":
		return "on Windows the hub must be accessible to libusb: install UsbDk, or bind the hub to WinUSB (e.g. via Zadig)"
	case "darwin":
		return "on macOS external hubs are normally controllable without extra privileges; internal/root hubs typically are not"
	default:
		return "this process needs write access to the hub's USB device node"
	}
}
