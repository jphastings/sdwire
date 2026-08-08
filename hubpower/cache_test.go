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
