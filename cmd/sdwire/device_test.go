package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jphastings/sdwire"
)

func TestResolveSelection(t *testing.T) {
	cfg := &Config{
		DefaultDevice: "bench",
		Devices: map[string]DeviceConfig{
			"bench":  {Serial: "20120501030900000", Location: "1-1.1.3"},
			"second": {Serial: "99999999999999999"},
		},
	}

	cases := []struct {
		name       string
		serialFlag string
		cfg        *Config
		want       selection
	}{
		{"named device prefers location over serial", "bench", cfg, selection{selector: "1-1.1.3", deviceName: "bench", named: true}},
		{"named device without location falls back to serial", "second", cfg, selection{selector: "99999999999999999", deviceName: "second", named: true}},
		{"default device honored when -s empty", "", cfg, selection{selector: "1-1.1.3", deviceName: "bench", named: true}},
		{"literal value used as-is when not a configured name", "1-2.3", cfg, selection{selector: "1-2.3", named: true}},
		{"no selector and no default -> not named", "", &Config{}, selection{}},
		{"nil config treated as empty", "literal", nil, selection{selector: "literal", named: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveSelection(c.serialFlag, c.cfg)
			if got.selector != c.want.selector || got.deviceName != c.want.deviceName || got.named != c.want.named {
				t.Errorf("resolveSelection(%q, cfg) = %+v, want %+v", c.serialFlag, got, c.want)
			}
		})
	}
}

func TestConnectSelectorRouting(t *testing.T) {
	origIdentity, origSerial := sdwireNewWithIdentity, sdwireNewWithSerial
	t.Cleanup(func() { sdwireNewWithIdentity, sdwireNewWithSerial = origIdentity, origSerial })

	var identityCalledWith, serialCalledWith string
	sdwireNewWithIdentity = func(id string, opts ...sdwire.Option) (*sdwire.SDWire, error) {
		identityCalledWith = id
		return nil, errors.New("stub")
	}
	sdwireNewWithSerial = func(serial string, opts ...sdwire.Option) (*sdwire.SDWire, error) {
		serialCalledWith = serial
		return nil, errors.New("stub")
	}

	// Location form (has a dash) -> NewWithIdentity.
	identityCalledWith, serialCalledWith = "", ""
	connectSelector("1-1.1.3")
	if identityCalledWith != "1-1.1.3" || serialCalledWith != "" {
		t.Errorf("location selector: identity=%q serial=%q", identityCalledWith, serialCalledWith)
	}

	// Suffixed identity form (has dots) -> NewWithIdentity.
	identityCalledWith, serialCalledWith = "", ""
	connectSelector("20120501030900000.1.1.3")
	if identityCalledWith != "20120501030900000.1.1.3" || serialCalledWith != "" {
		t.Errorf("identity selector: identity=%q serial=%q", identityCalledWith, serialCalledWith)
	}

	// Bare numeric serial (no dot or dash) -> NewWithSerial, never
	// NewWithIdentity (which would misparse it as a bus-only location).
	identityCalledWith, serialCalledWith = "", ""
	connectSelector("20120501030900000")
	if serialCalledWith != "20120501030900000" || identityCalledWith != "" {
		t.Errorf("bare serial: identity=%q serial=%q", identityCalledWith, serialCalledWith)
	}
}

func TestOpenSelectedNoSelectorZeroDevices(t *testing.T) {
	orig := sdwireListDevices
	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) { return nil, nil }
	t.Cleanup(func() { sdwireListDevices = orig })

	_, err := openSelected("", &Config{}, false)
	if err == nil || !strings.Contains(err.Error(), "no SDWire devices found") {
		t.Fatalf("openSelected error = %v", err)
	}
	if !errors.Is(err, sdwire.ErrNoDeviceFound) {
		t.Errorf("expected error to wrap sdwire.ErrNoDeviceFound, got %v", err)
	}
	var oe *opError
	if !errors.As(err, &oe) {
		t.Errorf("expected an opError, got %T", err)
	}
}

func TestOpenSelectedNoSelectorZeroDevicesReviveCallsSdwireNew(t *testing.T) {
	origList, origNew := sdwireListDevices, sdwireNew
	t.Cleanup(func() { sdwireListDevices, sdwireNew = origList, origNew })

	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) { return nil, nil }
	called := false
	sdwireNew = func(opts ...sdwire.Option) (*sdwire.SDWire, error) {
		called = true
		return nil, errors.New("stub sdwireNew failure")
	}

	_, err := openSelected("", &Config{}, true)
	if !called {
		t.Error("expected sdwireNew to be called so the SDK's cache fallback can revive the sole cached device")
	}
	if err == nil {
		t.Fatal("expected the stub sdwireNew failure to propagate")
	}
}

func TestOpenSelectedNoSelectorZeroDevicesNoReviveNeverCallsSdwireNew(t *testing.T) {
	origList, origNew := sdwireListDevices, sdwireNew
	t.Cleanup(func() { sdwireListDevices, sdwireNew = origList, origNew })

	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) { return nil, nil }
	sdwireNew = func(opts ...sdwire.Option) (*sdwire.SDWire, error) {
		t.Fatal("sdwireNew should not be called when revive is false")
		return nil, nil
	}

	_, err := openSelected("", &Config{}, false)
	if err == nil || !errors.Is(err, sdwire.ErrNoDeviceFound) {
		t.Fatalf("openSelected error = %v, want it to wrap sdwire.ErrNoDeviceFound", err)
	}
}

func TestOpenSelectedNoSelectorMultipleDevicesListsCandidates(t *testing.T) {
	orig := sdwireListDevices
	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) {
		return []*sdwire.DeviceInfo{
			{Serial: "a", Bus: 1, PortPath: []int{1}},
			{Serial: "b", Bus: 1, PortPath: []int{2}},
		}, nil
	}
	t.Cleanup(func() { sdwireListDevices = orig })

	_, err := openSelected("", &Config{}, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "a.1") || !strings.Contains(err.Error(), "b.2") {
		t.Errorf("error should list both candidate identities: %v", err)
	}
}

func TestOpenSelectedNoSelectorSingleDeviceConnectsByIdentity(t *testing.T) {
	origList, origIdentity := sdwireListDevices, sdwireNewWithIdentity
	t.Cleanup(func() { sdwireListDevices, sdwireNewWithIdentity = origList, origIdentity })

	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) {
		return []*sdwire.DeviceInfo{{Serial: "solo", Bus: 1, PortPath: []int{3}}}, nil
	}
	var connectedWith string
	sdwireNewWithIdentity = func(id string, opts ...sdwire.Option) (*sdwire.SDWire, error) {
		connectedWith = id
		return nil, errors.New("stub connect failure")
	}

	_, err := openSelected("", &Config{}, false)
	if connectedWith != "solo.3" {
		t.Errorf("connected with %q, want the sole device's identity solo.3", connectedWith)
	}
	if err == nil {
		t.Fatal("expected the stub connect failure to propagate")
	}
	var oe *opError
	if !errors.As(err, &oe) {
		t.Errorf("expected an opError, got %T", err)
	}
}

func TestOpenSelectedNamedSkipsEnumeration(t *testing.T) {
	origList, origSerial := sdwireListDevices, sdwireNewWithSerial
	listCalled := false
	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) {
		listCalled = true
		return nil, nil
	}
	sdwireNewWithSerial = func(serial string, opts ...sdwire.Option) (*sdwire.SDWire, error) {
		return nil, errors.New("stub")
	}
	t.Cleanup(func() { sdwireListDevices, sdwireNewWithSerial = origList, origSerial })

	_, _ = openSelected("20120501030900000", &Config{}, false)
	if listCalled {
		t.Error("openSelected should not enumerate devices when a selector is given")
	}
}

func TestBlockdevRefForGeneration(t *testing.T) {
	sdwire3 := blockdevRefFor(sdwire.DeviceInfo{Generation: sdwire.GenerationSDWire3, Bus: 1, PortPath: []int{1, 2}})
	if sdwire3.Vendor != uint16(sdwire.SDWire3VID) || sdwire3.Product != uint16(sdwire.SDWire3PID) {
		t.Errorf("SDWire3 ref = %04x:%04x", sdwire3.Vendor, sdwire3.Product)
	}

	sdwireC := blockdevRefFor(sdwire.DeviceInfo{Generation: sdwire.GenerationSDWireC, Bus: 2})
	if sdwireC.Vendor != uint16(sdwire.SDWireCVID) || sdwireC.Product != uint16(sdwire.SDWireCPID) {
		t.Errorf("SDWireC ref = %04x:%04x", sdwireC.Vendor, sdwireC.Product)
	}
}
