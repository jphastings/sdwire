// Package power documents the contract for SDWire target-power plugins.
//
// Vendor-specific implementations (relay boards, PDUs, etc.) live in
// subpackages of this one. Each subpackage exposes a constructor returning
// an sdwire.PowerFunc: a func(shouldBeOn bool) error that turns power to the
// Device Under Test on or off, blocking until the change has taken effect.
package power
