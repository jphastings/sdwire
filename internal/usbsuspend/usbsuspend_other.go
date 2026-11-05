//go:build !darwin && !linux

package usbsuspend

// Supported reports whether this platform gives userspace USB suspend
// control.
const Supported = false

// StateOf returns ErrUnsupported.
func StateOf(bus int, path []int) (State, error) { return Absent, ErrUnsupported }

// Strings returns ErrUnsupported.
func Strings(bus int, path []int) (serial, product, manufacturer string, err error) {
	return "", "", "", ErrUnsupported
}

// Suspend returns ErrUnsupported.
func Suspend(bus int, path []int) error { return ErrUnsupported }

// Resume returns ErrUnsupported.
func Resume(bus int, path []int) error { return ErrUnsupported }
