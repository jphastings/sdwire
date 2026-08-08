package sdwire

import (
	"errors"
	"strings"
	"testing"
)

func TestDeviceInfoIdentityAndLocation(t *testing.T) {
	cases := []struct {
		name    string
		info    DeviceInfo
		wantID  string
		wantLoc string
	}{
		{
			name:    "with port path",
			info:    DeviceInfo{Serial: "20120501030900000", Bus: 1, PortPath: []int{1, 1, 3}},
			wantID:  "20120501030900000.1.1.3",
			wantLoc: "1-1.1.3",
		},
		{
			name:    "empty port path",
			info:    DeviceInfo{Serial: "ABC123", Bus: 2, PortPath: nil},
			wantID:  "ABC123",
			wantLoc: "2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.info.Identity(); got != c.wantID {
				t.Errorf("Identity() = %q, want %q", got, c.wantID)
			}
			if got := c.info.Location(); got != c.wantLoc {
				t.Errorf("Location() = %q, want %q", got, c.wantLoc)
			}
		})
	}
}

func TestSelectBySerial(t *testing.T) {
	candidates := []DeviceInfo{
		{Serial: "SN1", Bus: 1, PortPath: []int{1}},
		{Serial: "20120501030900000", Bus: 2, PortPath: []int{2, 3}},
		{Serial: "20120501030900000", Bus: 2, PortPath: []int{4, 5}},
	}

	t.Run("plain serial, unique", func(t *testing.T) {
		idx, err := selectBySerial(candidates, "SN1")
		if err != nil || idx != 0 {
			t.Fatalf("got idx=%d err=%v, want idx=0 err=nil", idx, err)
		}
	})

	t.Run("plain serial, ambiguous", func(t *testing.T) {
		_, err := selectBySerial(candidates, "20120501030900000")
		if err == nil {
			t.Fatal("expected an ambiguity error")
		}
		for _, want := range []string{"20120501030900000.2.3", "20120501030900000.4.5"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should list candidate identity %q", err, want)
			}
		}
	})

	t.Run("suffixed identity without bus", func(t *testing.T) {
		idx, err := selectBySerial(candidates, "20120501030900000.2.3")
		if err != nil || idx != 1 {
			t.Fatalf("got idx=%d err=%v, want idx=1 err=nil", idx, err)
		}
	})

	t.Run("suffixed identity with bus", func(t *testing.T) {
		idx, err := selectBySerial(candidates, "20120501030900000.2.2.3")
		if err != nil || idx != 1 {
			t.Fatalf("got idx=%d err=%v, want idx=1 err=nil", idx, err)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if _, err := selectBySerial(candidates, "nope"); err == nil {
			t.Fatal("expected an error for an unknown serial")
		}
	})
}

// TestNoMatchVsAmbiguousErrorDistinction locks in that only a genuine "no
// match" error wraps errNoDeviceFound. connect() relies on this to decide
// whether the hubpower cache fallback is worth trying: an ambiguous match
// means real candidates already exist and should be reported as-is, not
// silently resolved by reviving whatever the cache happens to find.
func TestNoMatchVsAmbiguousErrorDistinction(t *testing.T) {
	candidates := []DeviceInfo{
		{Serial: "DUP", Bus: 1, PortPath: []int{1}},
		{Serial: "DUP", Bus: 1, PortPath: []int{2}},
	}

	_, noMatchErr := selectBySerial(candidates, "nope")
	if !errors.Is(noMatchErr, errNoDeviceFound) {
		t.Errorf("no-match error = %v, want it to wrap errNoDeviceFound", noMatchErr)
	}

	_, ambiguousErr := selectBySerial(candidates, "DUP")
	if errors.Is(ambiguousErr, errNoDeviceFound) {
		t.Errorf("ambiguous-match error = %v, should not wrap errNoDeviceFound", ambiguousErr)
	}
}

func TestSelectByIdentity(t *testing.T) {
	candidates := []DeviceInfo{
		{Serial: "S", Bus: 1, PortPath: []int{2, 3}},
		{Serial: "S", Bus: 1, PortPath: []int{4, 5}},
	}

	t.Run("suffixed identity without bus", func(t *testing.T) {
		idx, err := selectByIdentity(candidates, "S.2.3")
		if err != nil || idx != 0 {
			t.Fatalf("got idx=%d err=%v, want idx=0 err=nil", idx, err)
		}
	})

	t.Run("suffixed identity with bus", func(t *testing.T) {
		idx, err := selectByIdentity(candidates, "S.1.2.3")
		if err != nil || idx != 0 {
			t.Fatalf("got idx=%d err=%v, want idx=0 err=nil", idx, err)
		}
	})

	t.Run("location form", func(t *testing.T) {
		idx, err := selectByIdentity(candidates, "1-4.5")
		if err != nil || idx != 1 {
			t.Fatalf("got idx=%d err=%v, want idx=1 err=nil", idx, err)
		}
	})

	t.Run("location form, bus only", func(t *testing.T) {
		idx, err := selectByIdentity([]DeviceInfo{{Serial: "X", Bus: 7}}, "7")
		if err != nil || idx != 0 {
			t.Fatalf("got idx=%d err=%v, want idx=0 err=nil", idx, err)
		}
	})
}
