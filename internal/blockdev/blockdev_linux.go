//go:build linux

package blockdev

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Find returns the whole-disk block device path (e.g. /dev/sdb) for the USB
// device identified by ref. It walks /sys/bus/usb/devices/<bus>-<ports...>,
// verifying idVendor/idProduct, and looks for a "block" directory beneath
// it. It errors if no device matches, or if more than one block device name
// is found.
func Find(ref Ref) (string, error) {
	name, err := findLinux(os.DirFS("/sys/bus/usb/devices"), ref)
	if err != nil {
		return "", err
	}
	return "/dev/" + name, nil
}

// Size returns the device's size in bytes, read from
// /sys/block/<name>/size (which reports 512-byte sectors).
func Size(devPath string) (int64, error) {
	return sizeLinux(os.DirFS("/sys/block"), diskNameFromPath(devPath))
}

// Unmount unmounts every mounted partition (or the whole disk itself) of
// the device at devPath, by parsing /proc/self/mounts and exec'ing umount
// on each mountpoint found.
func Unmount(devPath string) error {
	diskName := diskNameFromPath(devPath)
	mounts, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return fmt.Errorf("blockdev: reading /proc/self/mounts: %w", err)
	}

	var errs []error
	for _, mountpoint := range mountpointsForDisk(mounts, diskName) {
		if out, err := exec.Command("umount", mountpoint).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("umount %s: %w: %s", mountpoint, err, out))
		}
	}
	return errors.Join(errs...)
}
