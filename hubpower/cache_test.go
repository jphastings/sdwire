package hubpower

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadCacheMissingFileIsEmpty(t *testing.T) {
	c, err := LoadCache(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.Get("anything"); got != nil {
		t.Errorf("Get on empty cache = %+v, want nil", got)
	}
}

func TestCacheSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	ref := &PortRef{Bus: 1, HubPath: []int{1, 2}, Port: 3, HubVendor: 0x0451, HubProduct: 0x8025}
	c.Put("device-key", ref)
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got := loaded.Get("device-key"); got == nil || !reflect.DeepEqual(got, ref) {
		t.Fatalf("Get(device-key) = %+v, want %+v", got, ref)
	}
}

func TestCachePutEvictsOtherKeyAtSamePort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	// A cache-by-serial entry written before the device's serial could be
	// read, superseded once it's known — the "unknown.1.1.3" /
	// "20120501030900000.1.1.3" pair that made a bench's location lookup
	// ambiguous.
	c.Put("unknown.1.1.3", &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 3})
	c.Put("20120501030900000.1.1.3", &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 3})

	if got := c.Get("unknown.1.1.3"); got != nil {
		t.Errorf("Get(unknown.1.1.3) = %+v, want nil (evicted by a later Put at the same port)", got)
	}
	if got := c.Get("20120501030900000.1.1.3"); got == nil {
		t.Error("Get(20120501030900000.1.1.3) = nil, want the entry just Put")
	}
}

func TestCachePutKeepsEntriesForOtherPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	c.Put("device-a", &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 3})
	c.Put("device-b", &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 4})

	if got := c.Get("device-a"); got == nil {
		t.Error("Get(device-a) = nil, want the entry retained (different port than device-b)")
	}
	if got := c.Get("device-b"); got == nil {
		t.Error("Get(device-b) = nil, want the entry just Put")
	}
}

func TestCachePutOverwritesSameKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	c.Put("device-key", &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 3})
	updated := &PortRef{Bus: 1, HubPath: []int{1, 1}, Port: 4}
	c.Put("device-key", updated)

	if got := c.Get("device-key"); got == nil || !reflect.DeepEqual(got, updated) {
		t.Errorf("Get(device-key) = %+v, want %+v", got, updated)
	}
}

func TestCacheDeleteRemovesEntryAcrossSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	c.Put("stale", &PortRef{Bus: 1, Port: 1})
	c.Delete("stale")
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if got := loaded.Get("stale"); got != nil {
		t.Errorf("Get(stale) after delete = %+v, want nil", got)
	}
}

func TestCacheSaveLeavesFileOtherReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	c, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	c.Put("device-key", &PortRef{Bus: 1, Port: 1})
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o004 == 0 {
		t.Errorf("Save left mode %o, want other-read bit set so unprivileged runs after a sudo run can read it", info.Mode().Perm())
	}
}

func TestLoadCachePermissionDeniedIsActionable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits don't restrict access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file, so this can't be exercised as root")
	}

	path := filepath.Join(t.TempDir(), "hubports.json")
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LoadCache(path)
	if err == nil {
		t.Fatal("LoadCache on a 0000 file: got nil error, want permission error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention path %q", err.Error(), path)
	}
	if !strings.Contains(err.Error(), "privileged") {
		t.Errorf("error %q does not give the actionable sudo hint", err.Error())
	}
}

func TestDefaultCachePath(t *testing.T) {
	path, err := DefaultCachePath()
	if err != nil {
		t.Skipf("no user cache dir available: %v", err)
	}
	want := filepath.Join("sdwire", "hubports.json")
	if !strings.HasSuffix(path, want) {
		t.Errorf("DefaultCachePath() = %q, want suffix %q", path, want)
	}
}
