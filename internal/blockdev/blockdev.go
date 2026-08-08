// Package blockdev locates, sizes, and prepares for writing the whole-disk
// block device backing a specific USB mass-storage device — typically an
// SDWire reader switched into host mode — identified by its USB
// vendor/product ID and its physical bus/port location.
//
// Find deliberately never guesses "whichever removable disk appeared": it
// ties its result to one specific attached device, and errors rather than
// picking among ambiguous candidates.
//
// Each OS backend is split into a pure, fixture-testable parsing/matching
// core (in an untagged file, compiled and tested on every platform) and a
// thin build-tagged shim that shells out or reads the live filesystem.
package blockdev

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Ref identifies the USB device whose block device we want.
type Ref struct {
	Vendor, Product uint16
	Bus             int
	PortPath        []int
}

// ErrNotFound is wrapped into the error Find returns when no block device
// matches a Ref.
var ErrNotFound = errors.New("blockdev: no matching device found")

// ErrAmbiguous is wrapped into the error Find returns when more than one
// candidate block device matches a Ref. Find never guesses among them.
var ErrAmbiguous = errors.New("blockdev: ambiguous match")

func fmtNotFound(ref Ref) error {
	return fmt.Errorf("%w: vendor=%04x product=%04x bus=%d port=%v", ErrNotFound, ref.Vendor, ref.Product, ref.Bus, ref.PortPath)
}

func fmtAmbiguous(candidates []string) error {
	return fmt.Errorf("%w: %s", ErrAmbiguous, strings.Join(candidates, ", "))
}

// RawWritePath converts a whole-disk path to the preferred raw-write node.
// On macOS, /dev/diskN is converted to /dev/rdiskN: the unbuffered
// character-device node, whose writes bypass the kernel's buffer cache and
// are dramatically faster for large sequential writes than the buffered
// block device. Other OSes make no such distinction and the path is
// returned unchanged.
func RawWritePath(path string) string {
	return rawWritePath(runtime.GOOS, path)
}

func rawWritePath(goos, path string) string {
	if goos != "darwin" {
		return path
	}
	const prefix = "/dev/disk"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	return "/dev/rdisk" + strings.TrimPrefix(path, prefix)
}
