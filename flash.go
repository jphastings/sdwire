package sdwire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/jphastings/sdwire/internal/blockdev"
)

// defaultBlockDevTimeout is how long FlashAndBoot waits, after switching to
// host mode, for the reader's block device to appear. Re-enumeration is
// typically visible within ~6s; this errs well above that.
const defaultBlockDevTimeout = 30 * time.Second

// defaultMaxDeviceSize is the sanity cap FlashAndBoot refuses to write past,
// guarding against a mis-mapped block device path pointing at something
// much larger than any SD card.
const defaultMaxDeviceSize = 2 * 1024 * 1024 * 1024 * 1024 // 2 TiB

// sectorSize is the write-alignment granularity raw block devices require.
const sectorSize = 512

// flashChunkSize is how much of the image is read and written per
// iteration; it must stay a multiple of sectorSize for raw device writes.
// It is a var, rather than a const, purely so tests can shrink it to
// exercise the ctx-cancellation-mid-write path without huge fixtures.
var flashChunkSize = 4 * 1024 * 1024

// defaultWriteRetries is how many times FlashAndBoot will revive a reader
// that drops off the bus mid-write and resume, before giving up. A reader
// that cannot survive three attempts is failing for a reason a fourth power
// cycle won't fix.
const defaultWriteRetries = 3

// blockDevPollInterval is how often FlashAndBoot polls for the reader's
// block device to appear after switching to host mode.
const blockDevPollInterval = 1 * time.Second

// Indirections over internal/blockdev and time.Now, swapped out in tests.
var (
	blockdevFind         = blockdev.Find
	blockdevSize         = blockdev.Size
	blockdevUnmount      = blockdev.Unmount
	blockdevRawWritePath = blockdev.RawWritePath
	now                  = time.Now
	openRawDevice        = openRawDeviceFile
)

// rawDevice is the subset of *os.File that writing an image to a raw block
// device needs.
type rawDevice interface {
	io.WriteSeeker
	Sync() error
	Close() error
}

// openRawDeviceFile opens a raw block device for writing. It sits behind
// the openRawDevice indirection so tests can stand in a device that fails
// the way a reader dropping off the bus mid-write does — the one thing a
// temp file standing in for a device cannot do on its own.
func openRawDeviceFile(path string) (rawDevice, error) {
	return os.OpenFile(path, os.O_WRONLY, 0)
}

// flashOptions holds the fully-resolved configuration for a FlashAndBoot
// call.
type flashOptions struct {
	progress        func(written, total int64)
	writeTiming     func(offset int64, size int, took time.Duration)
	blockDevTimeout time.Duration
	minDark         time.Duration
	maxDeviceSize   int64
	writeRetries    int
}

func defaultFlashOptions() *flashOptions {
	return &flashOptions{
		blockDevTimeout: defaultBlockDevTimeout,
		minDark:         DefaultMinDarkTime,
		maxDeviceSize:   defaultMaxDeviceSize,
		writeRetries:    defaultWriteRetries,
	}
}

// FlashOption customizes a FlashAndBoot call.
type FlashOption func(*flashOptions)

// WithFlashProgress configures a callback invoked after each chunk is
// written during FlashAndBoot, reporting bytes written so far and the total
// image size.
func WithFlashProgress(fn func(written, total int64)) FlashOption {
	return func(o *flashOptions) { o.progress = fn }
}

// WithBlockDevTimeout sets how long FlashAndBoot waits for the reader's
// block device to appear after switching to host mode. The default is 30
// seconds.
func WithBlockDevTimeout(d time.Duration) FlashOption {
	return func(o *flashOptions) { o.blockDevTimeout = d }
}

// WithFlashMinDarkTime sets the minimum time FlashAndBoot keeps the target
// board's power off before powering it back on, matching (*SDWire).
// PowerCycle's minOff semantics. The default is DefaultMinDarkTime.
func WithFlashMinDarkTime(d time.Duration) FlashOption {
	return func(o *flashOptions) { o.minDark = d }
}

// WithWriteRetries sets how many times a flash will recover from the
// reader dropping off the bus mid-write — power-cycling its hub port and
// resuming from the last completed chunk — before giving up. Zero disables
// the recovery, failing on the first such error as a plain write error.
func WithWriteRetries(n int) FlashOption {
	return func(o *flashOptions) { o.writeRetries = n }
}

// WithWriteTiming registers a callback invoked after each chunk is written
// to the card, with the chunk's offset within the image, its size in bytes,
// and how long the write itself took (excluding reading the image).
//
// It exists to make the shape of a flash measurable: a reader whose
// internal buffer is backing up shows up here as chunk times climbing well
// before it stalls outright.
func WithWriteTiming(fn func(offset int64, size int, took time.Duration)) FlashOption {
	return func(o *flashOptions) { o.writeTiming = fn }
}

// WithMaxDeviceSize sets the sanity cap FlashAndBoot refuses to write past,
// guarding against a mis-mapped block device. The default is 2TiB.
func WithMaxDeviceSize(bytes int64) FlashOption {
	return func(o *flashOptions) { o.maxDeviceSize = bytes }
}

// FlashAndBoot writes the image at imagePath to this SDWire's SD card and
// boots the target from it:
//
//  1. Powers off the target (a no-op if no PowerFunc is configured; see
//     WithTargetPower), recording when power was cut.
//  2. Switches to host mode and waits for the reader's block device to
//     appear.
//  3. Checks the image fits the device (and the device fits a sanity cap),
//     then unmounts any mounted volumes on it.
//  4. Raw-writes the image to the device in chunks, reporting progress. If
//     the reader drops off the bus mid-write — see WithWriteRetries — its
//     hub port is power-cycled and the write resumes where it stopped.
//  5. Switches back to target mode.
//  6. Tops up the dark time begun in step 1 to at least the configured
//     minimum, then powers the target back on.
//
// Because a target board normally only probes its SD slot at boot (see
// (*SDWire).SetMode), step 6's power-on is what actually makes the newly
// written card visible to it. If no PowerFunc is configured, step 6 is
// skipped entirely and the caller is responsible for booting the target
// themselves.
//
// Each step checks ctx before proceeding, so a cancelled context stops the
// operation at the next opportunity (including between write chunks).
func (s *SDWire) FlashAndBoot(ctx context.Context, imagePath string, opts ...FlashOption) error {
	o := defaultFlashOptions()
	for _, opt := range opts {
		opt(o)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	powerOffAt := now()
	if err := s.TargetPower(false); err != nil {
		return fmt.Errorf("powering off target: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.SetMode(ModeHost); err != nil {
		return fmt.Errorf("switching to host mode: %w", err)
	}

	devPath, err := waitForBlockDevice(ctx, blockdevRef(s.info), o.blockDevTimeout)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkSizesAndUnmount(imagePath, devPath, o.maxDeviceSize); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.writeImageResuming(ctx, imagePath, devPath, o); err != nil {
		return err
	}

	if err := s.SetMode(ModeTarget); err != nil {
		return fmt.Errorf("switching to target mode: %w", err)
	}

	if s.HasTargetPower() {
		if remaining := o.minDark - now().Sub(powerOffAt); remaining > 0 {
			sleep(remaining)
		}
		if err := s.TargetPower(true); err != nil {
			return fmt.Errorf("powering on target: %w", err)
		}
	}

	return nil
}

// blockdevRef derives the blockdev.Ref identifying the reader's own USB
// device from an SDWire's DeviceInfo: for SDWire3, the Realtek reader
// itself; for SDWireC, the same FTDI composite device the controller uses.
func blockdevRef(info DeviceInfo) blockdev.Ref {
	vendor, product := uint16(SDWireCVID), uint16(SDWireCPID)
	if info.Generation == GenerationSDWire3 {
		vendor, product = uint16(SDWire3VID), uint16(SDWire3PID)
	}
	return blockdev.Ref{
		Vendor:   vendor,
		Product:  product,
		Bus:      info.Bus,
		PortPath: info.PortPath,
	}
}

// waitForBlockDevice polls blockdev.Find at blockDevPollInterval until the
// reader's block device appears or timeout elapses. An ambiguous match is
// returned immediately rather than retried, since polling longer cannot
// resolve it.
func waitForBlockDevice(ctx context.Context, ref blockdev.Ref, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		devPath, err := blockdevFind(ref)
		if err == nil {
			return devPath, nil
		}
		if !errors.Is(err, blockdev.ErrNotFound) {
			return "", fmt.Errorf("finding reader's block device: %w", err)
		}
		if !time.Now().Before(deadline) {
			return "", fmt.Errorf("no block device appeared for reader within %s: %w", timeout, err)
		}
		sleep(blockDevPollInterval)
	}
}

// checkSizesAndUnmount validates that imagePath is non-empty and fits on
// the device at devPath, that the device itself is not implausibly larger
// than maxDeviceSize, and then unmounts any mounted volumes on the device.
func checkSizesAndUnmount(imagePath, devPath string, maxDeviceSize int64) error {
	imgInfo, err := os.Stat(imagePath)
	if err != nil {
		return fmt.Errorf("checking image: %w", err)
	}
	if imgInfo.Size() <= 0 {
		return fmt.Errorf("image %s is empty", imagePath)
	}

	devSize, err := blockdevSize(devPath)
	if err != nil {
		return fmt.Errorf("getting device size: %w", err)
	}
	if imgInfo.Size() > devSize {
		return fmt.Errorf("image %s is %d bytes, larger than device %s (%d bytes)", imagePath, imgInfo.Size(), devPath, devSize)
	}
	if devSize > maxDeviceSize {
		return fmt.Errorf("device %s is %d bytes, exceeding the %d byte sanity cap (see WithMaxDeviceSize)", devPath, devSize, maxDeviceSize)
	}

	if err := blockdevUnmount(devPath); err != nil {
		return fmt.Errorf("unmounting %s: %w", devPath, err)
	}
	return nil
}

// deviceLost reports whether err is the shape a reader that has gone away
// mid-write produces. A stalled reader is torn off the USB bus by the OS,
// after which the open raw-device handle answers ENXIO ("device not
// configured") or ENODEV, and in-flight I/O fails with EIO. Anything else —
// a full card, a bad image, a cancelled context — is a real failure that
// power-cycling cannot help, and must not be retried.
func deviceLost(err error) bool {
	return errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.ENODEV) || errors.Is(err, syscall.EIO)
}

// writeImageResuming writes the image to the reader's block device,
// recovering from the reader dropping off the bus mid-write: it power-cycles
// the device's hub port, waits for the same reader to come back at the same
// USB location, unmounts whatever the OS auto-mounted from the partially
// written card, and resumes from the last completed chunk.
//
// The reader's stall itself is not preventable in software — the device
// stalls its own bulk endpoint and the OS's port reset fails to clear it —
// but it need not cost the whole flash. Recovery is bounded by
// o.writeRetries and each attempt is warned about, so a flash that needed
// several power cycles never looks like a clean one.
//
// Only generations whose controller can power-cycle the device (SDWire3)
// can recover; for any other, the original write error is returned as-is.
func (s *SDWire) writeImageResuming(ctx context.Context, imagePath, devPath string, o *flashOptions) error {
	var resumeFrom int64
	for attempt := 1; ; attempt++ {
		written, err := writeImage(ctx, imagePath, blockdevRawWritePath(devPath), resumeFrom, o)
		if err == nil {
			return nil
		}

		reviver, canRevive := s.controller.(deviceReviver)
		if !deviceLost(err) || attempt > o.writeRetries || !canRevive {
			return err
		}

		s.warnf("the reader stopped responding %d MiB into the write; power-cycling its hub port and resuming (attempt %d of %d)",
			written/(1024*1024), attempt, o.writeRetries)

		if rerr := reviver.Revive(); rerr != nil {
			return fmt.Errorf("%w (power-cycling the reader to resume failed: %v)", err, rerr)
		}

		devPath, err = recoveredBlockDevice(ctx, s.info, o)
		if err != nil {
			return err
		}

		// Chunks are sector multiples and a failed write is never counted,
		// so this is already aligned; keeping it explicit means a future
		// change to the chunking can't silently produce an unaligned seek
		// on a raw device.
		resumeFrom = written - written%int64(sectorSize)
	}
}

// recoveredBlockDevice waits for a just-revived reader's block device to
// reappear and unmounts anything the OS mounted from it. A partially
// written card often still carries a mountable filesystem from whatever was
// on it before, and writing to a mounted device raw is exactly what
// checkSizesAndUnmount exists to prevent.
func recoveredBlockDevice(ctx context.Context, info DeviceInfo, o *flashOptions) (string, error) {
	devPath, err := waitForBlockDevice(ctx, blockdevRef(info), o.blockDevTimeout)
	if err != nil {
		return "", fmt.Errorf("the reader came back but its block device did not: %w", err)
	}
	if err := blockdevUnmount(devPath); err != nil {
		return "", fmt.Errorf("unmounting the revived reader at %s: %w", devPath, err)
	}
	return devPath, nil
}

// writeImage copies imagePath to rawPath in flashChunkSize chunks, calling
// o.progress (if non-nil) after each chunk, checking ctx between chunks,
// and syncing before close.
//
// Writing starts at resumeFrom, seeking both image and device to it, so a
// write interrupted by the reader disappearing can be continued rather than
// restarted. It returns the absolute number of bytes written — including
// resumeFrom — alongside any error, which is what makes that continuation
// possible: the caller resumes from exactly where this call stopped.
func writeImage(ctx context.Context, imagePath, rawPath string, resumeFrom int64, o *flashOptions) (int64, error) {
	src, err := os.Open(imagePath)
	if err != nil {
		return resumeFrom, fmt.Errorf("opening image: %w", err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return resumeFrom, fmt.Errorf("checking image: %w", err)
	}
	total := info.Size()

	dst, err := openRawDevice(rawPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return resumeFrom, fmt.Errorf("opening %s for raw write: %w (raw disk writes need elevated privileges: run with sudo on macOS/Linux, or as Administrator on Windows)", rawPath, err)
		}
		return resumeFrom, fmt.Errorf("opening %s for raw write: %w", rawPath, err)
	}
	defer dst.Close()

	if resumeFrom > 0 {
		if _, err := src.Seek(resumeFrom, io.SeekStart); err != nil {
			return resumeFrom, fmt.Errorf("seeking image to %d: %w", resumeFrom, err)
		}
		if _, err := dst.Seek(resumeFrom, io.SeekStart); err != nil {
			return resumeFrom, fmt.Errorf("seeking %s to %d: %w", rawPath, resumeFrom, err)
		}
	}

	// Raw block devices (macOS /dev/rdiskN in particular) reject writes that
	// are not multiples of the sector size, so the final chunk is
	// zero-padded up to a sector boundary; the padding always stays within
	// the device because device sizes are themselves sector multiples.
	storage := make([]byte, flashChunkSize+sectorSize)
	buf := storage[:flashChunkSize]
	written := resumeFrom
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			end := n
			if rem := n % sectorSize; rem != 0 {
				pad := sectorSize - rem
				clear(storage[n : n+pad])
				end = n + pad
			}
			start := now()
			if _, werr := dst.Write(storage[:end]); werr != nil {
				return written, fmt.Errorf("writing to %s: %w", rawPath, werr)
			}
			if o.writeTiming != nil {
				o.writeTiming(written, end, now().Sub(start))
			}
			written += int64(n)
			if o.progress != nil {
				o.progress(written, total)
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return written, fmt.Errorf("reading image: %w", rerr)
		}
	}

	// Character-special raw devices don't support fsync (ENOTTY on macOS):
	// their writes are unbuffered, which is the point of using them.
	if err := dst.Sync(); err != nil && !errors.Is(err, syscall.ENOTTY) {
		return written, fmt.Errorf("syncing %s: %w", rawPath, err)
	}
	return written, nil
}
