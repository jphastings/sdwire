package sdwire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jphastings/sdwire/internal/blockdev"
)

type fakeController struct {
	log *[]string
}

func (f *fakeController) SetMode(mode SwitchMode) error {
	*f.log = append(*f.log, "SetMode:"+mode.String())
	return nil
}
func (f *fakeController) Mode() (SwitchMode, error) { return ModeUnknown, nil }
func (f *fakeController) Close() error              { return nil }

// stubBlockdev replaces the package-level blockdev indirections with fakes
// backed by a temp-file "device" at devPath, restoring them on cleanup.
func stubBlockdev(t *testing.T, devPath string, size int64) {
	t.Helper()
	origFind, origSize, origUnmount, origRaw := blockdevFind, blockdevSize, blockdevUnmount, blockdevRawWritePath
	t.Cleanup(func() {
		blockdevFind, blockdevSize, blockdevUnmount, blockdevRawWritePath = origFind, origSize, origUnmount, origRaw
	})
	blockdevFind = func(blockdev.Ref) (string, error) { return devPath, nil }
	blockdevSize = func(string) (int64, error) { return size, nil }
	blockdevUnmount = func(string) error { return nil }
	blockdevRawWritePath = func(p string) string { return p }
}

func writeTestImage(t *testing.T, dir string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, "image.img")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestDevice(t *testing.T, dir string, size int64) string {
	t.Helper()
	path := filepath.Join(dir, "device")
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFlashAndBootSequenceAndRefDerivation(t *testing.T) {
	withStubSleep(t)

	dir := t.TempDir()
	imgData := []byte("hello sd card image data")
	imgPath := writeTestImage(t, dir, imgData)
	devPath := writeTestDevice(t, dir, 1<<20)

	var log []string
	origFind, origSize, origUnmount, origRaw := blockdevFind, blockdevSize, blockdevUnmount, blockdevRawWritePath
	t.Cleanup(func() {
		blockdevFind, blockdevSize, blockdevUnmount, blockdevRawWritePath = origFind, origSize, origUnmount, origRaw
	})
	var gotRef blockdev.Ref
	blockdevFind = func(ref blockdev.Ref) (string, error) {
		gotRef = ref
		log = append(log, "find")
		return devPath, nil
	}
	blockdevSize = func(string) (int64, error) {
		log = append(log, "size")
		return 1 << 20, nil
	}
	blockdevUnmount = func(string) error {
		log = append(log, "unmount")
		return nil
	}
	blockdevRawWritePath = func(p string) string { return p }

	s := &SDWire{
		info:       DeviceInfo{Generation: GenerationSDWire3, Bus: 2, PortPath: []int{1, 3}},
		controller: &fakeController{log: &log},
		powerFunc: func(on bool) error {
			log = append(log, "power:"+boolStr(on))
			return nil
		},
	}

	if err := s.FlashAndBoot(context.Background(), imgPath); err != nil {
		t.Fatalf("FlashAndBoot: %v", err)
	}

	// The second find+unmount pair is SetMode(ModeTarget)'s own default
	// unmount, re-checking for volumes that auto-remounted after the write.
	want := []string{"power:false", "SetMode:Host", "find", "size", "unmount", "find", "unmount", "SetMode:Target", "power:true"}
	if !slices.Equal(log, want) {
		t.Errorf("operation order = %v, want %v", log, want)
	}

	wantRef := blockdev.Ref{Vendor: SDWire3VID, Product: SDWire3PID, Bus: 2, PortPath: []int{1, 3}}
	if gotRef.Vendor != wantRef.Vendor || gotRef.Product != wantRef.Product || gotRef.Bus != wantRef.Bus || !slices.Equal(gotRef.PortPath, wantRef.PortPath) {
		t.Errorf("blockdev.Ref = %+v, want %+v", gotRef, wantRef)
	}

	written, err := os.ReadFile(devPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written[:len(imgData)], imgData) {
		t.Errorf("device content = %q, want prefix %q", written[:len(imgData)], imgData)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestFlashAndBootSkipsPowerWithoutPowerFunc(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, []byte("image"))
	devPath := writeTestDevice(t, dir, 1<<20)
	stubBlockdev(t, devPath, 1<<20)

	var log []string
	s := &SDWire{controller: &fakeController{log: &log}}

	if err := s.FlashAndBoot(context.Background(), imgPath); err != nil {
		t.Fatalf("FlashAndBoot: %v", err)
	}

	want := []string{"SetMode:Host", "SetMode:Target"}
	if !slices.Equal(log, want) {
		t.Errorf("operation order = %v, want %v (no power calls)", log, want)
	}
}

func TestFlashAndBootToppsUpOnlyRemainingDarkTime(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, []byte("image"))
	devPath := writeTestDevice(t, dir, 1<<20)
	stubBlockdev(t, devPath, 1<<20)

	slept := withStubSleep(t)

	t0 := time.Now()
	elapsed := 3 * time.Second
	calls := 0
	origNow := now
	now = func() time.Time {
		calls++
		if calls == 1 {
			return t0
		}
		return t0.Add(elapsed)
	}
	t.Cleanup(func() { now = origNow })

	var log []string
	s := &SDWire{
		controller: &fakeController{log: &log},
		powerFunc:  func(bool) error { return nil },
	}

	minDark := 8 * time.Second
	if err := s.FlashAndBoot(context.Background(), imgPath, WithFlashMinDarkTime(minDark)); err != nil {
		t.Fatalf("FlashAndBoot: %v", err)
	}

	want := minDark - elapsed
	if *slept != want {
		t.Errorf("slept %v, want %v (only the remainder of the dark time)", *slept, want)
	}
}

func TestFlashAndBootRefusesImageLargerThanDevice(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, make([]byte, 100))
	devPath := writeTestDevice(t, dir, 10) // device smaller than image
	stubBlockdev(t, devPath, 10)

	var log []string
	var powerCalls []bool
	s := &SDWire{
		controller: &fakeController{log: &log},
		powerFunc: func(on bool) error {
			powerCalls = append(powerCalls, on)
			return nil
		},
	}

	err := s.FlashAndBoot(context.Background(), imgPath)
	if err == nil {
		t.Fatal("expected an error for an image larger than the device")
	}

	if slices.Contains(log, "SetMode:Target") {
		t.Errorf("should not have switched back to target mode, log = %v", log)
	}
	if !slices.Equal(powerCalls, []bool{false}) {
		t.Errorf("power calls = %v, want only the initial power-off", powerCalls)
	}
}

func TestFlashAndBootAmbiguityPropagates(t *testing.T) {
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, []byte("image"))

	origFind := blockdevFind
	t.Cleanup(func() { blockdevFind = origFind })
	blockdevFind = func(blockdev.Ref) (string, error) {
		return "", fmt.Errorf("%w: disk4, disk5", blockdev.ErrAmbiguous)
	}

	var log []string
	s := &SDWire{controller: &fakeController{log: &log}}

	err := s.FlashAndBoot(context.Background(), imgPath, WithBlockDevTimeout(time.Second))
	if !errors.Is(err, blockdev.ErrAmbiguous) {
		t.Fatalf("err = %v, want wrapping %v", err, blockdev.ErrAmbiguous)
	}
	if slices.Contains(log, "SetMode:Target") {
		t.Errorf("should have stopped before switching back to target mode, log = %v", log)
	}
}

func TestFlashAndBootCtxCancellationStopsWriting(t *testing.T) {
	dir := t.TempDir()
	imgData := bytes.Repeat([]byte{0xAB}, 20)
	imgPath := writeTestImage(t, dir, imgData)
	devPath := writeTestDevice(t, dir, 1024)
	stubBlockdev(t, devPath, 1024)

	origChunk := flashChunkSize
	flashChunkSize = 5
	t.Cleanup(func() { flashChunkSize = origChunk })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var progressCalls int
	var log []string
	s := &SDWire{controller: &fakeController{log: &log}}

	err := s.FlashAndBoot(ctx, imgPath, WithFlashProgress(func(written, total int64) {
		progressCalls++
		cancel()
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if progressCalls != 1 {
		t.Fatalf("progress called %d times, want 1 (cancelled after the first chunk)", progressCalls)
	}

	written, err := os.ReadFile(devPath)
	if err != nil {
		t.Fatal(err)
	}
	var nonZero int
	for _, b := range written {
		if b != 0 {
			nonZero++
		}
	}
	if nonZero != 5 {
		t.Errorf("wrote %d non-zero bytes, want 5 (only the first chunk)", nonZero)
	}
	if slices.Contains(log, "SetMode:Target") {
		t.Errorf("should not have switched back to target mode after cancellation, log = %v", log)
	}
}

func TestFlashOptionSetters(t *testing.T) {
	o := defaultFlashOptions()

	var gotWritten, gotTotal int64
	WithFlashProgress(func(written, total int64) { gotWritten, gotTotal = written, total })(o)
	o.progress(3, 10)
	if gotWritten != 3 || gotTotal != 10 {
		t.Errorf("progress callback got (%d, %d), want (3, 10)", gotWritten, gotTotal)
	}

	WithBlockDevTimeout(5 * time.Second)(o)
	if o.blockDevTimeout != 5*time.Second {
		t.Errorf("blockDevTimeout = %v, want 5s", o.blockDevTimeout)
	}

	WithFlashMinDarkTime(3 * time.Second)(o)
	if o.minDark != 3*time.Second {
		t.Errorf("minDark = %v, want 3s", o.minDark)
	}

	WithMaxDeviceSize(1024)(o)
	if o.maxDeviceSize != 1024 {
		t.Errorf("maxDeviceSize = %d, want 1024", o.maxDeviceSize)
	}
}

func TestBlockdevRefDerivation(t *testing.T) {
	cases := []struct {
		name string
		info DeviceInfo
		want blockdev.Ref
	}{
		{
			name: "SDWireC",
			info: DeviceInfo{Generation: GenerationSDWireC, Bus: 1, PortPath: []int{2}},
			want: blockdev.Ref{Vendor: SDWireCVID, Product: SDWireCPID, Bus: 1, PortPath: []int{2}},
		},
		{
			name: "SDWire3",
			info: DeviceInfo{Generation: GenerationSDWire3, Bus: 1, PortPath: []int{1, 1, 3}},
			want: blockdev.Ref{Vendor: SDWire3VID, Product: SDWire3PID, Bus: 1, PortPath: []int{1, 1, 3}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := blockdevRef(c.info); got.Vendor != c.want.Vendor || got.Product != c.want.Product || got.Bus != c.want.Bus || !slices.Equal(got.PortPath, c.want.PortPath) {
				t.Errorf("blockdevRef(%+v) = %+v, want %+v", c.info, got, c.want)
			}
		})
	}
}
