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
	BlockDev   string `json:"block_dev"`
}

func listRowFor(info sdwire.DeviceInfo, blockDev string) listRow {
	return listRow{
		Serial:     info.Serial,
		Identity:   info.Identity(),
		Location:   info.Location(),
		Product:    info.Product,
		Generation: info.Generation.String(),
		BlockDev:   blockDev,
	}
}

// formatListHeader and formatListRow implement the Python sdwire CLI's
// exact `list` output shape: the identity column left-justified to 30
// chars, then a "[vendor::product]" hex product column, then two literal
// tabs, then the block device column ("None" when unresolved).
func formatListHeader() string {
	return fmt.Sprintf("%-30s%s\t\t%s\n", "Serial", "Product Info", "Block Dev")
}

func formatListRow(identity string, vendor, product uint16, blockDev string) string {
	productCol := fmt.Sprintf("[%04x::%04x]", vendor, product)
	bd := blockDev
	if bd == "" {
		bd = "None"
	}
	return fmt.Sprintf("%-30s%s\t\t%s\n", identity, productCol, bd)
}

func vidPidFor(info sdwire.DeviceInfo) (uint16, uint16) {
	if info.Generation == sdwire.GenerationSDWire3 {
		return uint16(sdwire.SDWire3VID), uint16(sdwire.SDWire3PID)
	}
	return uint16(sdwire.SDWireCVID), uint16(sdwire.SDWireCPID)
}

func writeListTable(w io.Writer, infos []*sdwire.DeviceInfo) {
	fmt.Fprint(w, formatListHeader())
	for _, info := range infos {
		vendor, product := vidPidFor(*info)
		fmt.Fprint(w, formatListRow(info.Identity(), vendor, product, resolveBlockDev(*info)))
	}
}

func writeListJSON(w io.Writer, infos []*sdwire.DeviceInfo) error {
	rows := make([]listRow, len(infos))
	for i, info := range infos {
		rows[i] = listRowFor(*info, resolveBlockDev(*info))
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
		Long: "List attached SDWire devices, their USB product info, and the " +
			"reader's block device path (when resolvable — a device switched " +
			"to target mode has no block device to find).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			infos, err := sdwireListDevices()
			if err != nil {
				return opErrf("listing devices: %w", err)
			}
			debugf(cmd, flags, "found %d attached SDWire device(s)", len(infos))

			if jsonOut {
				return writeListJSON(cmd.OutOrStdout(), infos)
			}
			writeListTable(cmd.OutOrStdout(), infos)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	return cmd
}
