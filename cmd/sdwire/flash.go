package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// formatFlashProgress renders a single updating progress line: "flashed X /
// Y MiB (Z%)". It's written with a leading \r (no trailing newline) so
// repeated calls overwrite the same terminal line.
func formatFlashProgress(written, total int64) string {
	const mib = 1024 * 1024
	pct := 0
	if total > 0 {
		pct = int(written * 100 / total)
	}
	return fmt.Sprintf("\rflashed %d / %d MiB (%d%%)", written/mib, total/mib, pct)
}

func newFlashCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flash <image>",
		Short: "Write an image to the selected SDWire's SD card and boot the target from it",
		Long: "Write an image to the selected SDWire's SD card and boot the target from it: " +
			"powers the target off (if a power plugin is configured), switches to host mode, " +
			"raw-writes the image, switches back to target mode, and powers the target back " +
			"on.\n\n" +
			"Raw disk writes need elevated privileges: run this with sudo on macOS/Linux, " +
			"or as Administrator on Windows.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imagePath := args[0]

			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			res, err := openSelected(flags.serial, cfg, true, warningOption(cmd))
			if err != nil {
				return err
			}
			defer res.sw.Close()

			debugf(cmd, flags, "resolved device %s via selector %q", res.sw.Info().Identity(), res.selector)

			if res.powerCfg != nil {
				powerFunc, err := buildPowerFunc(res.powerCfg)
				if err != nil {
					return opErrf("building power control: %w", err)
				}
				res.sw.SetTargetPower(powerFunc)
			} else {
				debugf(cmd, flags, "no power plugin configured; target will not be power-cycled after flashing")
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			stderr := cmd.ErrOrStderr()
			err = res.sw.FlashAndBoot(ctx, imagePath,
				sdwire.WithFlashMinDarkTime(cfg.MinOffDuration()),
				sdwire.WithFlashProgress(func(written, total int64) {
					fmt.Fprint(stderr, formatFlashProgress(written, total))
				}),
			)
			fmt.Fprintln(stderr)
			if err != nil {
				return opErrf("flashing %s: %w", imagePath, err)
			}
			return nil
		},
	}
	return cmd
}
