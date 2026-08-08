package sdwire

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// errNoDeviceFound is wrapped into every "not found" error returned by the
// selectBy* functions (and used directly by New()). It lets connect()
// distinguish "nothing matched" — worth trying the hubpower cache fallback
// for — from an ambiguous-match error, which should be returned as-is
// rather than silently resolved by a cache lookup.
var errNoDeviceFound = errors.New("no matching SDWire device found")

// Location returns the device's USB topology location in Linux sysfs style,
// e.g. "1-1.1.3". Devices with no parent hub ports in their path (an empty
// PortPath) return just the bus number, e.g. "1".
func (d DeviceInfo) Location() string {
	return formatLocation(d.Bus, d.PortPath)
}

// Identity returns the device's serial number, suffixed with its USB port
// path, e.g. "20120501030900000.1.1.3". This matches the identity format
// used by the Badger-Embedded Python sdwire CLI, and is needed because all
// Realtek SDWire3 devices share the same hardcoded USB serial number. A
// device with an empty PortPath returns its bare serial number.
func (d DeviceInfo) Identity() string {
	return formatIdentity(d.Serial, d.PortPath)
}

func formatLocation(bus int, path []int) string {
	if len(path) == 0 {
		return strconv.Itoa(bus)
	}
	return strconv.Itoa(bus) + "-" + joinInts(path)
}

func formatIdentity(serial string, path []int) string {
	if len(path) == 0 {
		return serial
	}
	return serial + "." + joinInts(path)
}

func joinInts(ints []int) string {
	parts := make([]string, len(ints))
	for i, v := range ints {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ".")
}

func parseInts(s string) ([]int, bool) {
	fields := strings.Split(s, ".")
	ints := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		ints[i] = n
	}
	return ints, true
}

// splitIdentity splits a NewWithSerial query into a serial and, if the query
// carries a dot-separated port-path suffix, that path. A query with no dot,
// or a malformed suffix, is treated as a bare serial with no path
// constraint.
func splitIdentity(id string) (serial string, path []int, hasSuffix bool) {
	serial, rest, found := strings.Cut(id, ".")
	if !found {
		return id, nil, false
	}
	path, ok := parseInts(rest)
	if !ok {
		return id, nil, false
	}
	return serial, path, true
}

// parseLocation parses the DeviceInfo.Location() form "<bus>[-<path...>]".
func parseLocation(s string) (bus int, path []int, ok bool) {
	busPart, pathPart, hasDash := strings.Cut(s, "-")
	bus, err := strconv.Atoi(busPart)
	if err != nil {
		return 0, nil, false
	}
	if !hasDash {
		return bus, nil, true
	}
	path, ok = parseInts(pathPart)
	if !ok {
		return 0, nil, false
	}
	return bus, path, true
}

// pathMatches reports whether suffix identifies (bus, portPath), tolerating
// a suffix given either as the bare port path or as the bus number
// prepended to it — matching whichever form the caller supplied.
func pathMatches(suffix []int, bus int, portPath []int) bool {
	if intsEqual(suffix, portPath) {
		return true
	}
	withBus := make([]int, 0, len(portPath)+1)
	withBus = append(withBus, bus)
	withBus = append(withBus, portPath...)
	return intsEqual(suffix, withBus)
}

func intsEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// selectBySerial resolves a NewWithSerial query (a plain serial or a
// suffixed Identity() form) against candidates.
func selectBySerial(candidates []DeviceInfo, query string) (int, error) {
	serial, path, _ := splitIdentity(query)
	return selectBySerialAndPath(candidates, serial, path)
}

func selectBySerialAndPath(candidates []DeviceInfo, serial string, path []int) (int, error) {
	var matches []int
	for i, c := range candidates {
		if c.Serial != serial {
			continue
		}
		if path == nil || pathMatches(path, c.Bus, c.PortPath) {
			matches = append(matches, i)
		}
	}
	return resolveMatch(candidates, matches, fmt.Sprintf("serial %q", serial))
}

// selectByIdentity resolves a NewWithIdentity query, which may be either the
// suffixed Identity() form ("<serial>.<path...>") or the Location() form
// ("<bus>[-<path...>]").
func selectByIdentity(candidates []DeviceInfo, query string) (int, error) {
	if bus, path, ok := parseLocation(query); ok {
		return selectByLocation(candidates, bus, path)
	}
	serial, path, _ := splitIdentity(query)
	return selectBySerialAndPath(candidates, serial, path)
}

func selectByLocation(candidates []DeviceInfo, bus int, path []int) (int, error) {
	var matches []int
	for i, c := range candidates {
		if c.Bus == bus && intsEqual(c.PortPath, path) {
			matches = append(matches, i)
		}
	}
	return resolveMatch(candidates, matches, fmt.Sprintf("location %s", formatLocation(bus, path)))
}

func resolveMatch(candidates []DeviceInfo, matches []int, what string) (int, error) {
	switch len(matches) {
	case 0:
		return 0, fmt.Errorf("no SDWire device matching %s found: %w", what, errNoDeviceFound)
	case 1:
		return matches[0], nil
	default:
		identities := make([]string, len(matches))
		for i, idx := range matches {
			identities[i] = candidates[idx].Identity()
		}
		return 0, fmt.Errorf("%s matches multiple SDWire devices; specify one of: %s", what, strings.Join(identities, ", "))
	}
}
