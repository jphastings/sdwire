package usbsuspend

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jphastings/sdwire/internal/blockdev"
)

const udevHint = "install the udev rule from the sdwire README, or run as root"

// sysfsDevice is a USB device's directory under /sys/bus/usb/devices.
//
// Linux resumes a device for as long as anything holds its usbfs node
// open, so everything read-only here comes from sysfs instead.
type sysfsDevice string

func sysfsDeviceAt(root string, bus int, path []int) sysfsDevice {
	return sysfsDevice(filepath.Join(root, blockdev.SysfsName(bus, path)))
}

func (d sysfsDevice) attr(name string) string {
	data, _ := os.ReadFile(filepath.Join(string(d), name))
	return strings.TrimSpace(string(data))
}

func (d sysfsDevice) state() State {
	if _, err := os.Stat(string(d)); err != nil {
		return Absent
	}
	if d.attr("power/runtime_status") == "suspended" {
		return Suspended
	}
	if d.driverBound() {
		return Configured
	}
	return Unconfigured
}

// driverBound reports whether a kernel driver is bound to interface 0
// (sysfs <name>:<config>.0/driver).
func (d sysfsDevice) driverBound() bool {
	matches, _ := filepath.Glob(filepath.Join(string(d), filepath.Base(string(d))+":*.0", "driver"))
	return len(matches) > 0
}

func (d sysfsDevice) strings(bus int, path []int) (serial, product, manufacturer string, err error) {
	if d.state() == Absent {
		return "", "", "", errNoDevice(bus, path)
	}
	return d.attr("serial"), d.attr("product"), d.attr("manufacturer"), nil
}

// enableAutosuspend lets the kernel suspend the device as soon as nothing
// holds it awake.
func (d sysfsDevice) enableAutosuspend() error {
	for _, a := range [][2]string{{"power/autosuspend_delay_ms", "0"}, {"power/control", "auto"}} {
		if d.attr(a[0]) == a[1] {
			continue
		}
		if err := os.WriteFile(filepath.Join(string(d), a[0]), []byte(a[1]), 0); err != nil {
			return fmt.Errorf("usbsuspend: enabling autosuspend: %w (%s)", err, udevHint)
		}
	}
	return nil
}

// usbfsNode is the device's usbfs node, e.g. /dev/bus/usb/001/004.
func (d sysfsDevice) usbfsNode() (string, error) {
	bus, err := strconv.Atoi(d.attr("busnum"))
	if err != nil {
		return "", fmt.Errorf("usbsuspend: reading busnum of %s: %w", d, err)
	}
	dev, err := strconv.Atoi(d.attr("devnum"))
	if err != nil {
		return "", fmt.Errorf("usbsuspend: reading devnum of %s: %w", d, err)
	}
	return fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, dev), nil
}

// reattachFunc detaches (attach false) or re-attaches interface 0's kernel
// driver through the usbfs node, then resets the device.
type reattachFunc func(node string, attach bool) error

// suspend lets the device autosuspend and detaches the driver that would
// otherwise keep it awake. It suspends once the usbfs node is closed.
func (d sysfsDevice) suspend(reattach reattachFunc) error {
	if err := d.enableAutosuspend(); err != nil {
		return err
	}
	if !d.driverBound() {
		return nil
	}
	node, err := d.usbfsNode()
	if err != nil {
		return err
	}
	return reattach(node, false)
}

// resume wakes the device (opening its usbfs node does) and re-attaches
// its driver.
func (d sysfsDevice) resume(reattach reattachFunc) error {
	node, err := d.usbfsNode()
	if err != nil {
		return err
	}
	return reattach(node, true)
}
