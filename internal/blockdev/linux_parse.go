package blockdev

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// maxUSBDeviceWalkDepth bounds how far below a USB device's sysfs directory
// findLinux will descend looking for "block" directories, matching the
// <dev>:<cfg>.<intf>/host*/target*/<hctl>/block/<name> shape (or shallower,
// e.g. for mmcblk devices) without risking an unbounded walk.
const maxUSBDeviceWalkDepth = 6

// usbDeviceDirName returns the sysfs device directory name for a device at
// a given bus and USB port path, e.g. bus 1, path [1,1,3] -> "1-1.1.3".
func usbDeviceDirName(bus int, path []int) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strconv.Itoa(p)
	}
	return fmt.Sprintf("%d-%s", bus, strings.Join(parts, "."))
}

func readHexFile(fsys fs.FS, p string) (uint16, error) {
	data, err := fs.ReadFile(fsys, p)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 16, 16)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", p, err)
	}
	return uint16(v), nil
}

// findLinux locates the block device name(s) backing the USB device
// identified by ref, within fsys (rooted at /sys/bus/usb/devices in
// production). It verifies the device at ref's bus/port path has the
// expected idVendor/idProduct, then walks its sysfs subtree (bounded to
// maxUSBDeviceWalkDepth) for directories named "block", collecting their
// entries as candidate block device names.
func findLinux(fsys fs.FS, ref Ref) (string, error) {
	entry := usbDeviceDirName(ref.Bus, ref.PortPath)

	vendor, err := readHexFile(fsys, path.Join(entry, "idVendor"))
	if err != nil {
		return "", fmtNotFound(ref)
	}
	product, err := readHexFile(fsys, path.Join(entry, "idProduct"))
	if err != nil {
		return "", fmtNotFound(ref)
	}
	if vendor != ref.Vendor || product != ref.Product {
		return "", fmtNotFound(ref)
	}

	var names []string
	err = fs.WalkDir(fsys, entry, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != entry {
			rel := strings.TrimPrefix(p, entry+"/")
			if depth := strings.Count(rel, "/"); depth > maxUSBDeviceWalkDepth {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() && d.Name() == "block" {
			entries, err := fs.ReadDir(fsys, p)
			if err != nil {
				return err
			}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("blockdev: walking %s: %w", entry, err)
	}

	switch len(names) {
	case 0:
		return "", fmtNotFound(ref)
	case 1:
		return names[0], nil
	default:
		return "", fmtAmbiguous(names)
	}
}

// sizeLinux reads a block device's size in 512-byte sectors from
// /sys/block/<name>/size (fsys rooted at /sys/block in production) and
// returns it in bytes.
func sizeLinux(fsys fs.FS, name string) (int64, error) {
	data, err := fs.ReadFile(fsys, path.Join(name, "size"))
	if err != nil {
		return 0, fmt.Errorf("blockdev: reading %s size: %w", name, err)
	}
	sectors, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("blockdev: parsing %s size: %w", name, err)
	}
	return sectors * 512, nil
}

// diskNameFromPath strips the "/dev/" prefix from a whole-disk path,
// returning the bare device name (e.g. "/dev/sdb" -> "sdb").
func diskNameFromPath(path string) string {
	return strings.TrimPrefix(path, "/dev/")
}

// isPartitionOf reports whether candidate (a bare device name, e.g. "sdb1"
// or "mmcblk0p1") is diskName itself or one of its numbered partitions.
// Devices whose base name ends in a digit (mmcblk0, nvme0n1) require the
// kernel's "p"-infixed partition form (mmcblk0p1): without it, "mmcblk01"
// would be indistinguishable from another whole disk's own numbering
// (mmcblk0, mmcblk1, ... are separate disks, not partitions of each other).
func isPartitionOf(diskName, candidate string) bool {
	if candidate == diskName {
		return true
	}
	rest, ok := strings.CutPrefix(candidate, diskName)
	if !ok || rest == "" {
		return false
	}
	if endsInDigit(diskName) {
		rest, ok = strings.CutPrefix(rest, "p")
		if !ok {
			return false
		}
	}
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func endsInDigit(s string) bool {
	return s != "" && s[len(s)-1] >= '0' && s[len(s)-1] <= '9'
}

var mountFieldUnescaper = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// mountpointsForDisk parses /proc/self/mounts-formatted data and returns the
// mountpoints of every mounted partition (or the whole disk itself) of
// diskName (a bare device name, e.g. "sdb" or "mmcblk0").
func mountpointsForDisk(mounts []byte, diskName string) []string {
	var points []string
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		dev := strings.TrimPrefix(fields[0], "/dev/")
		if isPartitionOf(diskName, dev) {
			points = append(points, mountFieldUnescaper.Replace(fields[1]))
		}
	}
	return points
}
