package blockdev

import (
	"errors"
	"testing"
	"testing/fstest"
)

var readerRef = Ref{Vendor: 0x0BDA, Product: 0x0316, Bus: 1, PortPath: []int{1, 1, 3}}

func readerDeviceFS(blockEntries ...string) fstest.MapFS {
	fsys := fstest.MapFS{
		"1-1.1.3/idVendor":  &fstest.MapFile{Data: []byte("0bda\n")},
		"1-1.1.3/idProduct": &fstest.MapFile{Data: []byte("0316\n")},
	}
	for _, name := range blockEntries {
		fsys["1-1.1.3/1-1.1.3:1.0/host5/target5:0:0/5:0:0:0/block/"+name] = &fstest.MapFile{}
	}
	return fsys
}

func TestFindLinuxFound(t *testing.T) {
	fsys := readerDeviceFS("sdb")
	name, err := findLinux(fsys, readerRef)
	if err != nil {
		t.Fatalf("findLinux: %v", err)
	}
	if name != "sdb" {
		t.Errorf("name = %q, want sdb", name)
	}
}

func TestFindLinuxFoundMMCBlk(t *testing.T) {
	fsys := readerDeviceFS("mmcblk0")
	name, err := findLinux(fsys, readerRef)
	if err != nil {
		t.Fatalf("findLinux: %v", err)
	}
	if name != "mmcblk0" {
		t.Errorf("name = %q, want mmcblk0", name)
	}
}

func TestFindLinuxAmbiguous(t *testing.T) {
	fsys := readerDeviceFS("sdb", "sdc")
	_, err := findLinux(fsys, readerRef)
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
}

func TestFindLinuxNoBlockDevice(t *testing.T) {
	fsys := readerDeviceFS()
	_, err := findLinux(fsys, readerRef)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFindLinuxWrongVendorProduct(t *testing.T) {
	fsys := fstest.MapFS{
		"1-1.1.3/idVendor":  &fstest.MapFile{Data: []byte("1234\n")},
		"1-1.1.3/idProduct": &fstest.MapFile{Data: []byte("5678\n")},
		"1-1.1.3/1-1.1.3:1.0/host5/target5:0:0/5:0:0:0/block/sdb": &fstest.MapFile{},
	}
	_, err := findLinux(fsys, readerRef)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFindLinuxDeviceMissing(t *testing.T) {
	_, err := findLinux(fstest.MapFS{}, readerRef)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSizeLinux(t *testing.T) {
	fsys := fstest.MapFS{
		"sdb/size": &fstest.MapFile{Data: []byte("31255756\n")},
	}
	size, err := sizeLinux(fsys, "sdb")
	if err != nil {
		t.Fatalf("sizeLinux: %v", err)
	}
	if want := int64(31255756 * 512); size != want {
		t.Errorf("size = %d, want %d", size, want)
	}
}

func TestIsPartitionOf(t *testing.T) {
	cases := []struct {
		disk, candidate string
		want            bool
	}{
		{"sdb", "sdb", true},
		{"sdb", "sdb1", true},
		{"sdb", "sdb12", true},
		{"sdb", "sdc1", false},
		{"sdb", "sdb1x", false},
		{"mmcblk0", "mmcblk0", true},
		{"mmcblk0", "mmcblk0p1", true},
		{"mmcblk0", "mmcblk0p", false},
		{"mmcblk0", "mmcblk01", false},
	}
	for _, c := range cases {
		if got := isPartitionOf(c.disk, c.candidate); got != c.want {
			t.Errorf("isPartitionOf(%q, %q) = %v, want %v", c.disk, c.candidate, got, c.want)
		}
	}
}

func TestMountpointsForDisk(t *testing.T) {
	mounts := []byte(
		"/dev/sda1 / ext4 rw 0 0\n" +
			"/dev/sdb1 /media/sd\\040card ext4 rw 0 0\n" +
			"/dev/sdb2 /media/sd2 ext4 rw 0 0\n" +
			"/dev/mmcblk0p1 /media/boot vfat rw 0 0\n" +
			"tmpfs /tmp tmpfs rw 0 0\n")

	got := mountpointsForDisk(mounts, "sdb")
	want := []string{"/media/sd card", "/media/sd2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("mountpointsForDisk(sdb) = %v, want %v", got, want)
	}

	got = mountpointsForDisk(mounts, "mmcblk0")
	if len(got) != 1 || got[0] != "/media/boot" {
		t.Errorf("mountpointsForDisk(mmcblk0) = %v, want [/media/boot]", got)
	}

	if got := mountpointsForDisk(mounts, "sdz"); len(got) != 0 {
		t.Errorf("mountpointsForDisk(sdz) = %v, want none", got)
	}
}

func TestUSBDeviceDirName(t *testing.T) {
	if got := usbDeviceDirName(1, []int{1, 1, 3}); got != "1-1.1.3" {
		t.Errorf("usbDeviceDirName = %q, want 1-1.1.3", got)
	}
}

func TestDiskNameFromPath(t *testing.T) {
	if got := diskNameFromPath("/dev/sdb"); got != "sdb" {
		t.Errorf("diskNameFromPath = %q, want sdb", got)
	}
}
