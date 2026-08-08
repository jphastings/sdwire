package sdwire

import "testing"

func TestSwitchModeString(t *testing.T) {
	cases := []struct {
		mode SwitchMode
		want string
	}{
		{ModeTarget, "Target"},
		{ModeHost, "Host"},
		{ModeUnknown, "Unknown"},
		{SwitchMode(99), "Unknown"},
	}
	for _, c := range cases {
		if got := c.mode.String(); got != c.want {
			t.Errorf("SwitchMode(%d).String() = %q, want %q", int(c.mode), got, c.want)
		}
	}
}
