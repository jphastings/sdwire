package sdwire

import (
	"errors"
	"slices"
	"testing"

	"github.com/jphastings/sdwire/internal/blockdev"
)

func stubModeBlockdev(t *testing.T, log *[]string, findErr, unmountErr error) {
	t.Helper()
	origFind, origUnmount := blockdevFind, blockdevUnmount
	blockdevFind = func(blockdev.Ref) (string, error) {
		*log = append(*log, "find")
		if findErr != nil {
			return "", findErr
		}
		return "/dev/faked", nil
	}
	blockdevUnmount = func(string) error {
		*log = append(*log, "unmount")
		return unmountErr
	}
	t.Cleanup(func() { blockdevFind, blockdevUnmount = origFind, origUnmount })
}

func TestSetModeTargetUnmountsFirst(t *testing.T) {
	var log []string
	stubModeBlockdev(t, &log, nil, nil)
	s := &SDWire{controller: &fakeController{log: &log}}

	if err := s.SetMode(ModeTarget); err != nil {
		t.Fatal(err)
	}
	if want := []string{"find", "unmount", "SetMode:Target"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

func TestSetModeTargetWithoutUnmount(t *testing.T) {
	var log []string
	stubModeBlockdev(t, &log, nil, nil)
	s := &SDWire{controller: &fakeController{log: &log}}

	if err := s.SetMode(ModeTarget, WithoutUnmount()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"SetMode:Target"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

func TestSetModeTargetProceedsWhenNoBlockDevice(t *testing.T) {
	var log []string
	stubModeBlockdev(t, &log, blockdev.ErrNotFound, nil)
	s := &SDWire{controller: &fakeController{log: &log}}

	if err := s.SetMode(ModeTarget); err != nil {
		t.Fatal(err)
	}
	if want := []string{"find", "SetMode:Target"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

func TestSetModeTargetAbortsOnUnmountFailure(t *testing.T) {
	var log []string
	unmountErr := errors.New("volume busy")
	stubModeBlockdev(t, &log, nil, unmountErr)
	s := &SDWire{controller: &fakeController{log: &log}}

	err := s.SetMode(ModeTarget)
	if !errors.Is(err, unmountErr) {
		t.Fatalf("err = %v, want wrapped %v", err, unmountErr)
	}
	if slices.Contains(log, "SetMode:Target") {
		t.Error("switch happened despite failed unmount")
	}
}

func TestSetModeHostSkipsUnmount(t *testing.T) {
	var log []string
	stubModeBlockdev(t, &log, nil, nil)
	s := &SDWire{controller: &fakeController{log: &log}}

	if err := s.SetMode(ModeHost); err != nil {
		t.Fatal(err)
	}
	if want := []string{"SetMode:Host"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}
