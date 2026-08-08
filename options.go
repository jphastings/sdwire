package sdwire

import "time"

// defaultHostWaitTimeout is how long SetMode(ModeHost) and the powered-off
// device fallback wait for an SDWire3 to re-enumerate before giving up.
// Re-enumeration has been observed to take ~11s on a macOS dock hub, so
// this errs well above that.
const defaultHostWaitTimeout = 20 * time.Second

// options holds the fully-resolved configuration for a connect() call.
// Options are applied to it before any controller is constructed, since
// SDWire3 controller construction itself depends on them (warn func,
// host-wait timeout, hub cache path, legacy switching).
type options struct {
	legacySDWire3   bool
	warnFunc        func(msg string)
	hostWaitTimeout time.Duration
	hubCachePath    string
	powerFunc       PowerFunc
	withoutRevive   bool
}

func defaultOptions() *options {
	return &options{
		warnFunc:        func(string) {},
		hostWaitTimeout: defaultHostWaitTimeout,
	}
}

// Option customizes a newly constructed SDWire.
type Option func(*options)

// WithTargetPower configures the PowerFunc used to control power to the
// target board (the Device Under Test) attached via this SDWire.
func WithTargetPower(fn PowerFunc) Option {
	return func(o *options) {
		o.powerFunc = fn
	}
}

// WithLegacySDWire3Switching makes SDWire3 devices use the legacy
// kernel-driver detach/reset controller instead of the default VBUS
// port-power controller. That mechanism is not known to reliably move the
// SD card mux (see sdwire3Controller's doc comment), but may work on some
// native Linux setups; it is never the default.
func WithLegacySDWire3Switching() Option {
	return func(o *options) {
		o.legacySDWire3 = true
	}
}

// WithWarningHandler configures a callback that receives non-fatal warning
// messages generated while operating a device, such as an SDWire3 sitting
// behind a ganged-power-switching hub. The default silently discards them.
func WithWarningHandler(fn func(msg string)) Option {
	return func(o *options) {
		if fn != nil {
			o.warnFunc = fn
		}
	}
}

// WithHostWaitTimeout sets how long to wait for an SDWire3 to re-enumerate
// after switching to ModeHost (or while powering one on from the hub cache
// fallback) before giving up. The default is 20 seconds.
func WithHostWaitTimeout(d time.Duration) Option {
	return func(o *options) {
		o.hostWaitTimeout = d
	}
}

// WithHubCachePath overrides where the on-disk hub-port cache is read from
// and written to, in place of hubpower.DefaultCachePath().
func WithHubCachePath(path string) Option {
	return func(o *options) {
		o.hubCachePath = path
	}
}

// WithoutRevive disables the hub-cache revive fallback: when no attached
// device matches, connect() returns the not-found error as-is instead of
// powering on a cached hub port and waiting for the device to reappear.
// Read-only callers (e.g. the sdwire CLI's `state` command) use this so
// they never have the side effect of switching an SDWire3 that is
// intentionally powered off in target mode back to host mode.
func WithoutRevive() Option {
	return func(o *options) {
		o.withoutRevive = true
	}
}
