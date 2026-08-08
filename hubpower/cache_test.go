package hubpower

import (
	"path/filepath"
	"reflect"
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
