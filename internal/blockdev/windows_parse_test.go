package blockdev

import (
	"errors"
	"testing"
)

var windowsRef = Ref{Vendor: 0x0BDA, Product: 0x0316}

const windowsFixtureSingle = `[
  {
    "DeviceID": "\\\\.\\PHYSICALDRIVE0",
    "PNPDeviceID": "USBSTOR\\DISK&VEN_GENERIC&PROD_FLASH_DISK\\5&1234&0",
    "Size": 500107862016,
    "Index": 0
  },
  {
    "DeviceID": "\\\\.\\PHYSICALDRIVE2",
    "PNPDeviceID": "USBSTOR\\DISK&VEN_GENERIC&PROD_SDWIRE3\\VID_0BDA&PID_0316&REV_0100\\6&abcd&0",
    "Size": 15931539456,
    "Index": 2
  }
]`

const windowsFixtureSingleResult = `{
  "DeviceID": "\\\\.\\PHYSICALDRIVE2",
  "PNPDeviceID": "USBSTOR\\DISK&VEN_GENERIC&PROD_SDWIRE3\\VID_0BDA&PID_0316&REV_0100\\6&abcd&0",
  "Size": 15931539456,
  "Index": 2
}`

const windowsFixtureAmbiguous = `[
  {
    "DeviceID": "\\\\.\\PHYSICALDRIVE2",
    "PNPDeviceID": "USBSTOR\\DISK&VEN_GENERIC&PROD_SDWIRE3\\VID_0BDA&PID_0316&REV_0100\\6&abcd&0",
    "Size": 15931539456,
    "Index": 2
  },
  {
    "DeviceID": "\\\\.\\PHYSICALDRIVE3",
    "PNPDeviceID": "USBSTOR\\DISK&VEN_GENERIC&PROD_SDWIRE3\\VID_0BDA&PID_0316&REV_0100\\6&efgh&0",
    "Size": 15931539456,
    "Index": 3
  }
]`

func TestParseDiskDrivesArray(t *testing.T) {
	drives, err := parseDiskDrives([]byte(windowsFixtureSingle))
	if err != nil {
		t.Fatalf("parseDiskDrives: %v", err)
	}
	if len(drives) != 2 {
		t.Fatalf("len(drives) = %d, want 2", len(drives))
	}
}

func TestParseDiskDrivesSingleObject(t *testing.T) {
	drives, err := parseDiskDrives([]byte(windowsFixtureSingleResult))
	if err != nil {
		t.Fatalf("parseDiskDrives: %v", err)
	}
	if len(drives) != 1 {
		t.Fatalf("len(drives) = %d, want 1", len(drives))
	}
	if drives[0].DeviceID != `\\.\PHYSICALDRIVE2` {
		t.Errorf("DeviceID = %q", drives[0].DeviceID)
	}
}

func TestFindWindowsFound(t *testing.T) {
	deviceID, size, err := findWindows([]byte(windowsFixtureSingle), windowsRef)
	if err != nil {
		t.Fatalf("findWindows: %v", err)
	}
	if deviceID != `\\.\PHYSICALDRIVE2` {
		t.Errorf("deviceID = %q, want \\\\.\\PHYSICALDRIVE2", deviceID)
	}
	if size != 15931539456 {
		t.Errorf("size = %d, want 15931539456", size)
	}
}

func TestFindWindowsNoMatch(t *testing.T) {
	noMatch := `[{"DeviceID":"\\\\.\\PHYSICALDRIVE0","PNPDeviceID":"USBSTOR\\DISK&VEN_GENERIC&PROD_FLASH_DISK","Size":1,"Index":0}]`
	_, _, err := findWindows([]byte(noMatch), windowsRef)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFindWindowsAmbiguous(t *testing.T) {
	_, _, err := findWindows([]byte(windowsFixtureAmbiguous), windowsRef)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
}
