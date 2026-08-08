package sdwire

import (
	"errors"
	"testing"
	"time"
)

func withStubSleep(t *testing.T) *time.Duration {
	t.Helper()
	var slept time.Duration
	orig := sleep
	sleep = func(d time.Duration) { slept = d }
	t.Cleanup(func() { sleep = orig })
	return &slept
}

func TestHasTargetPower(t *testing.T) {
	s := &SDWire{}
	if s.HasTargetPower() {
		t.Error("expected no target power configured by default")
	}
	s.SetTargetPower(func(bool) error { return nil })
	if !s.HasTargetPower() {
		t.Error("expected target power to be configured after SetTargetPower")
	}
}

func TestTargetPowerNoOp(t *testing.T) {
	s := &SDWire{}
	if err := s.TargetPower(true); err != nil {
		t.Fatalf("expected nil error with no PowerFunc configured, got %v", err)
	}
}

func TestPowerCycleNoOpWithoutPowerFunc(t *testing.T) {
	slept := withStubSleep(t)
	s := &SDWire{}
	if err := s.PowerCycle(0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *slept != 0 {
		t.Error("sleep should not be called when no PowerFunc is configured")
	}
}

func TestPowerCycleTurnsOffThenOn(t *testing.T) {
	slept := withStubSleep(t)
	var calls []bool
	s := &SDWire{}
	s.SetTargetPower(func(on bool) error {
		calls = append(calls, on)
		return nil
	})

	if err := s.PowerCycle(2 * time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 || calls[0] != false || calls[1] != true {
		t.Fatalf("expected [off, on], got %v", calls)
	}
	if *slept != 2*time.Second {
		t.Errorf("slept %v, want 2s", *slept)
	}
}

func TestPowerCycleDefaultsDarkTime(t *testing.T) {
	slept := withStubSleep(t)
	s := &SDWire{}
	s.SetTargetPower(func(bool) error { return nil })

	for _, minOff := range []time.Duration{0, -time.Second} {
		if err := s.PowerCycle(minOff); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *slept != DefaultMinDarkTime {
			t.Errorf("minOff=%v: slept %v, want default %v", minOff, *slept, DefaultMinDarkTime)
		}
	}
}

func TestPowerCycleNeverSleepsLessThanRequested(t *testing.T) {
	slept := withStubSleep(t)
	s := &SDWire{}
	s.SetTargetPower(func(bool) error { return nil })

	requested := 30 * time.Second
	if err := s.PowerCycle(requested); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *slept < requested {
		t.Errorf("slept %v, want at least %v", *slept, requested)
	}
}

func TestPowerCycleAbortsOnPowerOffError(t *testing.T) {
	slept := withStubSleep(t)
	var calls []bool
	wantErr := errors.New("relay stuck")

	s := &SDWire{}
	s.SetTargetPower(func(on bool) error {
		calls = append(calls, on)
		if !on {
			return wantErr
		}
		return nil
	})

	err := s.PowerCycle(time.Second)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if *slept != 0 {
		t.Error("should not sleep when power-off fails")
	}
	if len(calls) != 1 {
		t.Errorf("expected only the power-off call, got %v", calls)
	}
}
