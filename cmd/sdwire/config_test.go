package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testConfigYAML = `
default_device: bench
devices:
  bench:
    serial: "20120501030900000"
    location: "1-1.1.3"
  second:
    serial: "99999999999999999.2-1.2"
    power:
      type: meross
      ip: 192.168.1.50
      key: "test-key"
`

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestLoadConfigResolution(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.DefaultDevice != "bench" {
		t.Errorf("DefaultDevice = %q, want bench", cfg.DefaultDevice)
	}

	// Resolution by name: location is preferred over serial when present.
	sel := resolveSelection("bench", cfg)
	if sel.selector != "1-1.1.3" || sel.deviceName != "bench" || !sel.named {
		t.Errorf("resolveSelection(bench) = %+v", sel)
	}

	// A device with no location falls back to its serial.
	sel = resolveSelection("second", cfg)
	if sel.selector != "99999999999999999.2-1.2" || sel.deviceName != "second" {
		t.Errorf("resolveSelection(second) = %+v", sel)
	}
	if sel.powerCfg["type"] != "meross" || sel.powerCfg["ip"] != "192.168.1.50" {
		t.Errorf("resolveSelection(second).powerCfg = %+v", sel.powerCfg)
	}

	// default_device is honored when -s is empty.
	sel = resolveSelection("", cfg)
	if sel.deviceName != "bench" {
		t.Errorf("resolveSelection(\"\") = %+v, want default device bench", sel)
	}

	// -s overrides default_device.
	sel = resolveSelection("second", cfg)
	if sel.deviceName != "second" {
		t.Errorf("resolveSelection(second) did not override default_device: %+v", sel)
	}

	// Absent min_off_seconds defaults to 8.
	if cfg.MinOffSeconds != 8 {
		t.Errorf("MinOffSeconds = %d, want default 8", cfg.MinOffSeconds)
	}
	if cfg.MinOffDuration() != 8*time.Second {
		t.Errorf("MinOffDuration() = %v, want 8s", cfg.MinOffDuration())
	}
}

func TestLoadConfigMinOffSecondsExplicit(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML+"min_off_seconds: 15\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MinOffSeconds != 15 {
		t.Errorf("MinOffSeconds = %d, want 15", cfg.MinOffSeconds)
	}
}

func TestLoadConfigMissingFileIsZeroConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig on missing file: %v", err)
	}
	if cfg.DefaultDevice != "" || len(cfg.Devices) != 0 || cfg.MinOffSeconds != 8 {
		t.Errorf("expected zero-config defaults, got %+v", cfg)
	}
}

func TestLoadConfigNoPathIsZeroConfig(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\"): %v", err)
	}
	if cfg.DefaultDevice != "" || len(cfg.Devices) != 0 {
		t.Errorf("expected zero-config defaults, got %+v", cfg)
	}
}

func TestLoadConfigUnknownPowerTypeIsCaughtByRegistry(t *testing.T) {
	path := writeTestConfig(t, `
devices:
  bogus:
    serial: "123"
    power:
      type: not-a-real-plugin
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	_, err = buildPowerFunc(cfg.Devices["bogus"].Power)
	if err == nil {
		t.Fatal("expected an error for an unregistered power type")
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	path := writeTestConfig(t, testConfigYAML)
	t.Setenv("SDWIRE_DEFAULT_DEVICE", "second")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DefaultDevice != "second" {
		t.Errorf("DefaultDevice = %q, want env override \"second\"", cfg.DefaultDevice)
	}
}

func TestResolveConfigPath(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("SDWIRE_CONFIG", "/env/path.yaml")
		if got := ResolveConfigPath("/flag/path.yaml"); got != "/flag/path.yaml" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("env used when no flag", func(t *testing.T) {
		t.Setenv("SDWIRE_CONFIG", "/env/path.yaml")
		if got := ResolveConfigPath(""); got != "/env/path.yaml" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("default under home when neither set", func(t *testing.T) {
		t.Setenv("SDWIRE_CONFIG", "")
		home := t.TempDir()
		t.Setenv("HOME", home)
		want := filepath.Join(home, ".config", "sdwire", "config.yaml")
		if got := ResolveConfigPath(""); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
