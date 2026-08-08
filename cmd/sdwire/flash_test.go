package main

import "testing"

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
