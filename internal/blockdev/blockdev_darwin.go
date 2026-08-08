//go:build darwin

package blockdev

import (
	"fmt"
	"os/exec"
)

// Find returns the whole-disk block device path (e.g. /dev/disk4) for the
// USB device identified by ref. It runs `ioreg -a -r -c IOUSBHostDevice -l`
// and matches devices by locationID (see packLocationID), idVendor, and
// idProduct. It errors if no device matches, or if the matched device's
// registry subtree yields more than one whole-disk BSD name.
//
// This used to run `system_profiler SPUSBDataType -json`, but as of macOS
// 26 that reliably returns an empty SPUSBDataType array, so ioreg — which
// still works — is used instead.
func Find(ref Ref) (string, error) {
	data, err := runIoreg()
	if err != nil {
		return "", err
	}
	bsdName, _, err := findDarwin(data, ref)
	if err != nil {
		return "", err
	}
	return "/dev/" + bsdName, nil
}

// Size returns the device's size in bytes, by re-running and re-parsing
// ioreg's IOMedia "Size" property for path's BSD disk name.
func Size(path string) (int64, error) {
	data, err := runIoreg()
	if err != nil {
		return 0, err
	}
	return sizeForBSDName(data, bsdNameFromPath(path))
}

// Unmount unmounts all mounted volumes of the whole-disk device at path via
// `diskutil unmountDisk`. A polite unmount of auto-mounted volumes can be
// dissented (loginwindow commonly holds them), so on failure it retries
// with `force` — appropriate here because the caller is about to overwrite
// the whole device anyway.
func Unmount(path string) error {
	politeOut, politeErr := exec.Command("diskutil", "unmountDisk", path).CombinedOutput()
	if politeErr == nil {
		return nil
	}
	out, err := exec.Command("diskutil", "unmountDisk", "force", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("blockdev: diskutil unmountDisk %s: %w: %s (polite attempt: %s)", path, err, out, politeOut)
	}
	return nil
}

func runIoreg() ([]byte, error) {
	data, err := exec.Command("ioreg", "-a", "-r", "-c", "IOUSBHostDevice", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("blockdev: running ioreg: %w", err)
	}
	return data, nil
}
