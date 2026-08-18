package sdwire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"
)

// fakeRawDevice stands in for the reader's raw block device, optionally
// failing every write past failAfter bytes with err — the shape of a reader
// that stalls mid-flash and is torn off the USB bus by the OS.
type fakeRawDevice struct {
	data       []byte
	offset     int64
	failAfter  int64
	err        error
	writtenAt  []int64
	writeCalls int
}

func (d *fakeRawDevice) Write(p []byte) (int, error) {
	d.writeCalls++
	d.writtenAt = append(d.writtenAt, d.offset)
	if d.err != nil && d.offset+int64(len(p)) > d.failAfter {
		return 0, d.err
	}
	if end := d.offset + int64(len(p)); end > int64(len(d.data)) {
		d.data = append(d.data, make([]byte, end-int64(len(d.data)))...)
	}
	copy(d.data[d.offset:], p)
	d.offset += int64(len(p))
	return len(p), nil
}

func (d *fakeRawDevice) Seek(off int64, whence int) (int64, error) {
	d.offset = off
	return off, nil
}
func (d *fakeRawDevice) Sync() error  { return nil }
func (d *fakeRawDevice) Close() error { return nil }

// revivingController is a fakeController that can also power-cycle its
// device, counting how often it was asked to and optionally healing the
// device it hands back.
type revivingController struct {
	fakeController
	revives  int
	err      error
	onRevive func()
}

func (c *revivingController) Revive() error {
	c.revives++
	if c.onRevive != nil {
		c.onRevive()
	}
	return c.err
}

// stubRawDevice points openRawDevice at dev, restoring the real one after.
func stubRawDevice(t *testing.T, dev *fakeRawDevice) {
	t.Helper()
	orig := openRawDevice
	openRawDevice = func(string) (rawDevice, error) { return dev, nil }
	t.Cleanup(func() { openRawDevice = orig })
}

// shrinkChunks makes the write loop take several chunks over a small image.
func shrinkChunks(t *testing.T, size int) {
	t.Helper()
	orig := flashChunkSize
	flashChunkSize = size
	t.Cleanup(func() { flashChunkSize = orig })
}

func flashFixture(t *testing.T, imgData []byte) (string, *revivingController, *SDWire) {
	t.Helper()
	withStubSleep(t)
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, imgData)
	stubBlockdev(t, writeTestDevice(t, dir, 1<<20), 1<<20)

	var log []string
	ctrl := &revivingController{fakeController: fakeController{log: &log}}
	s := &SDWire{
		info:       DeviceInfo{Generation: GenerationSDWire3, Bus: 2, PortPath: []int{1, 3}},
		controller: ctrl,
	}
	return imgPath, ctrl, s
}

// The whole point of the recovery: a reader that vanishes 1KiB into a 3KiB
// write must not cost the flash, and must not cost the 1KiB already written.
func TestFlashResumesWhereTheReaderDroppedOffTheBus(t *testing.T) {
	imgData := bytes.Repeat([]byte("sdwire--"), 384) // 3 KiB
	shrinkChunks(t, 1024)
	imgPath, ctrl, s := flashFixture(t, imgData)

	dev := &fakeRawDevice{failAfter: 1024, err: syscall.ENXIO}
	stubRawDevice(t, dev)
	ctrl.onRevive = func() { dev.err = nil } // the power cycle brings it back

	var warnings []string
	s.warn = func(msg string) { warnings = append(warnings, msg) }

	if err := s.FlashAndBoot(context.Background(), imgPath); err != nil {
		t.Fatalf("FlashAndBoot: %v", err)
	}

	if !bytes.Equal(dev.data, imgData) {
		t.Errorf("device holds %d bytes, want the %d byte image intact", len(dev.data), len(imgData))
	}
	if ctrl.revives != 1 {
		t.Errorf("revives = %d, want 1", ctrl.revives)
	}
	// Writes: 0, 1024 (fails), then resumed 1024, 2048 — never back to 0.
	if got := dev.writtenAt; len(got) != 4 || got[2] != 1024 {
		t.Errorf("wrote at offsets %v, want the retry to resume at 1024 rather than restart", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one telling the operator it happened", warnings)
	}
}

// A full card or a bad image is not something a power cycle can fix, and
// retrying one wastes minutes before reporting the same failure.
func TestFlashDoesNotPowerCycleForErrorsThatArentTheReaderVanishing(t *testing.T) {
	shrinkChunks(t, 1024)
	imgPath, ctrl, s := flashFixture(t, bytes.Repeat([]byte("x"), 3072))

	stubRawDevice(t, &fakeRawDevice{failAfter: 1024, err: syscall.ENOSPC})

	err := s.FlashAndBoot(context.Background(), imgPath)
	if !errors.Is(err, syscall.ENOSPC) {
		t.Errorf("err = %v, want the original ENOSPC", err)
	}
	if ctrl.revives != 0 {
		t.Errorf("revives = %d, want none", ctrl.revives)
	}
}

func TestFlashGivesUpAfterWriteRetries(t *testing.T) {
	shrinkChunks(t, 1024)
	imgPath, ctrl, s := flashFixture(t, bytes.Repeat([]byte("x"), 3072))

	stubRawDevice(t, &fakeRawDevice{failAfter: 1024, err: syscall.ENXIO})

	err := s.FlashAndBoot(context.Background(), imgPath, WithWriteRetries(2))
	if !errors.Is(err, syscall.ENXIO) {
		t.Errorf("err = %v, want the underlying write error", err)
	}
	if ctrl.revives != 2 {
		t.Errorf("revives = %d, want to stop after the configured 2", ctrl.revives)
	}
}

func TestFlashReportsWhenTheReaderCannotBeBroughtBack(t *testing.T) {
	shrinkChunks(t, 1024)
	imgPath, ctrl, s := flashFixture(t, bytes.Repeat([]byte("x"), 3072))
	ctrl.err = errors.New("hub port stayed dark")

	stubRawDevice(t, &fakeRawDevice{failAfter: 1024, err: syscall.ENXIO})

	err := s.FlashAndBoot(context.Background(), imgPath)
	if !errors.Is(err, syscall.ENXIO) {
		t.Errorf("err = %v, want it to still carry the write failure", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("hub port stayed dark")) {
		t.Errorf("err = %v, want it to name why recovery failed too", err)
	}
}

// A controller with no power control (SDWireC) has no recovery to offer, so
// the write error must surface immediately rather than after three waits.
func TestFlashWithoutAReviverFailsImmediately(t *testing.T) {
	withStubSleep(t)
	dir := t.TempDir()
	imgPath := writeTestImage(t, dir, bytes.Repeat([]byte("x"), 3072))
	stubBlockdev(t, writeTestDevice(t, dir, 1<<20), 1<<20)
	shrinkChunks(t, 1024)
	stubRawDevice(t, &fakeRawDevice{failAfter: 1024, err: syscall.ENXIO})

	var log []string
	s := &SDWire{
		info:       DeviceInfo{Generation: GenerationSDWireC, Bus: 2, PortPath: []int{1, 3}},
		controller: &fakeController{log: &log},
	}

	if err := s.FlashAndBoot(context.Background(), imgPath); !errors.Is(err, syscall.ENXIO) {
		t.Errorf("err = %v, want the write error unretried", err)
	}
}

func TestWriteTimingReportsEveryChunk(t *testing.T) {
	shrinkChunks(t, 1024)
	imgPath, _, s := flashFixture(t, bytes.Repeat([]byte("x"), 3072))
	stubRawDevice(t, &fakeRawDevice{})

	type chunk struct {
		offset int64
		size   int
	}
	var chunks []chunk
	err := s.FlashAndBoot(context.Background(), imgPath,
		WithWriteTiming(func(offset int64, size int, took time.Duration) {
			chunks = append(chunks, chunk{offset, size})
		}))
	if err != nil {
		t.Fatalf("FlashAndBoot: %v", err)
	}

	want := []chunk{{0, 1024}, {1024, 1024}, {2048, 1024}}
	if len(chunks) != len(want) {
		t.Fatalf("timed %d chunks, want %d: %v", len(chunks), len(want), chunks)
	}
	for i, c := range chunks {
		if c != want[i] {
			t.Errorf("chunk %d = %+v, want %+v", i, c, want[i])
		}
	}
}

var _ io.WriteSeeker = (*fakeRawDevice)(nil)
