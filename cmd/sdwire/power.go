package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// sleepFn is a seam over time.Sleep so power cycle's dark-time wait can be
// verified in tests without actually blocking a test run.
var sleepFn = time.Sleep

// explainMissingPowerConfig describes what to add to the config file so
// deviceName (or, if it wasn't resolved from a configured device name, a
// placeholder key) gets power control. location, if non-empty, is filled
// in as a ready-to-use selector for the device actually connected to.
func explainMissingPowerConfig(deviceName, location string) string {
	key := deviceName
	if key == "" {
		key = "mydevice"
	}
	loc := location
	if loc == "" {
		loc = "<run `sdwire list` to find this device's location>"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "no power control is configured for this device.\n\n")
	fmt.Fprintf(&b, "Add a \"power\" section to your config file (~/.config/sdwire/config.yaml):\n\n")
	fmt.Fprintf(&b, "devices:\n")
	fmt.Fprintf(&b, "  %s:\n", key)
	fmt.Fprintf(&b, "    location: %q\n", loc)
	fmt.Fprintf(&b, "    power:\n")
	fmt.Fprintf(&b, "      type: meross\n")
	fmt.Fprintf(&b, "      ip: 192.168.1.112\n")
	fmt.Fprintf(&b, "      key: \"<meross account key>\"\n")
	fmt.Fprintf(&b, "      # channel: 0\n\n")
	fmt.Fprintf(&b, "Registered power plugin types: %s\n", strings.Join(registeredPowerTypes(), ", "))
	return b.String()
}

// resolvePowerSelection decides which configured device's power block the
// power command should use, purely from cfg — it never touches hardware,
// since power control is a network operation (e.g. a smart plug) entirely
// independent of the SDWire's USB connection. If -s/--serial or
// default_device names a device, that device's power block (possibly nil)
// is used as-is; otherwise, the config's sole device with a power block is
// used. deviceName and location (config-declared, if any) are returned
// alongside a nil power for the caller to build explainMissingPowerConfig's
// message from.
func resolvePowerSelection(serialFlag string, cfg *Config) (deviceName string, power map[string]any, location string, err error) {
	sel := resolveSelection(serialFlag, cfg)
	if sel.named {
		loc := ""
		if cfg != nil {
			loc = cfg.Devices[sel.deviceName].Location
		}
		return sel.deviceName, sel.powerCfg, loc, nil
	}

	if cfg == nil {
		return "", nil, "", nil
	}

	var withPower []string
	for name, dev := range cfg.Devices {
		if dev.Power != nil {
			withPower = append(withPower, name)
		}
	}
	sort.Strings(withPower)

	switch len(withPower) {
	case 0:
		return "", nil, "", nil
	case 1:
		name := withPower[0]
		return name, cfg.Devices[name].Power, cfg.Devices[name].Location, nil
	default:
		return "", nil, "", fmt.Errorf("multiple configured devices have power control; specify one with -s/--serial: %s", strings.Join(withPower, ", "))
	}
}

func newPowerCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "power {on|off|cycle}",
		Short: "Turn power to the selected SDWire's target board on, off, or cycle it",
		Long: "Turn power to the selected SDWire's target board on, off, or cycle it, via a " +
			"configured power plugin (e.g. a network smart plug). This never touches the " +
			"SDWire's USB connection — it's safe, and the normal way, to power-cycle a target " +
			"whose SD card is already switched to it.",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"on", "off", "cycle"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			deviceName, powerCfg, location, err := resolvePowerSelection(flags.serial, cfg)
			if err != nil {
				return opErrf("%w", err)
			}
			if powerCfg == nil {
				return opErrf("%s", explainMissingPowerConfig(deviceName, location))
			}

			powerFunc, err := buildPowerFunc(powerCfg)
			if err != nil {
				return opErrf("building power control: %w", err)
			}
			debugf(cmd, flags, "controlling power for device %q", deviceName)

			switch args[0] {
			case "on":
				if err := powerFunc(true); err != nil {
					return opErrf("powering on: %w", err)
				}
			case "off":
				if err := powerFunc(false); err != nil {
					return opErrf("powering off: %w", err)
				}
			case "cycle":
				if err := powerFunc(false); err != nil {
					return opErrf("powering off: %w", err)
				}
				sleepFn(cfg.MinOffDuration())
				if err := powerFunc(true); err != nil {
					return opErrf("powering on: %w", err)
				}
			}
			return nil
		},
	}
	return cmd
}
