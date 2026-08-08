package sdwire

import (
	"fmt"

	"github.com/google/gousb"
)

const (
	ftdiSioSetBitmodeRequest = 0x0B
	ftdiSioBitmodeCbus       = 0x20
	ftdiSioReadPinsRequest   = 0x0C // SIO_READ_PINS
)

// sdwireCController implements DeviceController for SDWireC devices using FTDI control.
type sdwireCController struct {
	device *gousb.Device
}

var _ DeviceController = (*sdwireCController)(nil)

// SetMode switches the SD card using FTDI bitmode control.
func (c *sdwireCController) SetMode(mode SwitchMode) error {
	if c.device == nil {
		return fmt.Errorf("device not initialized")
	}

	var target byte
	switch mode {
	case ModeTarget:
		target = 0
	case ModeHost:
		target = 1
	default:
		return fmt.Errorf("invalid switch mode: %v", mode)
	}

	// The Python code uses: ftdi.set_bitmode(0xF0 | target, Ftdi.BitMode.CBUS)
	// In FTDI terms: wValue = (mode << 8) | mask
	// where mode = FTDI_SIO_BITMODE_CBUS (0x20) and mask = 0xF0 | target
	value := uint16(ftdiSioBitmodeCbus<<8) | uint16(0xF0|target)

	_, err := c.device.Control(
		gousb.ControlOut|gousb.ControlVendor|gousb.ControlDevice,
		ftdiSioSetBitmodeRequest,
		value,
		0,
		nil,
	)

	if err != nil {
		return fmt.Errorf("failed to set SDWire mode: %w", err)
	}

	return nil
}

// Mode reads the SDWireC's current mode from its FTDI CBUS pins: bit 0 set
// means the SD card is connected to the host, clear means the target —
// matching the bit SetMode's 0xF0|target writes.
func (c *sdwireCController) Mode() (SwitchMode, error) {
	if c.device == nil {
		return ModeUnknown, fmt.Errorf("device not initialized")
	}

	buf := make([]byte, 1)
	_, err := c.device.Control(
		gousb.ControlIn|gousb.ControlVendor|gousb.ControlDevice,
		ftdiSioReadPinsRequest,
		0,
		0,
		buf,
	)
	if err != nil {
		return ModeUnknown, fmt.Errorf("failed to read SDWire mode: %w", err)
	}

	if buf[0]&0x1 != 0 {
		return ModeHost, nil
	}
	return ModeTarget, nil
}

// Close closes the underlying USB device handle.
func (c *sdwireCController) Close() error {
	if c.device == nil {
		return nil
	}
	return c.device.Close()
}
