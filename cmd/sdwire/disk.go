package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type diskJSON struct {
	BlockDev string `json:"block_dev"`
}

func writeDiskJSON(w io.Writer, blockDev string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(diskJSON{BlockDev: blockDev})
}

func newDiskCmd(flags *globalFlags) *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "disk",
		Short: "Print the selected SDWire reader's block device path",
		Long: "Print the selected SDWire reader's block device path, and nothing else — " +
			"intended for use in scripts, e.g. `dd if=image.img of=$(sdwire disk) bs=4M`. " +
			"Fails with a non-zero exit status if the reader's block device can't currently " +
			"be found (for example, because the device is switched to target mode).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			res, err := openSelected(cmd, flags.serial, cfg, false)
			if err != nil {
				return err
			}
			defer res.sw.Close()

			info := res.sw.Info()
			debugf(cmd, flags, "resolved device %s via selector %q", info.Identity(), res.selector)

			path := resolveBlockDev(info)
			if path == "" {
				return opErrf("no block device found for %s (it may be switched to target mode)", info.Identity())
			}

			if jsonOut {
				return writeDiskJSON(cmd.OutOrStdout(), path)
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output machine-readable JSON")
	return cmd
}
