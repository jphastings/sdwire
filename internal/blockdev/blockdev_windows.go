//go:build windows

package blockdev

import (
	"fmt"
	"os/exec"
	"strings"
)

// Find returns the whole-disk device path (e.g. \\.\PhysicalDrive2) for the
// USB device identified by ref, by running `Get-CimInstance Win32_DiskDrive`
// and matching PNPDeviceID against ref's vendor/product.
//
// Windows exposes no reliable USB bus/port location via WMI, so ref.Bus and
// ref.PortPath are ignored: two identical readers cannot be distinguished
// this way, and Find returns an ErrAmbiguous error rather than guessing —
// detach one of them.
func Find(ref Ref) (string, error) {
	data, err := runGetDiskDrives()
	if err != nil {
		return "", err
	}
	deviceID, _, err := findWindows(data, ref)
	return deviceID, err
}

// Size returns the device's size in bytes, from Win32_DiskDrive's Size
// field.
func Size(devicePath string) (int64, error) {
	data, err := runGetDiskDrives()
	if err != nil {
		return 0, err
	}
	drives, err := parseDiskDrives(data)
	if err != nil {
		return 0, err
	}
	for _, d := range drives {
		if strings.EqualFold(d.DeviceID, devicePath) {
			return d.Size, nil
		}
	}
	return 0, fmt.Errorf("blockdev: disk %q not found", devicePath)
}

// Unmount is a best-effort no-op on Windows: WMI/CIM offers no simple way
// to dismount a physical disk's volumes without cgo or COM bindings (full
// support would need FSCTL_LOCK_VOLUME / FSCTL_DISMOUNT_VOLUME via
// DeviceIoControl). A volume that is still mounted normally causes the
// flashing layer's exclusive O_WRONLY open of the raw device to fail with a
// clear "in use" error instead, which the caller sees. Always returns nil.
func Unmount(devicePath string) error {
	return nil
}

func runGetDiskDrives() ([]byte, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-CimInstance Win32_DiskDrive | Select-Object DeviceID,PNPDeviceID,Size,Index | ConvertTo-Json").Output()
	if err != nil {
		return nil, fmt.Errorf("blockdev: running Get-CimInstance Win32_DiskDrive: %w", err)
	}
	return out, nil
}
