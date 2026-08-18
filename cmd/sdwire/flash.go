package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

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

// formatWriteRate renders a chunk's throughput, the number to watch when
// deciding whether a reader is struggling: a healthy one holds steady,
// while one whose internal buffer is backing up slows chunk by chunk
// before it stalls outright.
func formatWriteRate(size int, took time.Duration) string {
	if took <= 0 {
		return "instant"
	}
	return fmt.Sprintf("%.1f MiB/s", float64(size)/(1024*1024)/took.Seconds())
}

func newFlashCmd(flags *globalFlags) *cobra.Command {
	var requirePower bool

	cmd := &cobra.Command{
		Use:   "flash <image>",
		Short: "Write an image to the selected SDWire's SD card and boot the target from it",
		Long: "Write an image to the selected SDWire's SD card and boot the target from it: " +
			"powers the target off (if a power plugin is configured), switches to host mode, " +
			"raw-writes the image, switches back to target mode, and powers the target back " +
			"on. Without a power plugin configured, the flash still happens, but the target is " +
			"left running whatever it was before — it is never power-cycled onto the new " +
			"image. Pass --require-power to turn that into an upfront error instead.\n\n" +
			"Raw disk writes need elevated privileges: run this with sudo on macOS/Linux, " +
			"or as Administrator on Windows.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imagePath := args[0]

			cfg, err := LoadConfig(ResolveConfigPath(flags.config))
			if err != nil {
				return opErrf("loading config: %w", err)
			}

			sel := resolveSelection(flags.serial, cfg)
			if requirePower && sel.powerCfg == nil {
				return opErrf("--require-power: %s", explainMissingPowerConfig(sel.deviceName, cfg.Devices[sel.deviceName].Location))
			}

			res, err := openSelected(cmd, flags.serial, cfg, true)
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
			}

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			stderr := cmd.ErrOrStderr()
			flashOpts := []sdwire.FlashOption{
				sdwire.WithFlashMinDarkTime(cfg.MinOffDuration()),
				sdwire.WithFlashProgress(func(written, total int64) {
					fmt.Fprint(stderr, formatFlashProgress(written, total))
				}),
			}
			if flags.debug {
				flashOpts = append(flashOpts, sdwire.WithWriteTiming(func(offset int64, size int, took time.Duration) {
					// The progress line is redrawn with \r and has no
					// newline of its own; finish it before writing over it.
					fmt.Fprintln(stderr)
					debugf(cmd, flags, "wrote %d KiB at offset %d MiB in %s (%s)",
						size/1024, offset/(1024*1024), took.Round(time.Millisecond), formatWriteRate(size, took))
				}))
			}

			err = res.sw.FlashAndBoot(ctx, imagePath, flashOpts...)
			fmt.Fprintln(stderr)
			if err != nil {
				return opErrf("flashing %s: %w", imagePath, err)
			}

			// Printed last, not before the flash: it's the last thing on
			// screen, right where the operator is looking when they wonder
			// why the target didn't come up.
			if res.powerCfg == nil {
				warnf(cmd, "%s", explainSkippedPowerCycle(res.deviceName, res.sw.Info().Location()))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&requirePower, "require-power", false,
		"fail before flashing if the selected device has no power plugin configured, instead of warning afterwards that the target wasn't power-cycled")
	return cmd
}
