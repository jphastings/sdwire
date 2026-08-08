package sdwire

import (
	"fmt"

	"github.com/google/gousb"
)

// sdwire3LegacyController implements DeviceController for SDWire3 devices
// using kernel driver attach/detach and a USB port reset.
//
// This mechanism does not reliably move the SD card mux in practice (see
// sdwire3Controller's doc comment for the VBUS-based mechanism that
// replaced it as the default). It is kept, opt-in via
// WithLegacySDWire3Switching, because it may still work on some native
// Linux setups.
type sdwire3LegacyController struct {
	device *gousb.Device
}

var _ DeviceController = (*sdwire3LegacyController)(nil)

// SetMode switches the SD card using kernel driver attach/detach mechanism.
func (c *sdwire3LegacyController) SetMode(mode SwitchMode) error {
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

// Mode always fails: kernel-driver detach/reset has no mechanism to read
// back which side the SD card is actually connected to, so any reported
// mode would be a guess rather than an honest readback.
func (c *sdwire3LegacyController) Mode() (SwitchMode, error) {
	return ModeUnknown, fmt.Errorf("legacy SDWire3 switching has no honest mode readback")
}

// Close closes the underlying USB device handle.
func (c *sdwire3LegacyController) Close() error {
	if c.device == nil {
		return nil
	}
	return c.device.Close()
}
