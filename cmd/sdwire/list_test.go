package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jphastings/sdwire"
	"github.com/jphastings/sdwire/internal/blockdev"
)

// TestFormatListGolden pins the `list` output to the shape captured from the
// real Python sdwire CLI v0.3.1:
//
//	Serial                        Product Info		Block Dev
//	20120501030900000.1.1.3       [0bda::0316]		None
//
// The state column is appended after it, so those three columns still parse
// by position for anything written against the Python tool.
func TestFormatListGolden(t *testing.T) {
	pythonHeader := "Serial" + strings.Repeat(" ", 30-len("Serial")) + "Product Info" + "\t\t" + "Block Dev"
	if got := formatListHeader(); !strings.HasPrefix(got, pythonHeader+"\t\t") {
		t.Errorf("formatListHeader() = %q, want it to start with the Python CLI's header %q", got, pythonHeader)
	}
	if got := formatListHeader(); !strings.HasSuffix(got, "State\n") {
		t.Errorf("formatListHeader() = %q, want a trailing State column", got)
	}

	identity := "20120501030900000.1.1.3"
	pythonRow := identity + strings.Repeat(" ", 30-len(identity)) + "[0bda::0316]" + "\t\t" + "None"
	got := formatListRow(identity, 0x0bda, 0x0316, "", "Target")
	if !strings.HasPrefix(got, pythonRow+"\t\t") {
		t.Errorf("formatListRow(...) = %q, want it to start with the Python CLI's row %q", got, pythonRow)
	}
	if !strings.HasSuffix(got, "Target\n") {
		t.Errorf("formatListRow(...) = %q, want the state appended", got)
	}
}

func TestFormatListRowResolvedBlockDev(t *testing.T) {
	got := formatListRow("id", 0x04e8, 0x6001, "/dev/disk4", "Host")
	if !strings.Contains(got, "/dev/disk4") {
		t.Errorf("expected resolved block dev path in %q", got)
	}
	if strings.Contains(got, "None") {
		t.Errorf("did not expect \"None\" when block dev resolved: %q", got)
	}
}

func TestVidPidFor(t *testing.T) {
	v, p := vidPidFor(sdwire.DeviceInfo{Generation: sdwire.GenerationSDWire3})
	if v != uint16(sdwire.SDWire3VID) || p != uint16(sdwire.SDWire3PID) {
		t.Errorf("SDWire3: got %04x:%04x", v, p)
	}
	v, p = vidPidFor(sdwire.DeviceInfo{Generation: sdwire.GenerationSDWireC})
	if v != uint16(sdwire.SDWireCVID) || p != uint16(sdwire.SDWireCPID) {
		t.Errorf("SDWireC: got %04x:%04x", v, p)
	}
}

func sdwire3At(path ...int) sdwire.DeviceInfo {
	return sdwire.DeviceInfo{
		Serial:     "20120501030900000",
		Product:    "USB3.0-CRW",
		Generation: sdwire.GenerationSDWire3,
		Bus:        1,
		PortPath:   path,
	}
}

func TestListRowForAndJSON(t *testing.T) {
	orig := blockdevFind
	blockdevFind = func(ref blockdev.Ref) (string, error) { return "/dev/disk4", nil }
	t.Cleanup(func() { blockdevFind = orig })

	info := sdwire3At(1, 1, 3)
	row := listRowFor(sdwire.DeviceState{Info: info, Mode: sdwire.ModeHost, Attached: true})

	if row.Identity != "20120501030900000.1.1.3" {
		t.Errorf("Identity = %q", row.Identity)
	}
	if row.Location != "1-1.1.3" {
		t.Errorf("Location = %q", row.Location)
	}
	if row.Generation != "SDWire3" {
		t.Errorf("Generation = %q", row.Generation)
	}
	if row.BlockDev != "/dev/disk4" {
		t.Errorf("BlockDev = %q", row.BlockDev)
	}
	if row.State != "Host" || !row.Attached {
		t.Errorf("State = %q, Attached = %v", row.State, row.Attached)
	}

	var buf bytes.Buffer
	states := []sdwire.DeviceState{{Info: info, Mode: sdwire.ModeHost, Attached: true}}
	if err := writeListJSON(&buf, states); err != nil {
		t.Fatalf("writeListJSON: %v", err)
	}
	var decoded []listRow
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding JSON output: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Serial != info.Serial || decoded[0].State != "Host" {
		t.Errorf("decoded = %+v", decoded)
	}
}

// A device known only from the hub-port cache is not on the USB bus, so
// there is no block device to look for — and reporting it at all is the
// point: an empty table is indistinguishable from "no SDWire is plugged in".
func TestListReportsUnattachedCachedDevices(t *testing.T) {
	orig := blockdevFind
	blockdevFind = func(ref blockdev.Ref) (string, error) {
		t.Error("block device lookup attempted for a device that isn't attached")
		return "", nil
	}
	t.Cleanup(func() { blockdevFind = orig })

	var buf bytes.Buffer
	writeListTable(&buf, []sdwire.DeviceState{
		{Info: sdwire3At(1, 1, 3), Mode: sdwire.ModeTarget},
		{Info: sdwire3At(1, 1, 4), Mode: sdwire.ModeUnknown},
	})

	out := buf.String()
	for _, want := range []string{"20120501030900000.1.1.3", "Target", "20120501030900000.1.1.4", "Unknown", "None"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in list output:\n%s", want, out)
		}
	}
}
