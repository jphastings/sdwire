package main

import (
	"fmt"

	"github.com/jphastings/sdwire"
	"github.com/spf13/cobra"
)

// warningOption returns an sdwire.Option that prints SDK warnings (e.g. a
// ganged-power-switching hub) to cmd's stderr. Warnings print unconditionally
// — --debug only adds extra verbosity on top, it doesn't gate these.
func warningOption(cmd *cobra.Command) sdwire.Option {
	return sdwire.WithWarningHandler(func(msg string) {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", msg)
	})
}

// debugf prints a diagnostic line to cmd's stderr when --debug is set.
func debugf(cmd *cobra.Command, flags *globalFlags, format string, args ...any) {
	if flags == nil || !flags.debug {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "debug: "+format+"\n", args...)
}
