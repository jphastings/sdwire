//go:build linux

package usbsuspend

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// Supported reports whether this platform gives userspace USB suspend
// control.
const Supported = true

const sysfsRoot = "/sys/bus/usb/devices"

// StateOf reads the device's state from sysfs.
func StateOf(bus int, path []int) (State, error) {
	return sysfsDeviceAt(sysfsRoot, bus, path).state(), nil
}

// Strings returns the device's serial number, product and manufacturer
// strings from sysfs.
func Strings(bus int, path []int) (serial, product, manufacturer string, err error) {
	return sysfsDeviceAt(sysfsRoot, bus, path).strings(bus, path)
}

// Suspend enables runtime autosuspend and detaches the device's kernel
// driver, after which the kernel suspends it.
func Suspend(bus int, path []int) error {
	return sysfsDeviceAt(sysfsRoot, bus, path).suspend(usbfsReattach)
}

// Resume wakes the device and re-attaches its kernel driver.
func Resume(bus int, path []int) error {
	return sysfsDeviceAt(sysfsRoot, bus, path).resume(usbfsReattach)
}

// struct usbdevfs_ioctl, <linux/usbdevice_fs.h>.
type usbdevfsIoctl struct {
	ifno, ioctlCode int32
	data            uintptr
}

// ponytail: the asm-generic ioctl encoding (x86, arm, arm64, riscv);
// powerpc, mips and sparc encode direction and size differently.
const (
	usbdevfsIoctlReq   = 3<<30 | unsafe.Sizeof(usbdevfsIoctl{})<<16 | 'U'<<8 | 18 // _IOWR('U', 18, struct usbdevfs_ioctl)
	usbdevfsReset      = 'U'<<8 | 20                                              // _IO('U', 20)
	usbdevfsDisconnect = 'U'<<8 | 22                                              // _IO('U', 22)
	usbdevfsConnect    = 'U'<<8 | 23                                              // _IO('U', 23)
)

func usbfsReattach(node string, attach bool) error {
	f, err := os.OpenFile(node, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("usbsuspend: %w (%s)", err, udevHint)
	}
	defer f.Close()

	// ENODATA (no driver to detach) and EBUSY (one already attached) mean
	// interface 0 is already where it is being moved to.
	req, already, action := usbdevfsIoctl{ioctlCode: usbdevfsDisconnect}, syscall.ENODATA, "detaching"
	if attach {
		req.ioctlCode, already, action = usbdevfsConnect, syscall.EBUSY, "attaching"
	}
	if err := ioctl(f, usbdevfsIoctlReq, unsafe.Pointer(&req)); err != nil && !errors.Is(err, already) {
		return fmt.Errorf("usbsuspend: %s the kernel driver of %s: %w", action, node, err)
	}
	if err := ioctl(f, usbdevfsReset, nil); err != nil {
		return fmt.Errorf("usbsuspend: resetting %s: %w", node, err)
	}
	return nil
}

func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
