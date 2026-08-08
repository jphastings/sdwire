package sdwire

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/gousb"
	"github.com/jphastings/sdwire/hubpower"
)

// sdwire3Controller implements DeviceController for SDWire3 devices by
// cutting and restoring VBUS on the SDWire3's upstream USB hub port.
//
// The SDWire3 (a single Realtek 0BDA:0316 SD reader, with no FTDI
// sidecar) never hands the SD card to the target while its own USB link
// stays up — the kernel-driver detach/reset trick sdwire3LegacyController
// uses does not move the mux. What does work is depriving the SDWire3 of
// power altogether: clearing PORT_POWER on its upstream hub port makes the
// reader disappear from the bus entirely, and the now-unpowered mux passes
// the card through to the target (observed ~1s). Restoring PORT_POWER
// re-powers the reader, which re-enumerates at its USB power-on default —
// card connected to the host — with the resulting block device typically
// appearing ~6s later.
//
// Because a target board normally only probes its SD slot at boot or on a
// card-detect edge, it will not notice a card that arrived via ModeTarget
// until it is rebooted or power-cycled.
type sdwire3Controller struct {
	ctx             *gousb.Context
	info            DeviceInfo
	warn            func(string)
	hostWaitTimeout time.Duration

	device *gousb.Device // nil while switched to ModeTarget (powered off)
	port   *hubpower.Port

	// resolveErr, when set, is returned from SetMode and Mode instead of
	// attempting to use port (which may be nil). It is not treated as a
	// construction failure so that listing/getters keep working even for
	// a device whose upstream hub port couldn't be resolved or opened
	// (e.g. one sitting directly on a root port).
	resolveErr error
}

var _ DeviceController = (*sdwire3Controller)(nil)

// newSDWire3Controller resolves dev's upstream hub port, caches it to disk
// keyed by info.Identity(), and opens it for power control. It never
// returns an error: any failure to resolve or open the hub port is stored
// and surfaced from SetMode/Mode instead, so the SDWire can still be
// constructed and its identifying getters used.
func newSDWire3Controller(ctx *gousb.Context, dev *gousb.Device, info DeviceInfo, o *options) *sdwire3Controller {
	c := &sdwire3Controller{
		ctx:             ctx,
		info:            info,
		warn:            o.warnFunc,
		hostWaitTimeout: o.hostWaitTimeout,
		device:          dev,
	}

	ref, err := hubpower.ResolveParent(ctx, info.Bus, info.PortPath)
	if err != nil {
		c.resolveErr = err
		return c
	}

	if err := cachePortRef(o, info.Identity(), ref); err != nil {
		c.warn(fmt.Sprintf("caching hub port for %s: %v", info.Identity(), err))
	}

	port, err := hubpower.Open(ctx, ref)
	if err != nil {
		c.resolveErr = err
		return c
	}
	c.port = port

	if !port.PerPortPower() {
		c.warn(fmt.Sprintf("hub for %s does not switch port power independently; switching this SDWire3 will also affect sibling devices on the same hub", info.Identity()))
	}

	return c
}

func cachePortRef(o *options, key string, ref *hubpower.PortRef) error {
	cachePath, err := resolveHubCachePath(o)
	if err != nil {
		return err
	}
	cache, err := hubpower.LoadCache(cachePath)
	if err != nil {
		return err
	}
	cache.Put(key, ref)
	return cache.Save(cachePath)
}

// SetMode switches the SD card by cutting or restoring VBUS on the
// SDWire3's upstream hub port. See sdwire3Controller's doc comment for the
// underlying mechanism and its timing.
func (c *sdwire3Controller) SetMode(mode SwitchMode) error {
	if c.resolveErr != nil {
		return fmt.Errorf("cannot switch mode: %w", c.resolveErr)
	}

	switch mode {
	case ModeTarget:
		if c.device != nil {
			if err := c.device.Close(); err != nil {
				return fmt.Errorf("closing SDWire3 device handle before powering off its hub port: %w", err)
			}
			c.device = nil
		}
		return c.port.SetPower(false)

	case ModeHost:
		if c.device != nil {
			// Already holding a handle (e.g. already in host mode): close it
			// before re-opening below rather than leaking it.
			c.device.Close()
			c.device = nil
		}
		if err := c.port.SetPower(true); err != nil {
			return err
		}
		ref := c.port.Ref()
		devPath := append(append([]int(nil), ref.HubPath...), ref.Port)
		dev, err := waitForSDWireViaPort(c.ctx, c.port, ref.Bus, devPath, c.hostWaitTimeout)
		if err != nil {
			// A reader that fails to re-enumerate may be latched up from a
			// too-brief power interruption: give it one full power-cycle
			// with real dark time before giving up.
			if rerr := revivePortPower(c.port); rerr == nil {
				dev, err = waitForSDWireViaPort(c.ctx, c.port, ref.Bus, devPath, c.hostWaitTimeout)
			}
		}
		if err != nil {
			return fmt.Errorf("switching to host mode: %w", err)
		}
		c.device = dev
		return nil

	default:
		return fmt.Errorf("invalid switch mode: %v", mode)
	}
}

// Mode reads the SDWire3's live state from its hub port: unpowered means
// ModeTarget; powered and enumerated means ModeHost; powered but not yet
// enumerated (e.g. mid re-enumeration) is reported as ModeUnknown with a
// nil error.
func (c *sdwire3Controller) Mode() (SwitchMode, error) {
	if c.resolveErr != nil {
		return ModeUnknown, fmt.Errorf("cannot read mode: %w", c.resolveErr)
	}

	status, err := c.port.Status()
	if err != nil {
		return ModeUnknown, err
	}
	return statusToMode(status), nil
}

// Close closes the SDWire3 device handle, if currently open, and the hub
// port handle used to control its power.
func (c *sdwire3Controller) Close() error {
	var errs []error
	if c.device != nil {
		if err := c.device.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if c.port != nil {
		if err := c.port.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
