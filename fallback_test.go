package sdwire

import (
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
