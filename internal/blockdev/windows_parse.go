package blockdev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// win32DiskDrive is the shape of one Win32_DiskDrive entry produced by
// `Get-CimInstance Win32_DiskDrive | Select-Object DeviceID,PNPDeviceID,Size,Index | ConvertTo-Json`.
type win32DiskDrive struct {
	DeviceID    string `json:"DeviceID"`
	PNPDeviceID string `json:"PNPDeviceID"`
	Size        int64  `json:"Size"`
	Index       int    `json:"Index"`
}

// parseDiskDrives parses ConvertTo-Json output for Win32_DiskDrive.
// PowerShell's ConvertTo-Json emits a bare object (not a one-element array)
// when exactly one result is produced, so both shapes are handled.
func parseDiskDrives(data []byte) ([]win32DiskDrive, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		var drives []win32DiskDrive
		if err := json.Unmarshal(data, &drives); err != nil {
			return nil, fmt.Errorf("blockdev: parsing Win32_DiskDrive JSON: %w", err)
		}
		return drives, nil
	}
	var drive win32DiskDrive
	if err := json.Unmarshal(data, &drive); err != nil {
		return nil, fmt.Errorf("blockdev: parsing Win32_DiskDrive JSON: %w", err)
	}
	return []win32DiskDrive{drive}, nil
}

// findWindows locates the disk drive matching ref's vendor/product among
// parsed Win32_DiskDrive entries (data), by looking for "VID_xxxx&PID_xxxx"
// within PNPDeviceID.
//
// Windows' WMI/CIM disk drive class exposes no reliable USB bus/port
// location, so ref.Bus and ref.PortPath are not used: two readers sharing
// the same vendor/product cannot be distinguished, and findWindows returns
// an ErrAmbiguous error naming both DeviceIDs rather than guessing — the
// caller must detach one of them.
func findWindows(data []byte, ref Ref) (deviceID string, sizeBytes int64, err error) {
	drives, err := parseDiskDrives(data)
	if err != nil {
		return "", 0, err
	}

	want := fmt.Sprintf("VID_%04X&PID_%04X", ref.Vendor, ref.Product)
	var matches []win32DiskDrive
	for _, d := range drives {
		if strings.Contains(strings.ToUpper(d.PNPDeviceID), want) {
			matches = append(matches, d)
		}
	}

	switch len(matches) {
	case 0:
		return "", 0, fmtNotFound(ref)
	case 1:
		return matches[0].DeviceID, matches[0].Size, nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.DeviceID
		}
		return "", 0, fmt.Errorf("%w: %d readers with vendor=%04x product=%04x found (%s) and cannot be distinguished on Windows; detach all but one",
			ErrAmbiguous, len(matches), ref.Vendor, ref.Product, strings.Join(ids, ", "))
	}
}
