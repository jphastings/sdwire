package blockdev

import (
	"fmt"
	"sort"
	"strings"
)

// packLocationID computes macOS's packed USB locationID for a device at a
// given bus and USB port path: the bus number occupies the top byte, and
// each port number (1-based, as gousb's DeviceDesc.Path reports) occupies
// successive nibbles from bit 20 down to bit 0, e.g. bus 1, ports [1,1,3]
// -> 0x01113000. Only the first 6 ports are representable; deeper ports are
// ignored (matching what macOS itself does — USB tops out well below that
// depth in practice).
func packLocationID(bus int, ports []int) uint32 {
	id := uint32(bus&0xFF) << 24
	for i, p := range ports {
		shift := 20 - 4*i
		if shift < 0 {
			break
		}
		id |= uint32(p&0xF) << shift
	}
	return id
}

// bsdNameFromPath strips the "/dev/" prefix from a whole-disk path,
// returning the bare BSD disk name (e.g. "/dev/disk4" -> "disk4").
func bsdNameFromPath(path string) string {
	return strings.TrimPrefix(path, "/dev/")
}

// findDarwin parses `ioreg -a -r -c IOUSBHostDevice -l` XML plist output
// (data) and locates the whole-disk BSD name and size for the device
// identified by ref, matched by locationID (see packLocationID),
// idVendor, and idProduct. It errors if no device matches, or if the
// matched device(s) collectively yield more than one whole-disk BSD name.
func findDarwin(data []byte, ref Ref) (bsdName string, sizeBytes int64, err error) {
	root, err := decodePlist(data)
	if err != nil {
		return "", 0, fmt.Errorf("blockdev: parsing ioreg output: %w", err)
	}

	wantLoc := packLocationID(ref.Bus, ref.PortPath)
	disks := collectWholeDisks(root, wantLoc, ref.Vendor, ref.Product)

	switch len(disks) {
	case 0:
		return "", 0, fmtNotFound(ref)
	case 1:
		for name, size := range disks {
			return name, size, nil
		}
	}

	names := make([]string, 0, len(disks))
	for name := range disks {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", 0, fmtAmbiguous(names)
}

// collectWholeDisks recursively searches node (the decoded plist root, or
// any value within it) for USB device dicts matching the given locationID,
// idVendor, and idProduct, and DFS's each match's children for whole-disk
// IOMedia entries, returning their BSD names and sizes.
//
// A real IOUSBHostDevice can appear more than once in ioreg's output at the
// same locationID — as its own top-level subtree root and again nested
// under an ancestor hub's subtree, and (observed in practice) even as
// distinct sibling registry entries for the same physical device, only one
// of which has media attached. Rather than picking a single "match" and
// discarding the rest, every matching occurrence is DFS'd and the results
// are merged into a map keyed by BSD name, which naturally collapses any
// duplicate discoveries of the same physical disk.
func collectWholeDisks(node any, wantLoc uint32, vendor, product uint16) map[string]int64 {
	disks := map[string]int64{}
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if isMatchingUSBDevice(v, wantLoc, vendor, product) {
				collectWholeDisksUnder(v, disks)
			}
			walkChildren(v, walk)
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(node)
	return disks
}

func isMatchingUSBDevice(v map[string]any, wantLoc uint32, vendor, product uint16) bool {
	loc, ok := intField(v, "locationID")
	if !ok || uint32(loc) != wantLoc {
		return false
	}
	vid, ok := intField(v, "idVendor")
	if !ok || uint16(vid) != vendor {
		return false
	}
	pid, ok := intField(v, "idProduct")
	if !ok || uint16(pid) != product {
		return false
	}
	return true
}

// collectWholeDisksUnder DFS's the children of a matched USB device dict,
// collecting every whole-disk IOMedia entry found (see isWholeDiskMedia)
// into disks.
func collectWholeDisksUnder(device map[string]any, disks map[string]int64) {
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if isWholeDiskMedia(v) {
				name, _ := v["BSD Name"].(string)
				size, _ := v["Size"].(int64)
				disks[name] = size
			}
			walkChildren(v, walk)
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walkChildren(device, walk)
}

// isWholeDiskMedia reports whether v is a whole-disk IOMedia entry. All
// three checks matter: driver-personality dicts elsewhere in the tree (e.g.
// an IOFDiskPartitionScheme's IOPropertyMatch) carry their own "Whole" key
// without being IOMedia at all, and non-disk BSD names (network interfaces
// like "en15") exist alongside real disks — the IOObjectClass check is what
// excludes both.
func isWholeDiskMedia(v map[string]any) bool {
	class, _ := v["IOObjectClass"].(string)
	if class != "IOMedia" {
		return false
	}
	whole, _ := v["Whole"].(bool)
	if !whole {
		return false
	}
	name, ok := v["BSD Name"].(string)
	return ok && name != ""
}

// sizeForBSDName parses `ioreg -a -r -c IOUSBHostDevice -l` XML plist
// output (data) and returns the size in bytes of the IOMedia entry with the
// given BSD name.
func sizeForBSDName(data []byte, bsdName string) (int64, error) {
	root, err := decodePlist(data)
	if err != nil {
		return 0, fmt.Errorf("blockdev: parsing ioreg output: %w", err)
	}

	size, ok := findMediaSize(root, bsdName)
	if !ok {
		return 0, fmt.Errorf("blockdev: disk %q not found in ioreg output", bsdName)
	}
	return size, nil
}

func findMediaSize(node any, bsdName string) (int64, bool) {
	switch v := node.(type) {
	case map[string]any:
		if class, _ := v["IOObjectClass"].(string); class == "IOMedia" {
			if name, _ := v["BSD Name"].(string); name == bsdName {
				size, _ := v["Size"].(int64)
				return size, true
			}
		}
		children, _ := v["IORegistryEntryChildren"].([]any)
		return findMediaSize(children, bsdName)
	case []any:
		for _, c := range v {
			if size, ok := findMediaSize(c, bsdName); ok {
				return size, true
			}
		}
	}
	return 0, false
}

func intField(v map[string]any, key string) (int64, bool) {
	n, ok := v[key].(int64)
	return n, ok
}

// walkChildren calls fn with each element of v's "IORegistryEntryChildren"
// array, if present.
func walkChildren(v map[string]any, fn func(any)) {
	children, ok := v["IORegistryEntryChildren"].([]any)
	if !ok {
		return
	}
	for _, c := range children {
		fn(c)
	}
}
