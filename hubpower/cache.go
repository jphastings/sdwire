package hubpower

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
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
//
// When running as root under sudo (e.g. "sudo sdwire flash"), it resolves
// the sudo invoker's cache directory rather than root's: some sudo
// configurations leave $HOME pointed at root's home, but privileged and
// unprivileged runs must agree on where the cache lives for the
// cache-based device-revival fallback to work later, unprivileged.
func DefaultCachePath() (string, error) {
	dir := sudoInvokerCacheDir()
	if dir == "" {
		var err error
		dir, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("determining user cache directory: %w", err)
		}
	}
	return filepath.Join(dir, "sdwire", "hubports.json"), nil
}

// sudoInvokerCacheDir returns the platform cache directory of the user that
// invoked sudo, when running as root under sudo, or "" if that isn't
// applicable or can't be determined (including on Windows, which has no
// sudo).
func sudoInvokerCacheDir() string {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		return ""
	}
	sudoUID := os.Getenv("SUDO_UID")
	if sudoUID == "" {
		return ""
	}
	u, err := user.LookupId(sudoUID)
	if err != nil || u.HomeDir == "" {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(u.HomeDir, "Library", "Caches")
	}
	return filepath.Join(u.HomeDir, ".cache")
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
		if os.IsPermission(err) {
			return nil, fmt.Errorf("reading hub port cache %s: permission denied; this was likely written by an earlier privileged (sudo) run and should be removed or chown'd to the current user: %w", path, err)
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
	newDirs := missingDirComponents(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating hub port cache directory %s: %w", dir, err)
	}
	// A first-ever run under sudo would otherwise leave a root-owned cache
	// directory that blocks every later unprivileged write.
	for _, d := range newDirs {
		restoreSudoOwnership(d)
	}

	tmp, err := os.CreateTemp(dir, ".hubports-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp hub port cache file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	// os.CreateTemp uses mode 0600; loosen it so unprivileged runs can read
	// a cache file written by a privileged (sudo) run.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("setting permissions on temp hub port cache file: %w", err)
	}
	restoreSudoOwnership(tmpPath)

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

// missingDirComponents walks up from dir, collecting the path components
// that don't yet exist, stopping at the first one that does (or at the
// filesystem root). It lets Save identify which directories it is about to
// create, so their ownership can be restored after a sudo run.
func missingDirComponents(dir string) []string {
	var missing []string
	for {
		if _, err := os.Stat(dir); err == nil || !os.IsNotExist(err) {
			break
		}
		missing = append(missing, dir)

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return missing
}

// restoreSudoOwnership best-effort chowns path to the user that invoked
// sudo, if running as root under sudo. A root process invoked via sudo
// would otherwise leave files and directories it creates owned by root,
// which blocks later unprivileged runs (e.g. plain "sdwire flash") from
// reading or replacing them. It is a no-op on Windows (os.Geteuid returns
// -1 there) and whenever SUDO_UID/SUDO_GID aren't both present and valid.
func restoreSudoOwnership(path string) {
	if os.Geteuid() != 0 {
		return
	}
	uid, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil {
		return
	}
	gid, err := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err != nil {
		return
	}
	_ = os.Chown(path, uid, gid)
}
