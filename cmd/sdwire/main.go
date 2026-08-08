// Command sdwire is a CLI for controlling SDWireC and SDWire3 USB SD-card
// multiplexers, and a drop-in replacement for the Python sdwire CLI's
// list/state/switch commands.
package main

import "os"

func main() {
	os.Exit(Execute(os.Args[1:]))
}
