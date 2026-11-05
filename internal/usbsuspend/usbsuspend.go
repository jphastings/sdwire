// Package usbsuspend USB-suspends and resumes a device, identified by its
// bus and port path as gousb reports them, and reads its state and strings
// from the OS's device registry without any I/O to the device itself.
//
// It exists for the SDWire3, whose reader hands its SD card to the target
// exactly while it is USB-suspended. Anything that talks to a suspended
// reader — even reading a string descriptor — resumes it and takes the
// card back, which is why StateOf and Strings never do.
package usbsuspend

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned on platforms with no userspace control over
// USB suspend (Windows). Supported reports the same thing up front.
var ErrUnsupported = errors.New("usbsuspend: USB suspend control is not supported on this platform")

// State is where a USB device stands as far as suspend is concerned.
type State int

const (
	// Absent means no device is enumerated at the location.
	Absent State = iota
	// Suspended means the device is USB-suspended.
	Suspended
	// Configured means the device is awake with its function driver (for
	// a card reader, mass storage) attached.
	Configured
	// Unconfigured means the device is awake but has no function driver
	// attached: something woke it without reconfiguring it.
	Unconfigured
)

func (s State) String() string {
	switch s {
	case Suspended:
		return "suspended"
	case Configured:
		return "configured"
	case Unconfigured:
		return "awake but unconfigured"
	default:
		return "absent"
	}
}

func errNoDevice(bus int, path []int) error {
	return fmt.Errorf("usbsuspend: no USB device at bus %d port path %v", bus, path)
}
