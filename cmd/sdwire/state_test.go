package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jphastings/sdwire"
)

// TestFormatStateGolden pins the `state` output to the exact shape captured
// from the real Python sdwire CLI v0.3.1:
//
//	Serial                        	State
//	20120501030900000.1.1.3       	Host
func TestFormatStateGolden(t *testing.T) {
	wantHeader := "Serial" + strings.Repeat(" ", 30-len("Serial")) + "\t" + "State" + "\n"
	if got := formatStateHeader(); got != wantHeader {
		t.Errorf("formatStateHeader() = %q, want %q", got, wantHeader)
	}

	identity := "20120501030900000.1.1.3"
	wantRow := identity + strings.Repeat(" ", 30-len(identity)) + "\t" + "Host" + "\n"
	if got := formatStateRow(identity, "Host"); got != wantRow {
		t.Errorf("formatStateRow(...) = %q, want %q", got, wantRow)
	}
}

func TestFormatStateRowOtherStates(t *testing.T) {
	for _, state := range []string{"Target", "Unknown"} {
		got := formatStateRow("id", state)
		if !strings.HasSuffix(got, "\t"+state+"\n") {
			t.Errorf("formatStateRow(id, %q) = %q", state, got)
		}
	}
}

func TestStateCommandFallsBackToCachedPortStateWhenNoLiveDevice(t *testing.T) {
	origList, origCached := sdwireListDevices, sdwireCachedPortState
	t.Cleanup(func() { sdwireListDevices, sdwireCachedPortState = origList, origCached })

	sdwireListDevices = func() ([]*sdwire.DeviceInfo, error) { return nil, nil }
	sdwireCachedPortState = func(selector string, opts ...sdwire.Option) (sdwire.SwitchMode, string, error) {
		return sdwire.ModeTarget, "20120501030900000.1.1.3", nil
	}

	flags := &globalFlags{config: filepath.Join(t.TempDir(), "does-not-exist.yaml")}
	cmd := newStateCmd(flags)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := formatStateHeader() + formatStateRow("20120501030900000.1.1.3", "Target")
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

func TestCachedPortStateForFallsBackToSerialOnLookupFailure(t *testing.T) {
	orig := sdwireCachedPortState
	t.Cleanup(func() { sdwireCachedPortState = orig })

	var tried []string
	sdwireCachedPortState = func(selector string, opts ...sdwire.Option) (sdwire.SwitchMode, string, error) {
		tried = append(tried, selector)
		if selector == "1-1.1.3" {
			return sdwire.ModeUnknown, "", errors.New("no cache entry at that location")
		}
		return sdwire.ModeTarget, "20120501030900000.1.1.3", nil
	}

	sel := selection{selector: "1-1.1.3", fallback: "20120501030900000", named: true}
	mode, identity, err := cachedPortStateFor(sel)
	if err != nil {
		t.Fatalf("cachedPortStateFor: %v", err)
	}
	if mode != sdwire.ModeTarget || identity != "20120501030900000.1.1.3" {
		t.Errorf("got mode=%v identity=%q", mode, identity)
	}
	if len(tried) != 2 || tried[0] != "1-1.1.3" || tried[1] != "20120501030900000" {
		t.Errorf("selectors tried = %v, want [1-1.1.3 20120501030900000] (location first, then serial fallback)", tried)
	}
}

func TestCachedPortStateForReturnsFirstResultOnSuccess(t *testing.T) {
	orig := sdwireCachedPortState
	t.Cleanup(func() { sdwireCachedPortState = orig })

	calls := 0
	sdwireCachedPortState = func(selector string, opts ...sdwire.Option) (sdwire.SwitchMode, string, error) {
		calls++
		return sdwire.ModeHost, "20120501030900000.1.1.3", nil
	}

	sel := selection{selector: "1-1.1.3", fallback: "20120501030900000", named: true}
	mode, identity, err := cachedPortStateFor(sel)
	if err != nil {
		t.Fatalf("cachedPortStateFor: %v", err)
	}
	if mode != sdwire.ModeHost || identity != "20120501030900000.1.1.3" {
		t.Errorf("got mode=%v identity=%q", mode, identity)
	}
	if calls != 1 {
		t.Errorf("sdwireCachedPortState called %d times, want 1 (no fallback needed on success)", calls)
	}
}

func TestWriteStateJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writeStateJSON(&buf, "20120501030900000.1.1.3", "Host"); err != nil {
		t.Fatalf("writeStateJSON: %v", err)
	}
	var decoded stateJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decoding JSON output: %v", err)
	}
	if decoded.Identity != "20120501030900000.1.1.3" || decoded.State != "Host" {
		t.Errorf("decoded = %+v", decoded)
	}
}
