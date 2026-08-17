package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// listRow is one device's list-command JSON representation.
type listRow struct {
	Serial     string `json:"serial"`
	Identity   string `json:"identity"`
	Location   string `json:"location"`
	Product    string `json:"product"`
	Generation string `json:"generation"`
	State      string `json:"state"`
	Attached   bool   `json:"attached"`
	BlockDev   string `json:"block_dev"`
}

func listRowFor(state sdwire.DeviceState) listRow {
	return listRow{
		Serial:     state.Info.Serial,
		Identity:   state.Info.Identity(),
		Location:   state.Info.Location(),
		Product:    state.Info.Product,
		Generation: state.Info.Generation.String(),
		State:      state.Mode.String(),
		Attached:   state.Attached,
		BlockDev:   blockDevFor(state),
	}
}

// formatListHeader and formatListRow keep the Python sdwire CLI's exact
// `list` output shape — the identity column left-justified to 30 chars,
// then a "[vendor::product]" hex product column, then two literal tabs,
// then the block device column ("None" when unresolved) — and append the
// state column after it. Anything reading the first three columns by
// position still parses; anything needing more should use --json.
func formatListHeader() string {
	return fmt.Sprintf("%-30s%s\t\t%s\t\t%s\n", "Serial", "Product Info", "Block Dev", "State")
}

func formatListRow(identity string, vendor, product uint16, blockDev, state string) string {
	productCol := fmt.Sprintf("[%04x::%04x]", vendor, product)
	bd := blockDev
	if bd == "" {
		bd = "None"
	}
	return fmt.Sprintf("%-30s%s\t\t%s\t\t%s\n", identity, productCol, bd, state)
}

func vidPidFor(info sdwire.DeviceInfo) (uint16, uint16) {
	if info.Generation == sdwire.GenerationSDWire3 {
		return uint16(sdwire.SDWire3VID), uint16(sdwire.SDWire3PID)
	}
	return uint16(sdwire.SDWireCVID), uint16(sdwire.SDWireCPID)
}

// blockDevFor resolves a device's reader block device, skipping the lookup
// for a device that isn't attached: there is no enumerated USB device for
// it to be found under.
func blockDevFor(state sdwire.DeviceState) string {
	if !state.Attached {
		return ""
	}
	return resolveBlockDev(state.Info)
}

func writeListTable(w io.Writer, states []sdwire.DeviceState) {
	fmt.Fprint(w, formatListHeader())
	for _, state := range states {
		vendor, product := vidPidFor(state.Info)
		fmt.Fprint(w, formatListRow(state.Info.Identity(), vendor, product, blockDevFor(state), state.Mode.String()))
	}
}

func writeListJSON(w io.Writer, states []sdwire.DeviceState) error {
	rows := make([]listRow, len(states))
	for i, state := range states {
		rows[i] = listRowFor(state)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func newListCmd(flags *globalFlags) *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List attached SDWire devices",
		Long: "List SDWire devices, their USB product info, the reader's block device " +
			"path, and which side each card is switched to.\n\n" +
			"Devices remembered in the hub-port cache are listed too, even when they " +
			"aren't currently on the USB bus — an SDWire3 in Target mode is powered " +
			"off, so enumeration alone cannot see it. For those, State is read from " +
			"the hub port without powering anything on: Target means the port is off " +
			"(the card is with the target), and Unknown means the port is powered but " +
			"nothing is enumerated on it — an empty socket, or a reader that has " +
			"crashed and been dropped from the bus. Recover the latter with " +
			"`sdwire revive`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			states, err := sdwireListDeviceStates(warningOption(cmd))
			if err != nil {
				return opErrf("listing devices: %w", err)
			}
			debugf(cmd, flags, "found %d SDWire device(s)", len(states))

			if jsonOut {
				return writeListJSON(cmd.OutOrStdout(), states)
			}
			writeListTable(cmd.OutOrStdout(), states)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	return cmd
}
