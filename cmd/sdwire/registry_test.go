package main

import (
	"strings"
	"testing"
)

func TestMerossPowerFactoryRequiresIP(t *testing.T) {
	if _, err := merossPowerFactory(map[string]any{"key": "k"}); err == nil {
		t.Fatal("expected error when ip is missing")
	}
}

func TestMerossPowerFactoryBuildsFromMinimalConfig(t *testing.T) {
	fn, err := merossPowerFactory(map[string]any{"ip": "192.168.1.50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fn == nil {
		t.Fatal("expected a non-nil PowerFunc")
	}
}

func TestMerossPowerFactoryChannel(t *testing.T) {
	for _, ch := range []any{2, int64(2), float64(2), "2"} {
		fn, err := merossPowerFactory(map[string]any{"ip": "192.168.1.50", "channel": ch})
		if err != nil {
			t.Errorf("channel %v (%T): unexpected error: %v", ch, ch, err)
		}
		if fn == nil {
			t.Errorf("channel %v (%T): expected non-nil PowerFunc", ch, ch)
		}
	}
}

func TestMerossPowerFactoryInvalidChannel(t *testing.T) {
	if _, err := merossPowerFactory(map[string]any{"ip": "1.2.3.4", "channel": "not-a-number"}); err == nil {
		t.Fatal("expected error for non-numeric channel")
	}
}

func TestBuildPowerFuncUnknownTypeListsRegistered(t *testing.T) {
	_, err := buildPowerFunc(map[string]any{"type": "bogus"})
	if err == nil || !strings.Contains(err.Error(), "meross") {
		t.Fatalf("buildPowerFunc(bogus) error = %v, want it to mention \"meross\"", err)
	}
}

func TestBuildPowerFuncMissingType(t *testing.T) {
	if _, err := buildPowerFunc(map[string]any{"ip": "1.2.3.4"}); err == nil {
		t.Fatal("expected error when type is missing")
	}
}

func TestBuildPowerFuncDispatchesToMeross(t *testing.T) {
	fn, err := buildPowerFunc(map[string]any{"type": "meross", "ip": "192.168.1.50"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fn == nil {
		t.Fatal("expected a non-nil PowerFunc")
	}
}

func TestRegisteredPowerTypesIncludesMeross(t *testing.T) {
	found := false
	for _, ty := range registeredPowerTypes() {
		if ty == "meross" {
			found = true
		}
	}
	if !found {
		t.Errorf("registeredPowerTypes() = %v, want it to include \"meross\"", registeredPowerTypes())
	}
}
