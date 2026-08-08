package sdwire

import (
	"fmt"

	"github.com/google/gousb"
)

// sdwire3Controller implements DeviceController for SDWire3 devices using kernel driver attach/detach.
type sdwire3Controller struct {
	device *gousb.Device
}

// SetMode switches the SD card using kernel driver attach/detach mechanism.
func (c *sdwire3Controller) SetMode(mode SwitchMode) error {
	if c.device == nil {
		return fmt.Errorf("device not initialized")
	}

	// Enable auto-detach so we can control kernel driver attachment
	err := c.device.SetAutoDetach(true)
	if err != nil {
		return fmt.Errorf("failed to enable auto-detach: %w", err)
	}

	switch mode {
	case ModeHost:
		// Switch to TS mode: ensure kernel driver is attached (don't claim interface)
		// Just reset the device - kernel driver should reattach automatically
		return c.device.Reset()

	case ModeTarget:
		// Switch to DUT mode: detach kernel driver by claiming interface 0, then reset
		cfg, err := c.device.Config(1)
		if err != nil {
			// If we can't get config, just reset - might work anyway
			return c.device.Reset()
		}
		defer cfg.Close()

		// Claim interface 0 to detach kernel driver
		intf, err := cfg.Interface(0, 0)
		if err == nil {
			// Successfully claimed interface (kernel driver detached)
			intf.Close() // Release interface but keep kernel driver detached
		}

		// Reset the device
		return c.device.Reset()

	default:
		return fmt.Errorf("invalid switch mode: %v", mode)
	}
}
