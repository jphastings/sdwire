package sdwire

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jphastings/sdwire/hubpower"
)

func TestResolveHubCachePathPrefersOverride(t *testing.T) {
	o := defaultOptions()
	o.hubCachePath = "/custom/path/hubports.json"

	got, err := resolveHubCachePath(o)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != o.hubCachePath {
		t.Errorf("resolveHubCachePath() = %q, want override %q", got, o.hubCachePath)
	}
}

func TestResolveHubCachePathFallsBackToDefault(t *testing.T) {
	o := defaultOptions()

	got, err := resolveHubCachePath(o)
	if err != nil {
		t.Skipf("no user cache dir available: %v", err)
	}
	want, err := hubpower.DefaultCachePath()
	if err != nil {
		t.Skipf("no user cache dir available: %v", err)
	}
	if got != want {
		t.Errorf("resolveHubCachePath() = %q, want %q", got, want)
	}
}

func TestCacheEntryDeviceInfo(t *testing.T) {
	ref := &hubpower.PortRef{Bus: 2, HubPath: []int{1, 4}, Port: 3}

	info := cacheEntryDeviceInfo("20120501030900000.1.4.3", ref)

	if info.Serial != "20120501030900000" {
		t.Errorf("Serial = %q, want %q", info.Serial, "20120501030900000")
	}
	if info.Bus != 2 {
		t.Errorf("Bus = %d, want 2", info.Bus)
	}
	if len(info.PortPath) != 3 || info.PortPath[0] != 1 || info.PortPath[1] != 4 || info.PortPath[2] != 3 {
		t.Errorf("PortPath = %v, want [1 4 3]", info.PortPath)
	}
	if info.Generation != GenerationSDWire3 {
		t.Errorf("Generation = %v, want GenerationSDWire3", info.Generation)
	}
}

func TestCacheEntryDeviceInfoFallsBackToRefPathWithoutSuffix(t *testing.T) {
	ref := &hubpower.PortRef{Bus: 2, HubPath: []int{1, 4}, Port: 3}

	info := cacheEntryDeviceInfo("bare-serial-no-suffix", ref)

	if info.Serial != "bare-serial-no-suffix" {
		t.Errorf("Serial = %q, want the whole key", info.Serial)
	}
	if len(info.PortPath) != 3 || info.PortPath[0] != 1 || info.PortPath[1] != 4 || info.PortPath[2] != 3 {
		t.Errorf("PortPath = %v, want ref-derived [1 4 3]", info.PortPath)
	}
}

func TestStatusToMode(t *testing.T) {
	cases := []struct {
		name string
		st   hubpower.PortStatus
		want SwitchMode
	}{
		{"unpowered means target", hubpower.PortStatus{Powered: false, Connected: false}, ModeTarget},
		{"unpowered but connected bit set still means target", hubpower.PortStatus{Powered: false, Connected: true}, ModeTarget},
		{"powered and connected means host", hubpower.PortStatus{Powered: true, Connected: true}, ModeHost},
		{"powered but not yet connected is honestly unknown", hubpower.PortStatus{Powered: true, Connected: false}, ModeUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := statusToMode(c.st); got != c.want {
				t.Errorf("statusToMode(%+v) = %v, want %v", c.st, got, c.want)
			}
		})
	}
}

func TestSelectCacheEntry(t *testing.T) {
	twoEntries := map[string]*hubpower.PortRef{
		"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3},
		"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4},
	}

	t.Run("empty selector requires exactly one entry", func(t *testing.T) {
		sole := map[string]*hubpower.PortRef{"solo.1.2.3": {Bus: 1, HubPath: []int{1, 2}, Port: 3}}
		key, ref, err := selectCacheEntry(sole, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "solo.1.2.3" || ref == nil {
			t.Errorf("got key=%q ref=%v", key, ref)
		}
	})

	t.Run("empty selector with multiple entries is ambiguous", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "20120501030900000.1.1.3") || !strings.Contains(err.Error(), "20120501030900000.1.1.4") {
			t.Errorf("error should list both candidate identities: %v", err)
		}
	})

	t.Run("suffixed identity selector matches", func(t *testing.T) {
		key, _, err := selectCacheEntry(twoEntries, "20120501030900000.1.1.4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.4" {
			t.Errorf("key = %q", key)
		}
	})

	t.Run("location selector matches", func(t *testing.T) {
		key, _, err := selectCacheEntry(twoEntries, "1-1.1.3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.3" {
			t.Errorf("key = %q", key)
		}
	})

	t.Run("bare numeric serial falls back from identity's location misparse to a serial match", func(t *testing.T) {
		// A bare numeric serial parses as a (bogus, empty-path) bus number
		// under selectByIdentity's location-form rules, so the real match
		// only comes from the selectBySerial fallback — the same reason
		// the CLI's connectSelector avoids routing bare serials through
		// NewWithIdentity at all.
		sole := map[string]*hubpower.PortRef{"20120501030900000.1.2.3": {Bus: 1, HubPath: []int{1, 2}, Port: 3}}
		key, _, err := selectCacheEntry(sole, "20120501030900000")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.2.3" {
			t.Errorf("key = %q", key)
		}
	})

	t.Run("no match returns an error", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "nope")
		if err == nil {
			t.Fatal("expected an error")
		}
	})
}

// writeFakeHubCache saves a cache file containing entries at a temp path
// and returns the path. The PortRefs never correspond to real hardware, so
// any attempt to actually open one of them is expected to fail — that's
// fine for exercising CachedPortState's cache-loading and selection logic
// without touching real USB.
func writeFakeHubCache(t *testing.T, entries map[string]*hubpower.PortRef) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hubports.json")
	cache, err := hubpower.LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	for key, ref := range entries {
		cache.Put(key, ref)
	}
	if err := cache.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

func TestCachedPortStateEmptyCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hubports.json")

	mode, identity, err := CachedPortState("", WithHubCachePath(path))
	if err == nil {
		t.Fatal("expected an error for an empty cache")
	}
	if mode != ModeUnknown || identity != "" {
		t.Errorf("mode=%v identity=%q, want ModeUnknown and empty identity", mode, identity)
	}
}

func TestCachedPortStateAmbiguousWithoutSelector(t *testing.T) {
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"a.1.1.1": {Bus: 1, HubPath: []int{1}, Port: 1},
		"b.1.1.2": {Bus: 1, HubPath: []int{1}, Port: 2},
	})

	mode, identity, err := CachedPortState("", WithHubCachePath(path))
	if err == nil {
		t.Fatal("expected an error listing candidates")
	}
	if mode != ModeUnknown || identity != "" {
		t.Errorf("mode=%v identity=%q, want ModeUnknown and empty identity", mode, identity)
	}
}

func TestCachedPortStateNoMatch(t *testing.T) {
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"a.1.1.1": {Bus: 1, HubPath: []int{1}, Port: 1},
	})

	_, _, err := CachedPortState("nope", WithHubCachePath(path))
	if err == nil {
		t.Fatal("expected a no-match error")
	}
}

// TestCachedPortStateMatchedEntryAttemptsHubOpen proves CachedPortState
// wires cache-loading and selection through to an actual (real, non-mocked)
// hub open attempt: the cache entry here doesn't correspond to a real
// hub, so the open is expected to fail, but the returned identity must
// still be the matched entry's — confirming selection ran before the
// (expectedly failing) hardware access.
func TestCachedPortStateMatchedEntryAttemptsHubOpen(t *testing.T) {
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"20120501030900000.1.1.3": {Bus: 250, HubPath: []int{1}, Port: 3},
	})

	mode, identity, err := CachedPortState("", WithHubCachePath(path))
	if err == nil {
		t.Fatal("expected a hub-open error against a non-existent hub")
	}
	if identity != "20120501030900000.1.1.3" {
		t.Errorf("identity = %q, want the matched cache entry's identity", identity)
	}
	if mode != ModeUnknown {
		t.Errorf("mode = %v, want ModeUnknown on error", mode)
	}
}
