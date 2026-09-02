package sdwire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/gousb"
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
	const cachePath = "/cache/hubports.json"

	twoEntries := map[string]*hubpower.PortRef{
		"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3},
		"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4},
	}

	t.Run("empty selector requires exactly one entry", func(t *testing.T) {
		sole := map[string]*hubpower.PortRef{"solo.1.2.3": {Bus: 1, HubPath: []int{1, 2}, Port: 3}}
		key, ref, err := selectCacheEntry(sole, "", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "solo.1.2.3" || ref == nil {
			t.Errorf("got key=%q ref=%v", key, ref)
		}
	})

	t.Run("empty selector with multiple entries is ambiguous", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "", cachePath)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "20120501030900000.1.1.3") || !strings.Contains(err.Error(), "20120501030900000.1.1.4") {
			t.Errorf("error should list both candidate identities: %v", err)
		}
	})

	t.Run("suffixed identity selector matches", func(t *testing.T) {
		key, _, err := selectCacheEntry(twoEntries, "20120501030900000.1.1.4", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.4" {
			t.Errorf("key = %q", key)
		}
	})

	t.Run("location selector matches", func(t *testing.T) {
		key, _, err := selectCacheEntry(twoEntries, "1-1.1.3", cachePath)
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
		key, _, err := selectCacheEntry(sole, "20120501030900000", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.2.3" {
			t.Errorf("key = %q", key)
		}
	})

	t.Run("no match returns an error", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "nope", cachePath)
		if err == nil {
			t.Fatal("expected an error")
		}
	})

	// A serial matching several remembered ports is what a device moved
	// between sockets leaves behind. Reporting the identity lookup's "no
	// device matching location <serial>" instead hides both the real
	// problem and its fix.
	t.Run("a serial matching several ports reports the ambiguity, not a miss", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "20120501030900000", cachePath)
		if err == nil {
			t.Fatal("expected an error")
		}
		for _, want := range []string{"20120501030900000.1.1.3", "20120501030900000.1.1.4", cachePath} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should mention %q: %v", want, err)
			}
		}
		if strings.Contains(err.Error(), "matching location") {
			t.Errorf("error should not be the identity lookup's location miss: %v", err)
		}
	})

	t.Run("a serial matching several ports picks the one most recently seen", func(t *testing.T) {
		now := time.Now()
		entries := map[string]*hubpower.PortRef{
			"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3, LastSeen: now.Add(-time.Hour)},
			"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4, LastSeen: now},
		}
		key, _, err := selectCacheEntry(entries, "20120501030900000", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.4" {
			t.Errorf("key = %q, want the most recently seen entry", key)
		}
	})

	t.Run("a serial matching several ports prefers a recorded sighting over none", func(t *testing.T) {
		now := time.Now()
		zeroThenSeen := map[string]*hubpower.PortRef{
			"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3},
			"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4, LastSeen: now},
		}
		key, _, err := selectCacheEntry(zeroThenSeen, "20120501030900000", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.4" {
			t.Errorf("key = %q, want the entry with a recorded sighting", key)
		}

		seenThenZero := map[string]*hubpower.PortRef{
			"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3, LastSeen: now},
			"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4},
		}
		key, _, err = selectCacheEntry(seenThenZero, "20120501030900000", cachePath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if key != "20120501030900000.1.1.3" {
			t.Errorf("key = %q, want the entry with a recorded sighting", key)
		}
	})

	t.Run("a serial matching several ports with equal last-seen stays ambiguous", func(t *testing.T) {
		now := time.Now()
		entries := map[string]*hubpower.PortRef{
			"20120501030900000.1.1.3": {Bus: 1, HubPath: []int{1, 1}, Port: 3, LastSeen: now},
			"20120501030900000.1.1.4": {Bus: 1, HubPath: []int{1, 1}, Port: 4, LastSeen: now},
		}
		_, _, err := selectCacheEntry(entries, "20120501030900000", cachePath)
		if err == nil {
			t.Fatal("expected an error")
		}
		for _, want := range []string{"20120501030900000.1.1.3", "20120501030900000.1.1.4", cachePath} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should mention %q: %v", want, err)
			}
		}
	})

	t.Run("a bare numeric serial that matches nothing names the serial, not a location", func(t *testing.T) {
		_, _, err := selectCacheEntry(twoEntries, "999", cachePath)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), `serial "999"`) {
			t.Errorf("error should name the serial: %v", err)
		}
		if strings.Contains(err.Error(), "location") {
			t.Errorf("error should not be the location miss: %v", err)
		}
	})
}

func TestRevivePortPath(t *testing.T) {
	cases := []struct {
		selector string
		wantOK   bool
	}{
		{"1-1.1.3", true},
		{"2-4", true},
		// All digits: parses as a bus number with no port path, which names
		// no port to power-cycle. Must go to the cache instead.
		{"20120501030900000", false},
		{"20120501030900000.1.1.3", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.selector, func(t *testing.T) {
			if _, _, ok := revivePortPath(c.selector); ok != c.wantOK {
				t.Errorf("revivePortPath(%q) ok = %v, want %v", c.selector, ok, c.wantOK)
			}
		})
	}
}

// writeFakeHubCache saves a cache file containing entries at a temp path and
// returns the path. It writes the map straight to JSON rather than going
// through Cache.Put, which stamps LastSeen with time.Now() in
// map-iteration order — tests need to control LastSeen explicitly. The
// PortRefs never correspond to real hardware, so any attempt to actually
// open one of them is expected to fail — that's fine for exercising
// CachedPortState's cache-loading and selection logic without touching real
// USB.
func writeFakeHubCache(t *testing.T, entries map[string]*hubpower.PortRef) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hubports.json")
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
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

// TestCachedPortStatePrefersMostRecentlySeenPort mirrors
// TestCachedPortStateMatchedEntryAttemptsHubOpen but with two cache entries
// for the same serial: CachedPortState must resolve the ambiguity to the
// more recently seen one rather than erroring.
func TestCachedPortStatePrefersMostRecentlySeenPort(t *testing.T) {
	now := time.Now()
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"20120501030900000.1.1.3": {Bus: 250, HubPath: []int{1}, Port: 3, LastSeen: now.Add(-time.Hour)},
		"20120501030900000.1.1.4": {Bus: 250, HubPath: []int{1}, Port: 4, LastSeen: now},
	})

	_, identity, err := CachedPortState("20120501030900000", WithHubCachePath(path))
	if err == nil {
		t.Fatal("expected a hub-open error against a non-existent hub")
	}
	if identity != "20120501030900000.1.1.4" {
		t.Errorf("identity = %q, want the most recently seen entry", identity)
	}
}

// TestTryCacheFallbackTriesMostRecentlySeenFirst proves tryCacheFallback
// offers matching entries to the caller in recency order rather than
// cache/map order, by recording the order matches is called with.
func TestTryCacheFallbackTriesMostRecentlySeenFirst(t *testing.T) {
	now := time.Now()
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"20120501030900000.1.1.2": {Bus: 250, HubPath: []int{1}, Port: 2},
		"20120501030900000.1.1.3": {Bus: 250, HubPath: []int{1}, Port: 3, LastSeen: now.Add(-time.Hour)},
		"20120501030900000.1.1.4": {Bus: 250, HubPath: []int{1}, Port: 4, LastSeen: now},
	})

	ctx := gousb.NewContext()
	defer ctx.Close()
	o := defaultOptions()
	o.hubCachePath = path

	var tried []string
	matches := func(key string, ref *hubpower.PortRef) bool {
		tried = append(tried, key)
		return true
	}

	_, _, err := tryCacheFallback(ctx, o, matches)
	if err == nil {
		t.Fatal("expected an error against non-existent hubs")
	}

	want := []string{
		"20120501030900000.1.1.4",
		"20120501030900000.1.1.3",
		"20120501030900000.1.1.2",
	}
	if len(tried) != len(want) {
		t.Fatalf("tried %v, want %v", tried, want)
	}
	for i, k := range want {
		if tried[i] != k {
			t.Errorf("tried[%d] = %q, want %q", i, tried[i], k)
		}
	}
}

// TestListDeviceStatesWarnsOnceForUnreachablePorts proves ListDeviceStates
// warns once (not once per entry) when cached hub ports can't be read, and
// omits those entries from the returned states rather than reporting them
// with a guessed mode.
func TestListDeviceStatesWarnsOnceForUnreachablePorts(t *testing.T) {
	path := writeFakeHubCache(t, map[string]*hubpower.PortRef{
		"20120501030900000.1.1.3": {Bus: 250, HubPath: []int{1}, Port: 3},
		"20120501030900000.1.1.4": {Bus: 250, HubPath: []int{1}, Port: 4},
	})

	var warnings []string
	states, err := ListDeviceStates(WithHubCachePath(path), WithWarningHandler(func(msg string) {
		warnings = append(warnings, msg)
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "2 remembered hub port(s)") || !strings.Contains(warnings[0], path) {
		t.Errorf("warning = %q, want it to mention the count and %q", warnings[0], path)
	}
	for _, st := range states {
		if st.Info.Identity() == "20120501030900000.1.1.3" || st.Info.Identity() == "20120501030900000.1.1.4" {
			t.Errorf("states should not include the unreachable fake entries, got %+v", st)
		}
	}
}

func TestEntriesByRecency(t *testing.T) {
	now := time.Now()
	entries := map[string]*hubpower.PortRef{
		"newest": {LastSeen: now},
		"older":  {LastSeen: now.Add(-time.Hour)},
		"zero":   {},
		"tie-b":  {LastSeen: now.Add(-2 * time.Hour)},
		"tie-a":  {LastSeen: now.Add(-2 * time.Hour)},
	}

	got := entriesByRecency(entries)

	want := []string{"newest", "older", "tie-a", "tie-b", "zero"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i, k := range want {
		if got[i].key != k {
			t.Errorf("entry %d = %q, want %q", i, got[i].key, k)
		}
	}
}
