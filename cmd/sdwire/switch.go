package main

import (
	"errors"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// errSwitchOffUnsupported explains why "off" isn't a switch mode: for
// SDWireC there's no third state, and for SDWire3 "powering the port off"
// is precisely how ModeTarget is implemented (see sdwire3Controller's doc
// comment) — it does not mean disconnected-from-both.
var errSwitchOffUnsupported = errors.New(`"off" is not supported: SDWireC has no third connection state, ` +
	`and for SDWire3 powering the reader off is how target mode is switched to (see sdwire.ModeTarget) — ` +
	`not a disconnected-from-both state. Use "target"/"dut" or "host"/"ts" instead`)

// resolveSwitchMode maps a switch subcommand argument to a SwitchMode. It
// returns errSwitchOffUnsupported for "off"; any other value is rejected
// earlier by cobra's ValidArgs check.
func resolveSwitchMode(arg string) (sdwire.SwitchMode, error) {
	switch arg {
	case "dut", "target":
		return sdwire.ModeTarget, nil
	case "host", "ts":
		return sdwire.ModeHost, nil
	case "off":
		return sdwire.ModeUnknown, errSwitchOffUnsupported
	default:
		return sdwire.ModeUnknown, errors.New("unknown switch mode " + arg)
	}
}

func newSwitchCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "switch {dut|target|host|ts|off}",
		Short:     "Switch the selected SDWire's SD card between target and host",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"dut", "target", "host", "ts", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveSwitchMode(args[0])
			if err != nil {
				return opErrf("%w", err)
			}

			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			if mode == sdwire.ModeTarget {
				return switchToTarget(cmd, flags, cfg)
			}

			res, err := openSelected(flags.serial, cfg, true, warningOption(cmd))
			if err != nil {
				return err
			}
			return applyMode(cmd, flags, res, mode)
		},
	}
	return cmd
}

// applyMode switches res.sw to mode and closes it, so every switch path —
// a live device or one just revived — releases its device handle the same
// way.
func applyMode(cmd *cobra.Command, flags *globalFlags, res *openResult, mode sdwire.SwitchMode) error {
	defer res.sw.Close()
	debugf(cmd, flags, "resolved device %s via selector %q", res.sw.Info().Identity(), res.selector)
	if err := res.sw.SetMode(mode); err != nil {
		return opErrf("switching mode: %w", err)
	}
	return nil
}

// switchToTarget implements `switch dut`/`switch target`. A device that
// isn't currently live (openSelected's WithoutRevive-guarded attempt fails
// not-found) may simply already be in target mode — an SDWire3 sitting
// powered off, which is exactly what target mode looks like. Reviving it
// just to confirm that, and switch it right back to where it already was,
// would be pure churn, so the hub-port cache is consulted first; only a
// device that isn't already in target mode gets revived.
func switchToTarget(cmd *cobra.Command, flags *globalFlags, cfg *Config) error {
	res, err := openSelected(flags.serial, cfg, false, warningOption(cmd))
	if err == nil {
		return applyMode(cmd, flags, res, sdwire.ModeTarget)
	}
	if !errors.Is(err, sdwire.ErrNoDeviceFound) {
		return err
	}

	if cachedMode, identity, cacheErr := sdwireCachedPortState(selectorForCache(flags.serial, cfg), warningOption(cmd)); cacheErr == nil && cachedMode == sdwire.ModeTarget {
		debugf(cmd, flags, "device %s already in target mode (cached hub-port state); nothing to do", identity)
		return nil
	}

	res, err = openSelected(flags.serial, cfg, true, warningOption(cmd))
	if err != nil {
		return err
	}
	return applyMode(cmd, flags, res, sdwire.ModeTarget)
}
