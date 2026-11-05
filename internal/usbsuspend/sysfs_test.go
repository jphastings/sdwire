package usbsuspend

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSysfs lays out a USB device at bus 1 port path [1 2] under a temp
// sysfs root, with the given attributes (paths relative to the device).
func fakeSysfs(t *testing.T, attrs map[string]string) sysfsDevice {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "1-1.2")
	for name, value := range attrs {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return sysfsDeviceAt(filepath.Dir(dir), 1, []int{1, 2})
}

var hostMode = map[string]string{
	"busnum": "1", "devnum": "7",
	"power/runtime_status": "active", "power/control": "on", "power/autosuspend_delay_ms": "2000",
	"1-1.2:1.0/driver/bind": "",
	"serial":                "20120501030900000", "product": "USB3.0-CRW", "manufacturer": "Generic",
}

func TestSysfsState(t *testing.T) {
	cases := map[string]struct {
		attrs map[string]string
		want  State
	}{
		"no device directory":        {nil, Absent},
		"storage driver bound":       {hostMode, Configured},
		"runtime-suspended":          {map[string]string{"power/runtime_status": "suspended"}, Suspended},
		"awake with no driver bound": {map[string]string{"power/runtime_status": "active"}, Unconfigured},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := fakeSysfs(t, c.attrs).state(); got != c.want {
				t.Errorf("state = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSysfsStringsComeFromSysfs(t *testing.T) {
	serial, product, manufacturer, err := fakeSysfs(t, hostMode).strings(1, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if serial != "20120501030900000" || product != "USB3.0-CRW" || manufacturer != "Generic" {
		t.Errorf("strings = %q %q %q", serial, product, manufacturer)
	}
}

func TestSysfsSuspendEnablesAutosuspendAndDetachesTheDriver(t *testing.T) {
	d := fakeSysfs(t, hostMode)
	var calls []string
	err := d.suspend(func(node string, attach bool) error {
		calls = append(calls, node)
		if attach {
			t.Error("suspend re-attached the driver")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if d.attr("power/control") != "auto" || d.attr("power/autosuspend_delay_ms") != "0" {
		t.Errorf("power/control = %q, autosuspend_delay_ms = %q", d.attr("power/control"), d.attr("power/autosuspend_delay_ms"))
	}
	if len(calls) != 1 || calls[0] != "/dev/bus/usb/001/007" {
		t.Errorf("usbfs calls = %v, want one detach via /dev/bus/usb/001/007", calls)
	}
}

// Opening the usbfs node wakes the device, so one with nothing to detach
// must not be touched.
func TestSysfsSuspendLeavesAnUnboundDeviceAlone(t *testing.T) {
	d := fakeSysfs(t, map[string]string{"busnum": "1", "devnum": "7", "power/control": "auto", "power/autosuspend_delay_ms": "0"})
	err := d.suspend(func(string, bool) error {
		t.Error("opened the usbfs node of a device with no driver bound")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSysfsResumeReattachesTheDriver(t *testing.T) {
	var attached bool
	err := fakeSysfs(t, map[string]string{"busnum": "1", "devnum": "7"}).resume(func(_ string, attach bool) error {
		attached = attach
		return nil
	})
	if err != nil || !attached {
		t.Errorf("resume: err = %v, attached = %v", err, attached)
	}
}
