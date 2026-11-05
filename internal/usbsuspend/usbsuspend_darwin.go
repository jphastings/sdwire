//go:build darwin

package usbsuspend

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/IOCFPlugIn.h>
#include <IOKit/usb/IOUSBLib.h>
#include <mach/mach_error.h>
#include <unistd.h>

typedef IOUSBDeviceInterface650 **sdw_dev;

static io_service_t sdw_find(uint32_t loc) {
	io_iterator_t it;
	if (IOServiceGetMatchingServices(kIOMainPortDefault, IOServiceMatching("IOUSBHostDevice"), &it) != KERN_SUCCESS)
		return 0;
	io_service_t s, found = 0;
	while (!found && (s = IOIteratorNext(it))) {
		CFTypeRef n = IORegistryEntryCreateCFProperty(s, CFSTR("locationID"), NULL, 0);
		int64_t v = -1;
		if (n) {
			if (CFGetTypeID(n) == CFNumberGetTypeID())
				CFNumberGetValue(n, kCFNumberSInt64Type, &v);
			CFRelease(n);
		}
		if (v == loc)
			found = s;
		else
			IOObjectRelease(s);
	}
	IOObjectRelease(it);
	return found;
}

static int sdw_number(CFTypeRef n) {
	int v = -1;
	if (n && CFGetTypeID(n) == CFNumberGetTypeID())
		CFNumberGetValue(n, kCFNumberIntType, &v);
	return v;
}

static int sdw_props(uint32_t loc, int *power, int *config) {
	io_service_t s = sdw_find(loc);
	if (!s)
		return 0;
	*power = -1;
	CFTypeRef pm = IORegistryEntryCreateCFProperty(s, CFSTR("IOPowerManagement"), NULL, 0);
	if (pm) {
		if (CFGetTypeID(pm) == CFDictionaryGetTypeID())
			*power = sdw_number(CFDictionaryGetValue(pm, CFSTR("CurrentPowerState")));
		CFRelease(pm);
	}
	CFTypeRef c = IORegistryEntryCreateCFProperty(s, CFSTR("kUSBCurrentConfiguration"), NULL, 0);
	*config = sdw_number(c);
	if (c)
		CFRelease(c);
	IOObjectRelease(s);
	return 1;
}

static void sdw_copy_string(io_service_t s, CFStringRef key, char *buf, int len) {
	buf[0] = 0;
	CFTypeRef v = IORegistryEntryCreateCFProperty(s, key, NULL, 0);
	if (!v)
		return;
	if (CFGetTypeID(v) != CFStringGetTypeID() || !CFStringGetCString(v, buf, len, kCFStringEncodingUTF8))
		buf[0] = 0;
	CFRelease(v);
}

static int sdw_strings(uint32_t loc, char *serial, char *product, char *vendor, int len) {
	io_service_t s = sdw_find(loc);
	if (!s)
		return 0;
	sdw_copy_string(s, CFSTR("kUSBSerialNumberString"), serial, len);
	sdw_copy_string(s, CFSTR("kUSBProductString"), product, len);
	sdw_copy_string(s, CFSTR("kUSBVendorString"), vendor, len);
	IOObjectRelease(s);
	return 1;
}

static IOReturn sdw_open(uint32_t loc, sdw_dev *dev) {
	*dev = NULL;
	io_service_t s = sdw_find(loc);
	if (!s)
		return kIOReturnNotFound;
	IOCFPlugInInterface **plug = NULL;
	SInt32 score;
	IOReturn r = IOCreatePlugInInterfaceForService(s, kIOUSBDeviceUserClientTypeID, kIOCFPlugInInterfaceID, &plug, &score);
	IOObjectRelease(s);
	if (r != kIOReturnSuccess)
		return r;
	if (!plug)
		return kIOReturnError;
	(*plug)->QueryInterface(plug, CFUUIDGetUUIDBytes(kIOUSBDeviceInterfaceID650), (LPVOID *)dev);
	(*plug)->Release(plug);
	if (!*dev)
		return kIOReturnUnsupported;
	r = (**dev)->USBDeviceOpen(*dev);
	if (r != kIOReturnSuccess)
		r = (**dev)->USBDeviceOpenSeize(*dev);
	if (r != kIOReturnSuccess) {
		(**dev)->Release(*dev);
		*dev = NULL;
	}
	return r;
}

static void sdw_close(sdw_dev d) {
	(*d)->USBDeviceClose(d);
	(*d)->Release(d);
}

// The mass-storage driver vetoes suspend (kIOReturnAborted), so the reader
// is unconfigured first, which tears that driver down; the veto can outlive
// SetConfiguration briefly while the teardown completes.
static IOReturn sdw_suspend(uint32_t loc, const char **step) {
	sdw_dev d;
	*step = "opening";
	IOReturn r = sdw_open(loc, &d);
	if (r != kIOReturnSuccess)
		return r;
	*step = "unconfiguring";
	r = (*d)->SetConfiguration(d, 0);
	if (r == kIOReturnSuccess) {
		*step = "suspending";
		for (int i = 0; i < 8; i++) {
			r = (*d)->USBDeviceSuspend(d, true);
			if (r != kIOReturnAborted)
				break;
			usleep(250000);
		}
	}
	sdw_close(d);
	return r;
}

// The reader needs time after waking to bring its card back up: a bus reset
// straight out of suspend, or half a second after resume signalling, brings
// it back reporting no medium. So it is woken first and re-enumerated (to
// reconfigure it) only after SDW_CARD_SETTLE_SECS. The resume's result is
// ignored: a reader that is already awake has nothing to resume.
#define SDW_CARD_SETTLE_SECS 3

static IOReturn sdw_resume(uint32_t loc, const char **step) {
	sdw_dev d;
	*step = "opening";
	IOReturn r = sdw_open(loc, &d);
	if (r != kIOReturnSuccess)
		return r;
	(*d)->USBDeviceSuspend(d, false);
	sleep(SDW_CARD_SETTLE_SECS);
	*step = "re-enumerating";
	r = (*d)->USBDeviceReEnumerate(d, 0);
	sdw_close(d);
	return r;
}
*/
import "C"

import (
	"fmt"

	"github.com/jphastings/sdwire/internal/blockdev"
)

// Supported reports whether this platform gives userspace USB suspend
// control.
const Supported = true

// The IOUSBHostDevice power state a USB-suspended device sits in (0 is off,
// 2 is on).
const powerStateSuspended = 1

func locationID(bus int, path []int) C.uint32_t {
	return C.uint32_t(blockdev.PackLocationID(bus, path))
}

// StateOf reads the device's state from the IORegistry.
func StateOf(bus int, path []int) (State, error) {
	var power, config C.int
	if C.sdw_props(locationID(bus, path), &power, &config) == 0 {
		return Absent, nil
	}
	switch {
	case power == powerStateSuspended:
		return Suspended, nil
	case config > 0:
		return Configured, nil
	default:
		return Unconfigured, nil
	}
}

// Strings returns the device's serial number, product and manufacturer
// strings as the IORegistry cached them at enumeration.
func Strings(bus int, path []int) (serial, product, manufacturer string, err error) {
	const size = 256
	var s, p, m [size]C.char
	if C.sdw_strings(locationID(bus, path), &s[0], &p[0], &m[0], size) == 0 {
		return "", "", "", errNoDevice(bus, path)
	}
	return C.GoString(&s[0]), C.GoString(&p[0]), C.GoString(&m[0]), nil
}

// Suspend unconfigures and USB-suspends the device. It stays suspended
// after this process exits, until something resumes it.
func Suspend(bus int, path []int) error {
	var step *C.char
	return ioError(C.sdw_suspend(locationID(bus, path), &step), step, bus, path)
}

// Resume wakes the device and re-enumerates it, which reattaches its
// drivers.
func Resume(bus int, path []int) error {
	var step *C.char
	return ioError(C.sdw_resume(locationID(bus, path), &step), step, bus, path)
}

func ioError(r C.IOReturn, step *C.char, bus int, path []int) error {
	switch r {
	case C.kIOReturnSuccess:
		return nil
	case C.kIOReturnNotFound:
		return errNoDevice(bus, path)
	}
	return fmt.Errorf("usbsuspend: %s the USB device at bus %d port path %v: %s (%#x)",
		C.GoString(step), bus, path, C.GoString(C.mach_error_string(C.mach_error_t(r))), uint32(r))
}
