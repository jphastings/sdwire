package main

import (
	"testing"
	"time"
)

func TestFormatFlashProgress(t *testing.T) {
	const mib = 1024 * 1024
	cases := []struct {
		written, total int64
		want           string
	}{
		{0, 100 * mib, "\rflashed 0 / 100 MiB (0%)"},
		{50 * mib, 100 * mib, "\rflashed 50 / 100 MiB (50%)"},
		{100 * mib, 100 * mib, "\rflashed 100 / 100 MiB (100%)"},
		{0, 0, "\rflashed 0 / 0 MiB (0%)"},
	}
	for _, c := range cases {
		if got := formatFlashProgress(c.written, c.total); got != c.want {
			t.Errorf("formatFlashProgress(%d, %d) = %q, want %q", c.written, c.total, got, c.want)
		}
	}
}

func TestFormatWriteRate(t *testing.T) {
	if got := formatWriteRate(4*1024*1024, 2*time.Second); got != "2.0 MiB/s" {
		t.Errorf("formatWriteRate = %q, want 2.0 MiB/s", got)
	}
	// A chunk timed at zero (a stubbed clock, or a device fast enough to
	// round to nothing) must not divide by zero.
	if got := formatWriteRate(1024, 0); got != "instant" {
		t.Errorf("formatWriteRate(_, 0) = %q", got)
	}
}
