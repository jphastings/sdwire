package sdwire

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/google/gousb"
	"github.com/jphastings/sdwire/hubpower"
	"github.com/jphastings/sdwire/internal/usbsuspend"
)

// Indirections over internal/usbsuspend, swapped out in tests.
var (
	usbStateOf = usbsuspend.StateOf
	usbStrings = usbsuspend.Strings
	usbSuspend = usbsuspend.Suspend
	usbResume  = usbsuspend.Resume
)

// suspendTimeout is how long SetMode(ModeTarget) waits for the reader to
// report itself suspended.
const suspendTimeout = 5 * time.Second

const statePollInterval = 250 * time.Millisecond

// sdwire3Controller implements DeviceController for SDWire3 devices.
//
// The SDWire3 is a single Realtek RTS5306E SD reader (0BDA:0316) with no
// FTDI sidecar. Its suspend output, meant to cut power to the SD card, also
// drives the mux, so the card is with the target exactly while the reader
// is USB-suspended — or unpowered, which holds that pin the same way.
//
// Where the OS lets userspace suspend a USB device (macOS and Linux; see
// internal/usbsuspend), ModeTarget suspends the reader and ModeHost
// resumes it, on any USB port. Where it doesn't (Windows), ModeTarget cuts
// VBUS on the reader's upstream hub port instead, which needs a hub with
// per-port power switching, and ModeHost restores it.
//
// Either way, Revive power-cycles that hub port to recover a reader that
// has latched up mid-transfer, when the reader sits behind such a hub.
//
// Because a target board normally only probes its SD slot at boot or on a
// card-detect edge, it will not notice a card that arrived via ModeTarget
// until it is rebooted or power-cycled.
type sdwire3Controller struct {
	ctx             *gousb.Context
	info            DeviceInfo
	warn            func(string)
	hostWaitTimeout time.Duration

	// device is nil unless a revive or a hub-power switch to ModeHost
	// opened the reader.
	device *gousb.Device
	port   *hubpower.Port

	// resolveErr, when set, is returned from whatever needs port (Revive,
	// and switching where USB suspend is unsupported) instead of using it.
	// It is not a construction failure, so a device whose upstream hub
	// port couldn't be resolved or opened (e.g. one sitting directly on a
	// root port) can still be listed and switched.
	resolveErr error
}

var _ DeviceController = (*sdwire3Controller)(nil)

// newSDWire3Controller resolves the reader's upstream hub port, caches it to
// disk keyed by info.Identity(), and opens it for power control. It never
// returns an error: any failure to resolve or open the hub port is stored
// and surfaced from whatever needs the port instead.
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

	if !port.PerPortPower() && !usbsuspend.Supported {
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

// SetMode switches the SD card by suspending or resuming the reader, or
// where that is unsupported by cutting or restoring VBUS on its upstream
// hub port. See sdwire3Controller's doc comment.
func (c *sdwire3Controller) SetMode(mode SwitchMode) error {
	if usbsuspend.Supported {
		return c.setModeBySuspend(mode)
	}
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

// setModeBySuspend skips a reader already in the requested state: on Linux
// opening a suspended reader wakes it, and re-enumerating a configured one
// drops its mounted volumes.
func (c *sdwire3Controller) setModeBySuspend(mode SwitchMode) error {
	if mode != ModeTarget && mode != ModeHost {
		return fmt.Errorf("invalid switch mode: %v", mode)
	}

	// An open handle keeps the reader awake, and on macOS holds the
	// exclusive access suspending and re-enumerating it need.
	if c.device != nil {
		c.device.Close()
		c.device = nil
	}

	st, err := usbStateOf(c.info.Bus, c.info.PortPath)
	if err != nil {
		return err
	}
	if mode == ModeTarget {
		if st == usbsuspend.Suspended {
			return nil
		}
		if err := usbSuspend(c.info.Bus, c.info.PortPath); err != nil {
			return err
		}
		return c.awaitState(usbsuspend.Suspended, suspendTimeout)
	}

	if st == usbsuspend.Configured {
		return nil
	}
	if err := usbResume(c.info.Bus, c.info.PortPath); err != nil {
		return err
	}
	return c.awaitState(usbsuspend.Configured, c.hostWaitTimeout)
}

// awaitState polls the reader until it reports want: neither a suspend nor
// a re-enumeration has settled when the call that started it returns.
func (c *sdwire3Controller) awaitState(want usbsuspend.State, timeout time.Duration) error {
	deadline := now().Add(timeout)
	for {
		st, err := usbStateOf(c.info.Bus, c.info.PortPath)
		if err == nil && st == want {
			return nil
		}
		if !now().Before(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("the reader at %s is %s %s after switching, not %s, so the card has not moved%s",
				c.info.Location(), st, timeout, want, suspendHint(want))
		}
		sleep(statePollInterval)
	}
}

func suspendHint(want usbsuspend.State) string {
	if want != usbsuspend.Suspended || runtime.GOOS != "linux" {
		return ""
	}
	return ` (its sysfs power/control must be "auto" and nothing may hold it open; install the udev rule from the README)`
}

// stateToMode maps the reader's USB state to where its card is. An awake
// reader with no mass-storage driver (something woke it without
// reconfiguring it) is on neither side usefully, so it is ModeUnknown, as
// is one that isn't enumerated at all.
func stateToMode(st usbsuspend.State) SwitchMode {
	switch st {
	case usbsuspend.Suspended:
		return ModeTarget
	case usbsuspend.Configured:
		return ModeHost
	default:
		return ModeUnknown
	}
}

// Revive power-cycles the SDWire3's hub port and waits for the reader to
// re-enumerate, which leaves it awake and so in host mode.
//
// It exists for a reader that has stopped answering mid-transfer and been
// torn off the bus by the OS: a power cycle is the only thing that clears
// a latched-up reader, so it needs a hub with port power switching.
func (c *sdwire3Controller) Revive() error {
	if c.resolveErr != nil {
		return fmt.Errorf("cannot revive: %w", c.resolveErr)
	}

	if c.device != nil {
		// Best-effort: the handle is usually already dead, which is why we
		// are here, and its close error says nothing useful about the
		// power cycle that follows.
		c.device.Close()
		c.device = nil
	}

	if err := revivePortPower(c.port); err != nil {
		return err
	}

	ref := c.port.Ref()
	devPath := append(append([]int(nil), ref.HubPath...), ref.Port)
	dev, err := waitForSDWireViaPort(c.ctx, c.port, ref.Bus, devPath, c.hostWaitTimeout)
	if err != nil {
		return err
	}
	c.device = dev
	return nil
}

// Mode reads the reader's live state without waking it: suspended is
// ModeTarget, configured is ModeHost (see stateToMode). Where USB suspend
// is unsupported it reads the hub port instead: unpowered means ModeTarget;
// powered and enumerated means ModeHost; powered but not yet enumerated
// (e.g. mid re-enumeration) is ModeUnknown with a nil error.
func (c *sdwire3Controller) Mode() (SwitchMode, error) {
	if usbsuspend.Supported {
		st, err := usbStateOf(c.info.Bus, c.info.PortPath)
		return stateToMode(st), err
	}
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
