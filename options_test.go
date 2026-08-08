package sdwire

import (
	"testing"
	"time"
)

func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()
	if o.legacySDWire3 {
		t.Error("expected legacy SDWire3 switching to be off by default")
	}
	if o.hostWaitTimeout != defaultHostWaitTimeout {
		t.Errorf("hostWaitTimeout = %v, want %v", o.hostWaitTimeout, defaultHostWaitTimeout)
	}
	if o.warnFunc == nil {
		t.Fatal("expected a default warn func, got nil")
	}
	o.warnFunc("must be safe to call and silently dropped") // should not panic
}

func TestWithWarningHandlerReceivesMessages(t *testing.T) {
	var got []string
	o := defaultOptions()
	WithWarningHandler(func(msg string) { got = append(got, msg) })(o)
	o.warnFunc("ganged power switching in use")

	if len(got) != 1 || got[0] != "ganged power switching in use" {
		t.Errorf("warnFunc calls = %v, want a single ganged-power warning", got)
	}
}

func TestWithWarningHandlerNilLeavesDefault(t *testing.T) {
	o := defaultOptions()
	WithWarningHandler(nil)(o)
	if o.warnFunc == nil {
		t.Fatal("expected default warn func to remain set")
	}
	o.warnFunc("must not panic")
}

func TestWithLegacySDWire3Switching(t *testing.T) {
	o := defaultOptions()
	WithLegacySDWire3Switching()(o)
	if !o.legacySDWire3 {
		t.Error("expected legacySDWire3 to be enabled")
	}
}

func TestWithHostWaitTimeout(t *testing.T) {
	o := defaultOptions()
	WithHostWaitTimeout(30 * time.Second)(o)
	if o.hostWaitTimeout != 30*time.Second {
		t.Errorf("hostWaitTimeout = %v, want 30s", o.hostWaitTimeout)
	}
}

func TestWithHubCachePath(t *testing.T) {
	o := defaultOptions()
	WithHubCachePath("/tmp/custom-hubports.json")(o)
	if o.hubCachePath != "/tmp/custom-hubports.json" {
		t.Errorf("hubCachePath = %q, want /tmp/custom-hubports.json", o.hubCachePath)
	}
}

func TestWithTargetPowerSetsPowerFunc(t *testing.T) {
	o := defaultOptions()
	called := false
	WithTargetPower(func(bool) error { called = true; return nil })(o)
	if o.powerFunc == nil {
		t.Fatal("expected powerFunc to be set")
	}
	if err := o.powerFunc(true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected the configured PowerFunc to be invoked")
	}
}
