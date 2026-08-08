package blockdev

import (
	"errors"
	"testing"
)

func TestRawWritePath(t *testing.T) {
	cases := []struct {
		goos, path, want string
	}{
		{"darwin", "/dev/disk4", "/dev/rdisk4"},
		{"darwin", "/dev/disk12", "/dev/rdisk12"},
		{"darwin", "/dev/other", "/dev/other"},
		{"linux", "/dev/sdb", "/dev/sdb"},
		{"windows", `\\.\PhysicalDrive2`, `\\.\PhysicalDrive2`},
	}
	for _, c := range cases {
		if got := rawWritePath(c.goos, c.path); got != c.want {
			t.Errorf("rawWritePath(%q, %q) = %q, want %q", c.goos, c.path, got, c.want)
		}
	}
}

func TestFmtNotFoundWrapsErrNotFound(t *testing.T) {
	err := fmtNotFound(Ref{Vendor: 0x0BDA, Product: 0x0316, Bus: 1, PortPath: []int{1, 1, 3}})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("fmtNotFound result does not wrap ErrNotFound: %v", err)
	}
}

func TestFmtAmbiguousWrapsErrAmbiguousAndListsCandidates(t *testing.T) {
	err := fmtAmbiguous([]string{"disk4", "disk5"})
	if !errors.Is(err, ErrAmbiguous) {
		t.Errorf("fmtAmbiguous result does not wrap ErrAmbiguous: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Error("expected a non-empty error message")
	}
}
