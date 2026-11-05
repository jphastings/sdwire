package sdwire

import (
	"slices"
	"testing"
	"time"

	"github.com/jphastings/sdwire/internal/usbsuspend"
)

// fakeReader stands in for usbsuspend: StateOf reports seen in order,
// repeating the last, and Suspend/Resume are recorded.
type fakeReader struct {
	seen  []usbsuspend.State
	calls []string
}

func stubReader(t *testing.T, seen ...usbsuspend.State) *fakeReader {
	t.Helper()
	r := &fakeReader{seen: seen}
	origState, origSuspend, origResume, origSleep, origNow := usbStateOf, usbSuspend, usbResume, sleep, now
	t.Cleanup(func() {
		usbStateOf, usbSuspend, usbResume, sleep, now = origState, origSuspend, origResume, origSleep, origNow
	})

	usbStateOf = func(int, []int) (usbsuspend.State, error) {
		st := r.seen[0]
		if len(r.seen) > 1 {
			r.seen = r.seen[1:]
		}
		return st, nil
	}
	usbSuspend = func(int, []int) error { r.calls = append(r.calls, "suspend"); return nil }
	usbResume = func(int, []int) error { r.calls = append(r.calls, "resume"); return nil }
	clock := time.Unix(0, 0)
	now = func() time.Time { return clock }
	sleep = func(d time.Duration) { clock = clock.Add(d) }
	return r
}

func newTestSDWire3() *sdwire3Controller {
	return &sdwire3Controller{info: DeviceInfo{Bus: 1, PortPath: []int{1}}, hostWaitTimeout: 20 * time.Second}
}

func TestSDWire3TargetSuspendsTheReader(t *testing.T) {
	r := stubReader(t, usbsuspend.Configured, usbsuspend.Unconfigured, usbsuspend.Suspended)

	if err := newTestSDWire3().SetMode(ModeTarget); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"suspend"}) {
		t.Errorf("calls = %v, want one suspend", r.calls)
	}
}

func TestSDWire3HostWaitsForTheReaderToComeBackConfigured(t *testing.T) {
	r := stubReader(t, usbsuspend.Suspended, usbsuspend.Absent, usbsuspend.Absent, usbsuspend.Configured)

	if err := newTestSDWire3().SetMode(ModeHost); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"resume"}) {
		t.Errorf("calls = %v, want one resume", r.calls)
	}
	if len(r.seen) != 1 {
		t.Error("returned before the reader came back configured")
	}
}

func TestSDWire3TargetFailsWhenTheReaderStaysAwake(t *testing.T) {
	stubReader(t, usbsuspend.Configured, usbsuspend.Unconfigured)

	if err := newTestSDWire3().SetMode(ModeTarget); err == nil {
		t.Fatal("expected an error for a reader that never suspended")
	}
}

// On Linux opening a suspended reader wakes it, and re-enumerating a
// configured one drops its mounted volumes, so a reader already where it
// is asked to go must not be touched.
func TestSDWire3SwitchingToTheCurrentModeLeavesTheReaderAlone(t *testing.T) {
	for st, mode := range map[usbsuspend.State]SwitchMode{usbsuspend.Suspended: ModeTarget, usbsuspend.Configured: ModeHost} {
		r := stubReader(t, st)
		if err := newTestSDWire3().SetMode(mode); err != nil {
			t.Fatal(err)
		}
		if len(r.calls) != 0 {
			t.Errorf("%v while %v: calls = %v, want none", mode, st, r.calls)
		}
	}
}

func TestSDWire3ModeReadsTheReaderState(t *testing.T) {
	for st, want := range map[usbsuspend.State]SwitchMode{
		usbsuspend.Suspended:    ModeTarget,
		usbsuspend.Configured:   ModeHost,
		usbsuspend.Unconfigured: ModeUnknown,
		usbsuspend.Absent:       ModeUnknown,
	} {
		stubReader(t, st)
		if got, err := newTestSDWire3().Mode(); err != nil || got != want {
			t.Errorf("Mode() while %v = %v, %v; want %v", st, got, err, want)
		}
	}
}
