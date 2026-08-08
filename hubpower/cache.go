package hubpower

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Cache persists PortRef entries on disk, keyed by a caller-supplied device
// identity string, so a device's hub and port can still be found (and
// re-powered) after it has been switched off and dropped from USB
// enumeration.
type Cache struct {
	mu      sync.Mutex
	entries map[string]*PortRef
}

// DefaultCachePath returns the default on-disk location for the hub port
// cache: "<user cache dir>/sdwire/hubports.json".
func DefaultCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("determining user cache directory: %w", err)
	}
	return filepath.Join(dir, "sdwire", "hubports.json"), nil
}

// LoadCache reads the cache from path. A missing file is treated as an
// empty cache, not an error.
func LoadCache(path string) (*Cache, error) {
	c := &Cache{entries: make(map[string]*PortRef)}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("reading hub port cache %s: %w", path, err)
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		return nil, fmt.Errorf("parsing hub port cache %s: %w", path, err)
	}
	if c.entries == nil {
		c.entries = make(map[string]*PortRef)
	}
	return c, nil
}

// Get returns the cached PortRef for key, or nil if there is none.
func (c *Cache) Get(key string) *PortRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[key]
}

// Put records ref as the PortRef for key.
func (c *Cache) Put(key string, ref *PortRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = ref
}

// Delete removes any cached PortRef for key.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// All returns a copy of every cached entry, keyed by the identity it was
// cached under. Not part of the originally specified API surface, but
// needed by callers (such as sdwire's powered-off-device fallback) that
// must scan the cache for entries matching a query.
func (c *Cache) All() map[string]*PortRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]*PortRef, len(c.entries))
	for k, v := range c.entries {
		ref := *v
		out[k] = &ref
	}
	return out
}

// Save writes the cache to path, creating its parent directory if needed.
// It writes to a temp file in the same directory and renames it into
// place, so readers never observe a partially written file.
func (c *Cache) Save(path string) error {
	c.mu.Lock()
	data, err := json.MarshalIndent(c.entries, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("encoding hub port cache: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating hub port cache directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".hubports-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp hub port cache file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing hub port cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing hub port cache temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming hub port cache into place: %w", err)
	}
	return nil
}
