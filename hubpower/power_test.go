package hubpower

import (
	"errors"
	"testing"

	"github.com/google/gousb"
)

func TestParseHubCharacteristics(t *testing.T) {
	cases := []struct {
		name string
		wHub uint16
		want PowerSwitchingMode
	}{
		{"ganged", 0b00, PowerSwitchingGanged},
		{"per-port", 0b01, PowerSwitchingPerPort},
		{"reserved bit set", 0b10, PowerSwitchingNone},
		{"no switching, other bits set", 0b1110, PowerSwitchingNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			desc := []byte{9, 0x29, 4, byte(c.wHub), byte(c.wHub >> 8)}
			got, err := parseHubCharacteristics(desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}

	t.Run("too short", func(t *testing.T) {
		if _, err := parseHubCharacteristics([]byte{1, 2, 3}); err == nil {
			t.Fatal("expected an error for a too-short descriptor")
		}
	})
}

func TestDecodePortStatus(t *testing.T) {
	cases := []struct {
		name   string
		status uint16
		spec   gousb.BCD
		want   PortStatus
	}{
		{"USB2 powered+connected", 0x0101, gousb.Version(2, 0), PortStatus{Powered: true, Connected: true}},
		{"USB2 unpowered, disconnected", 0x0000, gousb.Version(2, 0), PortStatus{Powered: false, Connected: false}},
		{"USB2 powered, not connected", 0x0100, gousb.Version(2, 0), PortStatus{Powered: true, Connected: false}},
		{"USB3 powered+connected", 0x0201, gousb.Version(3, 0), PortStatus{Powered: true, Connected: true}},
		{"USB3 hub ignores USB2's power bit", 0x0100, gousb.Version(3, 0), PortStatus{Powered: false, Connected: false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			buf := []byte{byte(c.status), byte(c.status >> 8), 0, 0}
			got, err := decodePortStatus(buf, c.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}

	t.Run("too short", func(t *testing.T) {
		if _, err := decodePortStatus([]byte{1}, gousb.Version(2, 0)); err == nil {
			t.Fatal("expected an error for a too-short status buffer")
		}
	})
}

func TestParentPathAndPort(t *testing.T) {
	t.Run("empty path is a root port", func(t *testing.T) {
		_, _, err := parentPathAndPort(nil)
		if !errors.Is(err, ErrRootPort) {
			t.Fatalf("err = %v, want ErrRootPort", err)
		}
	})

	t.Run("single-element path is a root port", func(t *testing.T) {
		_, _, err := parentPathAndPort([]int{3})
		if !errors.Is(err, ErrRootPort) {
			t.Fatalf("err = %v, want ErrRootPort", err)
		}
	})

	t.Run("nested path splits into parent and port", func(t *testing.T) {
		parent, port, err := parentPathAndPort([]int{1, 2, 3})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if port != 3 {
			t.Errorf("port = %d, want 3", port)
		}
		if len(parent) != 2 || parent[0] != 1 || parent[1] != 2 {
			t.Errorf("parent = %v, want [1 2]", parent)
		}
	})
}
