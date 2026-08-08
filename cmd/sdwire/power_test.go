package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jphastings/sdwire"
)

func TestExplainMissingPowerConfig(t *testing.T) {
	msg := explainMissingPowerConfig("bench", "1-1.1.3")
	for _, want := range []string{"power:", "type: meross", "bench", "1-1.1.3", "meross"} {
		if !strings.Contains(msg, want) {
			t.Errorf("explainMissingPowerConfig output missing %q:\n%s", want, msg)
		}
	}
}

func TestExplainMissingPowerConfigPlaceholders(t *testing.T) {
	msg := explainMissingPowerConfig("", "")
	if !strings.Contains(msg, "mydevice") {
		t.Errorf("expected placeholder device key \"mydevice\":\n%s", msg)
	}
}

func TestPowerCommandUnknownArgIsUsageError(t *testing.T) {
	code := Execute([]string{"power", "sideways"})
	if code != 2 {
		t.Errorf("Execute([power sideways]) = %d, want 2", code)
	}
}

func TestResolvePowerSelection(t *testing.T) {
	cfg := &Config{
		Devices: map[string]DeviceConfig{
			"bench":   {Location: "1-1.1.3", Power: map[string]any{"type": "meross"}},
			"nopower": {Serial: "999"},
		},
	}

	t.Run("named device with power", func(t *testing.T) {
		name, power, loc, err := resolvePowerSelection("bench", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "bench" || power["type"] != "meross" || loc != "1-1.1.3" {
			t.Errorf("got name=%q power=%v loc=%q", name, power, loc)
		}
	})

	t.Run("named device without power", func(t *testing.T) {
		name, power, _, err := resolvePowerSelection("nopower", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "nopower" || power != nil {
			t.Errorf("got name=%q power=%v, want nil power", name, power)
		}
	})

	t.Run("unnamed with exactly one configured power device is used", func(t *testing.T) {
		soloCfg := &Config{Devices: map[string]DeviceConfig{
			"only": {Location: "1-1", Power: map[string]any{"type": "meross"}},
		}}
		name, power, loc, err := resolvePowerSelection("", soloCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "only" || power == nil || loc != "1-1" {
			t.Errorf("got name=%q power=%v loc=%q", name, power, loc)
		}
	})

	t.Run("unnamed with zero configured power devices", func(t *testing.T) {
		zeroCfg := &Config{Devices: map[string]DeviceConfig{"nopower": {}}}
		name, power, _, err := resolvePowerSelection("", zeroCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "" || power != nil {
			t.Errorf("got name=%q power=%v, want both empty", name, power)
		}
	})

	t.Run("unnamed with multiple configured power devices errors", func(t *testing.T) {
		multiCfg := &Config{Devices: map[string]DeviceConfig{
			"a": {Power: map[string]any{"type": "meross"}},
			"b": {Power: map[string]any{"type": "meross"}},
		}}
		_, _, _, err := resolvePowerSelection("", multiCfg)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
			t.Errorf("error should list both candidate device names: %v", err)
		}
	})
}

// stubUSBSeams replaces every USB-touching seam with a function that fails
// the test if called, restoring the originals on cleanup. Used to prove a
// command path never touches USB.
func stubUSBSeams(t *testing.T) {
	t.Helper()
	origList, origIdentity, origSerial, origNew, origCached :=
		sdwireListDevices, sdwireNewWithIdentity, sdwireNewWithSerial, sdwireNew, sdwireCachedPortState
	t.Cleanup(func() {
		sdwireListDevices, sdwireNewWithIdentity, sdwireNewWithSerial, sdwireNew, sdwireCachedPortState =
			origList, origIdentity, origSerial, origNew, origCached
	})
	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) {
		t.Fatal("sdwireListDevices should not be called by the power command")
		return nil, nil
	}
	sdwireNewWithIdentity = func(string, ...sdwire.Option) (*sdwire.SDWire, error) {
		t.Fatal("sdwireNewWithIdentity should not be called by the power command")
		return nil, nil
	}
	sdwireNewWithSerial = func(string, ...sdwire.Option) (*sdwire.SDWire, error) {
		t.Fatal("sdwireNewWithSerial should not be called by the power command")
		return nil, nil
	}
	sdwireNew = func(...sdwire.Option) (*sdwire.SDWire, error) {
		t.Fatal("sdwireNew should not be called by the power command")
		return nil, nil
	}
	sdwireCachedPortState = func(string, ...sdwire.Option) (sdwire.SwitchMode, string, error) {
		t.Fatal("sdwireCachedPortState should not be called by the power command")
		return sdwire.ModeUnknown, "", nil
	}
}

// stubPowerRegistry replaces the power plugin registry with a single "stub"
// type recording each on/off call into calls, restoring the original on
// cleanup.
func stubPowerRegistry(t *testing.T, calls *[]string) {
	t.Helper()
	orig := powerRegistry
	t.Cleanup(func() { powerRegistry = orig })
	powerRegistry = map[string]PowerFactory{
		"stub": func(config map[string]any) (sdwire.PowerFunc, error) {
			return func(on bool) error {
				if on {
					*calls = append(*calls, "on")
				} else {
					*calls = append(*calls, "off")
				}
				return nil
			}, nil
		},
	}
}

func TestPowerCommandNeverTouchesUSB(t *testing.T) {
	stubUSBSeams(t)
	var calls []string
	stubPowerRegistry(t, &calls)

	cfgPath := writeTestConfig(t, `
devices:
  bench:
    power:
      type: stub
default_device: bench
`)

	flags := &globalFlags{config: cfgPath}
	cmd := newPowerCmd(flags)
	cmd.SetArgs([]string{"on"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("power on: %v", err)
	}
	if len(calls) != 1 || calls[0] != "on" {
		t.Errorf("calls = %v, want [on]", calls)
	}
}

func TestPowerCycleSleepsAtLeastMinOffAndOrdersOffThenOn(t *testing.T) {
	stubUSBSeams(t)
	var calls []string
	stubPowerRegistry(t, &calls)

	origSleep := sleepFn
	var sleptFor time.Duration
	sleepFn = func(d time.Duration) { sleptFor = d }
	t.Cleanup(func() { sleepFn = origSleep })

	cfgPath := writeTestConfig(t, `
devices:
  bench:
    power:
      type: stub
default_device: bench
min_off_seconds: 3
`)

	flags := &globalFlags{config: cfgPath}
	cmd := newPowerCmd(flags)
	cmd.SetArgs([]string{"cycle"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("power cycle: %v", err)
	}

	if len(calls) != 2 || calls[0] != "off" || calls[1] != "on" {
		t.Errorf("calls = %v, want [off on]", calls)
	}
	if sleptFor < 3*time.Second {
		t.Errorf("sleptFor = %v, want >= 3s", sleptFor)
	}
}
