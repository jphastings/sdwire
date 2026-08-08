package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// defaultMinOffSeconds is PowerCycle/FlashAndBoot's minimum dark time when
// min_off_seconds is absent from the config.
const defaultMinOffSeconds = 8

// Config is the parsed contents of ~/.config/sdwire/config.yaml (or
// whatever path/env override selected it).
type Config struct {
	DefaultDevice string
	Devices       map[string]DeviceConfig
	MinOffSeconds int
}

// DeviceConfig is one entry under the config file's "devices" map.
type DeviceConfig struct {
	Serial   string
	Location string
	// Power holds the raw "power:" section for this device (including its
	// "type" key), handed as-is to the matching registry.PowerFactory.
	// nil if the device has no power section configured.
	Power map[string]any
}

// MinOffDuration returns MinOffSeconds as a time.Duration, falling back to
// defaultMinOffSeconds when unset or non-positive.
func (c *Config) MinOffDuration() time.Duration {
	n := defaultMinOffSeconds
	if c != nil && c.MinOffSeconds > 0 {
		n = c.MinOffSeconds
	}
	return time.Duration(n) * time.Second
}

// ResolveConfigPath decides which config file path to use, in order of
// precedence: an explicit --config flag value, the SDWIRE_CONFIG
// environment variable, then the fixed default
// ~/.config/sdwire/config.yaml (the same path on every OS: macOS's
// os.UserConfigDir() would return ~/Library/Application Support, which this
// project deliberately does not use, for consistency with the XDG-style
// path Linux/Windows users of this tool are more likely to expect).
func ResolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("SDWIRE_CONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "sdwire", "config.yaml")
	}
	return filepath.Join(home, ".config", "sdwire", "config.yaml")
}

// LoadConfig reads and parses the config file at path. A missing file is
// not an error: the zero-config path (no file at all) must work, resulting
// in a Config with no default device and no configured devices. Individual
// values are also overridable via SDWIRE_-prefixed environment variables
// (e.g. SDWIRE_DEFAULT_DEVICE), independently of whether a config file
// exists.
func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("SDWIRE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("min_off_seconds", defaultMinOffSeconds)

	if path != "" {
		if _, err := os.Stat(path); err == nil {
			v.SetConfigFile(path)
			if err := v.ReadInConfig(); err != nil {
				return nil, fmt.Errorf("reading config %s: %w", path, err)
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("checking config %s: %w", path, err)
		}
	}

	cfg := &Config{
		DefaultDevice: v.GetString("default_device"),
		MinOffSeconds: v.GetInt("min_off_seconds"),
		Devices:       map[string]DeviceConfig{},
	}
	if err := v.UnmarshalKey("devices", &cfg.Devices); err != nil {
		return nil, fmt.Errorf("parsing devices config: %w", err)
	}
	return cfg, nil
}
