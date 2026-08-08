package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jphastings/sdwire"
)

func TestResolveSwitchMode(t *testing.T) {
	cases := []struct {
		arg  string
		want sdwire.SwitchMode
	}{
		{"dut", sdwire.ModeTarget},
		{"target", sdwire.ModeTarget},
		{"host", sdwire.ModeHost},
		{"ts", sdwire.ModeHost},
	}
	for _, c := range cases {
		got, err := resolveSwitchMode(c.arg)
		if err != nil {
			t.Errorf("resolveSwitchMode(%q) unexpected error: %v", c.arg, err)
		}
		if got != c.want {
			t.Errorf("resolveSwitchMode(%q) = %v, want %v", c.arg, got, c.want)
		}
	}
}

func TestResolveSwitchModeOffRejected(t *testing.T) {
	_, err := resolveSwitchMode("off")
	if !errors.Is(err, errSwitchOffUnsupported) {
		t.Fatalf("resolveSwitchMode(\"off\") error = %v, want errSwitchOffUnsupported", err)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error message = %q, want it to explain why off is unsupported", err.Error())
	}
}

func TestSwitchCommandOffExitsOperationalWithoutTouchingHardware(t *testing.T) {
	// "off" is rejected before any device/config resolution happens, so
	// this must not attempt real USB/config access.
	code := Execute([]string{"switch", "off"})
	if code != 1 {
		t.Errorf("Execute([switch off]) = %d, want 1", code)
	}
}

func TestSwitchCommandUnknownArgIsUsageError(t *testing.T) {
	code := Execute([]string{"switch", "sideways"})
	if code != 2 {
		t.Errorf("Execute([switch sideways]) = %d, want 2", code)
	}
}

func TestSwitchDutNoLiveDeviceButAlreadyCachedTargetIsANoop(t *testing.T) {
	origList, origCached, origNew := sdwireListDevices, sdwireCachedPortState, sdwireNew
	t.Cleanup(func() { sdwireListDevices, sdwireCachedPortState, sdwireNew = origList, origCached, origNew })

	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) { return nil, nil }
	sdwireCachedPortState = func(selector string, opts ...sdwire.Option) (sdwire.SwitchMode, string, error) {
		return sdwire.ModeTarget, "20120501030900000.1.1.3", nil
	}
	sdwireNew = func(opts ...sdwire.Option) (*sdwire.SDWire, error) {
		t.Fatal("sdwireNew (revive) should not be called when the cached state is already Target")
		return nil, nil
	}

	flags := &globalFlags{config: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	cmd := newSwitchCmd(flags)
	cmd.SetArgs([]string{"dut"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
