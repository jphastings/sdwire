package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jphastings/sdwire"
	"github.com/jphastings/sdwire/internal/blockdev"
)

// TestFormatListGolden pins the `list` output to the exact shape captured
// from the real Python sdwire CLI v0.3.1:
//
//	Serial                        Product Info		Block Dev
//	20120501030900000.1.1.3       [0bda::0316]		None
func TestFormatListGolden(t *testing.T) {
	wantHeader := "Serial" + strings.Repeat(" ", 30-len("Serial")) + "Product Info" + "\t\t" + "Block Dev" + "\n"
	if got := formatListHeader(); got != wantHeader {
		t.Errorf("formatListHeader() = %q, want %q", got, wantHeader)
	}

	identity := "20120501030900000.1.1.3"
	wantRow := identity + strings.Repeat(" ", 30-len(identity)) + "[0bda::0316]" + "\t\t" + "None" + "\n"
	if got := formatListRow(identity, 0x0bda, 0x0316, ""); got != wantRow {
		t.Errorf("formatListRow(...) = %q, want %q", got, wantRow)
	}
}

func TestFormatListRowResolvedBlockDev(t *testing.T) {
	got := formatListRow("id", 0x04e8, 0x6001, "/dev/disk4")
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

func TestListRowForAndJSON(t *testing.T) {
	orig := blockdevFind
	blockdevFind = func(ref blockdev.Ref) (string, error) { return "/dev/disk4", nil }
	t.Cleanup(func() { blockdevFind = orig })

	info := sdwire.DeviceInfo{
		Serial:     "20120501030900000",
		Product:    "USB2.0-CRW",
		Generation: sdwire.GenerationSDWire3,
		Bus:        1,
		PortPath:   []int{1, 1, 3},
	}

	row := listRowFor(info, resolveBlockDev(info))
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

	var buf bytes.Buffer
	if err := writeListJSON(&buf, []*sdwire.DeviceInfo{&info}); err != nil {
		t.Fatalf("writeListJSON: %v", err)
	}
	var decoded []listRow
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding JSON output: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Serial != info.Serial {
		t.Errorf("decoded = %+v", decoded)
	}
}
