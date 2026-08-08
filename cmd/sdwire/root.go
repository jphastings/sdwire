package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is injected at build time via:
//
//	-ldflags "-X main.version=1.2.3"
//
// and defaults to "dev" for `go run`/`go build` without ldflags.
var version = "dev"

// globalFlags holds the values of persistent flags shared by every
// subcommand. A single instance is populated by newRootCmd and threaded
// through to subcommands via closures, rather than package-level vars, so
// tests can construct independent command trees.
type globalFlags struct {
	serial string
	config string
	debug  bool
}

// opError marks an error as "operational" (device/config/IO failure) rather
// than a CLI usage error, so Execute can tell the two apart and choose exit
// code 1 vs 2. Cobra-level errors (bad flags, wrong arg count, unknown
// subcommand) are never wrapped in opError and are never returned by RunE
// functions in this package unless explicitly wrapped.
type opError struct{ err error }

func (e *opError) Error() string { return e.err.Error() }
func (e *opError) Unwrap() error { return e.err }

func opErrf(format string, args ...any) error {
	return &opError{fmt.Errorf(format, args...)}
}

func newRootCmd() *cobra.Command {
	flags := &globalFlags{}

	cmd := &cobra.Command{
		Use:           "sdwire",
		Short:         "Control SDWireC and SDWire3 USB SD-card multiplexers",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.SetVersionTemplate("{{.Name}}, version {{.Version}}\n")

	cmd.PersistentFlags().StringVarP(&flags.serial, "serial", "s", "", "device serial, port-suffixed identity, location, or configured device name")
	cmd.PersistentFlags().StringVar(&flags.config, "config", "", "path to config file (default ~/.config/sdwire/config.yaml, or $SDWIRE_CONFIG)")
	cmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "print SDK warnings and extra diagnostics to stderr")

	cmd.AddCommand(
		newListCmd(flags),
		newStateCmd(flags),
		newSwitchCmd(flags),
		newFlashCmd(flags),
		newPowerCmd(flags),
		newDiskCmd(flags),
	)

	return cmd
}

// Execute runs the sdwire CLI with args (excluding the program name) and
// returns the process exit code: 0 on success, 1 for operational errors
// (device/config/IO failures), 2 for CLI usage errors.
func Execute(args []string) int {
	cmd := newRootCmd()
	cmd.SetArgs(args)

	err := cmd.Execute()
	if err == nil {
		return 0
	}

	fmt.Fprintln(os.Stderr, "Error:", err)

	var oe *opError
	if errors.As(err, &oe) {
		return 1
	}
	return 2
}
