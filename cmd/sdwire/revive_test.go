package main

import (
	"errors"
	"testing"

	"github.com/jphastings/sdwire"
)

func TestReviveForFallsBackToSerialWhenLocationIsStale(t *testing.T) {
	orig := sdwireRevive
	t.Cleanup(func() { sdwireRevive = orig })

	var tried []string
	sdwireRevive = func(selector string, opts ...sdwire.Option) (sdwire.DeviceInfo, error) {
		tried = append(tried, selector)
		if selector != "20120501030900000" {
			return sdwire.DeviceInfo{}, errors.New("nothing at that location")
		}
		return sdwire.DeviceInfo{Serial: selector, Bus: 1, PortPath: []int{1, 1, 4}}, nil
	}

	sel := selection{selector: "1-1.1.3", fallback: "20120501030900000", named: true, deviceName: "bench"}
	info, err := reviveFor(sel)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Location() != "1-1.1.4" {
		t.Errorf("revived %s, want the device found by serial at 1-1.1.4", info.Location())
	}
	if len(tried) != 2 || tried[0] != "1-1.1.3" || tried[1] != "20120501030900000" {
		t.Errorf("selectors tried = %v, want the location first then the serial", tried)
	}
}

func TestReviveForReportsOriginalErrorWithoutFallback(t *testing.T) {
	orig := sdwireRevive
	t.Cleanup(func() { sdwireRevive = orig })

	wantErr := errors.New("no hub at that location")
	sdwireRevive = func(string, ...sdwire.Option) (sdwire.DeviceInfo, error) {
		return sdwire.DeviceInfo{}, wantErr
	}

	_, err := reviveFor(selection{selector: "1-9.9.9", named: true})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want it to wrap %v", err, wantErr)
	}
}
