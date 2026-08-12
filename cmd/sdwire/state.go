package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// stateJSON is the state command's --json representation.
type stateJSON struct {
	Identity string `json:"identity"`
	State    string `json:"state"`
}

// formatStateHeader and formatStateRow implement the Python sdwire CLI's
// exact `state` output shape: the identity column left-justified to 30
// chars, one literal tab, then the state word (Host/Target/Unknown).
func formatStateHeader() string {
	return fmt.Sprintf("%-30s\t%s\n", "Serial", "State")
}

func formatStateRow(identity, state string) string {
	return fmt.Sprintf("%-30s\t%s\n", identity, state)
}

func writeStateJSON(w io.Writer, identity, state string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(stateJSON{Identity: identity, State: state})
}

// writeStateOutput prints identity/state in the requested format, shared by
// both the live-device and cached-hub-port-state paths.
func writeStateOutput(cmd *cobra.Command, jsonOut bool, identity, state string) error {
	if jsonOut {
		return writeStateJSON(cmd.OutOrStdout(), identity, state)
	}
	fmt.Fprint(cmd.OutOrStdout(), formatStateHeader())
	fmt.Fprint(cmd.OutOrStdout(), formatStateRow(identity, state))
	return nil
}

// cachedPortStateFor reads hub-port state for sel from the on-disk cache,
// retrying with a configured device's serial when its location — which may
// name a socket the device has since been moved out of — matches no cache
// entry, or several stale ones left by a previous occupant of that port.
func cachedPortStateFor(sel selection, opts ...sdwire.Option) (sdwire.SwitchMode, string, error) {
	mode, identity, err := sdwireCachedPortState(sel.cacheSelector(), opts...)
	if err != nil && sel.fallback != "" {
		if fallbackMode, fallbackIdentity, fallbackErr := sdwireCachedPortState(sel.fallback, opts...); fallbackErr == nil {
			return fallbackMode, fallbackIdentity, nil
		}
	}
	return mode, identity, err
}

func newStateCmd(flags *globalFlags) *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Print which side the selected SDWire's SD card is connected to",
		Long: "Print which side the selected SDWire's SD card is currently connected to: " +
			"Host, Target, or Unknown. When the device is attached and powered on, this is an " +
			"honest live readback via the device. An SDWire3 currently in Target mode is " +
			"powered off and unreachable over USB; in that case, state is inferred from the " +
			"on-disk hub-port cache and a hub port-status read, without powering anything back " +
			"on (see the README's SDWire3 notes).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			res, err := openSelected(cmd, flags.serial, cfg, false)
			if err != nil {
				if !errors.Is(err, sdwire.ErrNoDeviceFound) {
					return err
				}

				sel := resolveSelection(flags.serial, cfg)
				mode, identity, cacheErr := cachedPortStateFor(sel, warningOption(cmd))
				if cacheErr != nil {
					return opErrf("reading device state%s: %w", sel.originSuffix(), cacheErr)
				}
				debugf(cmd, flags, "resolved device %s via cached hub-port state (no live device)", identity)
				return writeStateOutput(cmd, jsonOut, identity, mode.String())
			}
			defer res.sw.Close()

			mode, err := res.sw.Mode()
			if err != nil {
				return opErrf("reading device state: %w", err)
			}

			identity := res.sw.Info().Identity()
			debugf(cmd, flags, "resolved device %s via selector %q", identity, res.selector)

			return writeStateOutput(cmd, jsonOut, identity, mode.String())
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	return cmd
}
