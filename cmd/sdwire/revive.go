package main

import (
	"fmt"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// reviveFor power-cycles sel's hub port, retrying with a configured
// device's serial when its location matches nothing — the same
// location-goes-stale-on-a-replug fallback openSelected and
// cachedPortStateFor apply.
func reviveFor(sel selection, opts ...sdwire.Option) (sdwire.DeviceInfo, error) {
	info, err := sdwireRevive(sel.cacheSelector(), opts...)
	if err != nil && sel.fallback != "" {
		if fallbackInfo, fallbackErr := sdwireRevive(sel.fallback, opts...); fallbackErr == nil {
			return fallbackInfo, nil
		}
	}
	return info, err
}

func newReviveCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revive",
		Short: "Power-cycle an SDWire's hub port to recover a reader that has dropped off the bus",
		Long: "Cut power to the selected SDWire's upstream hub port, hold it off long " +
			"enough for the reader's own supply to drain, then restore it and wait for " +
			"the device to re-enumerate. This is the software equivalent of unplugging " +
			"the device and plugging it back in.\n\n" +
			"Use it when a reader has stopped answering and been dropped from the USB " +
			"bus — `sdwire list` shows its state as Unknown, or nothing at all. A USB " +
			"port reset does not clear that state; only removing power does.\n\n" +
			"Because such a device is not enumerated, it cannot be selected by serial " +
			"alone. -s/--serial accepts a location (e.g. \"1-1.1.3\", as shown in " +
			"`sdwire list`), which is resolved from the live USB topology and works " +
			"even when the hub-port cache is empty, stale, or names the same serial at " +
			"several ports.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			sel := resolveSelection(flags.serial, cfg)
			info, err := reviveFor(sel, warningOption(cmd))
			if err != nil {
				return opErrf("reviving device%s: %w", sel.originSuffix(), err)
			}

			debugf(cmd, flags, "revived device via selector %q", sel.cacheSelector())
			fmt.Fprintf(cmd.OutOrStdout(), "Revived %s at %s\n", info.Identity(), info.Location())
			return nil
		},
	}
	return cmd
}
