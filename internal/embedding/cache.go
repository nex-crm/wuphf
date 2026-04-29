package embedding

// cache.go is a JSONL append-only cache for embeddings. Keyed by SHA-256
// of the input text + provider name so a model swap invalidates rows
// without manual cleanup.
//
// The cache is intentionally "dumb": load on first miss, in-memory map
// for hits, append the new row to disk for misses. Size is capped at
// maxCacheBytes — once exceeded we rotate the file in place (same
// strategy as a compact log: copy to a .old suffix, start fresh). This
// keeps the cache scaling predictable without pulling in BoltDB / LMDB.
//
// File layout (one JSON object per line):
//
//	{"sha256":"<hex>","model":"openai-text-embedding-3-small","vector":[...]}
//
// Concurrency: a single sync.RWMutex guards the map + file handle. The
// underlying append is sync.OnceFunc-style: each Set takes the write
// lock, writes the row, releases. We don't use os.O_APPEND because we
// also want the size-based rotation to happen atomically with the
// in-memory map state.
//
// IMPORTANT: this cache is goroutine-safe but **single-process only**.
// Two `wuphf` processes pointed at the same JSONL will race the rotation
// path (rename + truncate vs. open-append) and may lose rows. If you
// need shared state across processes, run a single broker and have
// other tools talk to it; do not point a second wuphf at the same
// WUPHF_EMBEDDING_CACHE_PATH.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nex-crm/wuphf/internal/config"
)

// maxCacheBytes is the rotation threshold. 10MiB ≈ 5k cached entries at
// the OpenAI text-embedding-3-small dimension (1536 floats ≈ 1.5KB each
// after JSON encoding overhead).
const maxCacheBytes = 10 * 1024 * 1024

// cacheRow is the JSONL record shape. Reused for both reads and writes.
type cacheRow struct {
	SHA256 string    `json:"sha256"`
	Model  string    `json:"model"`
	Vector []float32 `json:"vector"`
}

// Cache is the JSONL-backed embedding cache. Construct via NewCache or
// the package-level DefaultCache singleton. Safe for concurrent use.
type Cache struct {
	path string

	mu          sync.RWMutex
	loaded      bool
	rows        map[string][]float32 // key: sha256 + "@" + model
	bytesOnDisk int64
}

// NewCache constructs a cache at the given path. The file is created on
// first Set call — passing a path that does not yet exist is fine.
//
// If path is empty, NewCache returns a no-op cache that always misses.
// This lets the broker disable persistence (e.g. in tests, or when we're
// running with WUPHF_EMBEDDING_CACHE=disabled).
func NewCache(path string) *Cache {
	return &Cache{
		path: path,
		rows: map[string][]float32{},
	}
}

// DefaultCachePath resolves $WUPHF_HOME/.wuphf/cache/embeddings.jsonl.
// Honours WUPHF_EMBEDDING_CACHE_PATH for full override and
// WUPHF_EMBEDDING_CACHE=disabled to opt out entirely.
func DefaultCachePath() string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_CACHE")), "disabled") {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("WUPHF_EMBEDDING_CACHE_PATH")); v != "" {
		return v
	}
	home := config.RuntimeHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "cache", "embeddings.jsonl")
}

// HashText computes the SHA-256 of text, hex-encoded. Exposed so tests
// can construct expected cache keys without re-implementing the hash.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Get returns (vector, true) on hit, (nil, false) on miss. Triggers a
// one-time on-disk load so callers don't need to call Load explicitly.
// A nil receiver returns a miss — convenient for "cache is optional"
// call sites.
func (c *Cache) Get(text, model string) ([]float32, bool) {
	if c == nil || c.path == "" {
		return nil, false
	}
	if err := c.ensureLoaded(); err != nil {
		// Load failures are silently treated as a cold cache. The next
		// Set will overwrite the corrupt file (rotation happens once we
		// exceed the size cap).
		return nil, false
	}
	key := cacheKey(text, model)
	c.mu.RLock()
	v, ok := c.rows[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	out := make([]float32, len(v))
	copy(out, v)
	return out, true
}

// Set stores vector for (text, model) and appends a JSONL row to disk.
// If the cache file exceeds maxCacheBytes after the append, we rotate
// in place: the existing file becomes <path>.old and a fresh file is
// started with the current row as its first line.
//
// Errors are returned but never block the caller — the embedding
// pipeline must keep working even when the cache file is unwritable.
func (c *Cache) Set(text, model string, vector []float32) error {
	if c == nil || c.path == "" {
		return nil
	}
	if len(vector) == 0 {
		return errors.New("embedding: cache: empty vector")
	}
	// Best-effort load: a failure leaves the in-memory map empty, which
	// is fine for a Set call (we still write the row to disk). When the
	// load fails we still need bytesOnDisk to reflect the actual file
	// size — otherwise the rotation check below thinks the file is
	// empty and the cache grows unbounded on disk while in-memory state
	// stays cold. Stat the file independently so rotation triggers
	// regardless of whether the JSONL parse succeeded.
	if loadErr := c.ensureLoaded(); loadErr != nil {
		if info, statErr := os.Stat(c.path); statErr == nil {
			c.mu.Lock()
			if c.bytesOnDisk == 0 {
				c.bytesOnDisk = info.Size()
			}
			c.mu.Unlock()
		}
	}

	key := cacheKey(text, model)
	row := cacheRow{
		SHA256: HashText(text),
		Model:  model,
		Vector: vector,
	}
	raw, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("embedding: cache: marshal: %w", err)
	}
	raw = append(raw, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("embedding: cache: mkdir: %w", err)
	}

	// Rotation: if the next append would push us over the cap, rename
	// the current file to .old and start fresh. We rotate BEFORE the
	// write so the new row always lands in a non-truncated file.
	if c.bytesOnDisk+int64(len(raw)) > maxCacheBytes {
		_ = os.Remove(c.path + ".old")
		if err := os.Rename(c.path, c.path+".old"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("embedding: cache: rotate: %w", err)
		}
		c.bytesOnDisk = 0
		c.rows = map[string][]float32{}
	}

	f, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("embedding: cache: open: %w", err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return fmt.Errorf("embedding: cache: write: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("embedding: cache: close: %w", err)
	}

	c.bytesOnDisk += int64(len(raw))
	stored := make([]float32, len(vector))
	copy(stored, vector)
	c.rows[key] = stored
	return nil
}

// ensureLoaded reads the cache file once and populates the in-memory
// map. Subsequent calls are no-ops. Sequential malformed rows are
// skipped so a single corrupt line doesn't disable the whole cache.
func (c *Cache) ensureLoaded() error {
	c.mu.RLock()
	if c.loaded {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return nil
	}
	c.loaded = true

	f, err := os.Open(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("embedding: cache: open: %w", err)
	}
	defer func() { _ = f.Close() }()

	if info, statErr := f.Stat(); statErr == nil {
		c.bytesOnDisk = info.Size()
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row cacheRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.SHA256 == "" || row.Model == "" || len(row.Vector) == 0 {
			continue
		}
		c.rows[row.SHA256+"@"+row.Model] = row.Vector
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("embedding: cache: read: %w", err)
	}
	return nil
}

// cacheKey is the in-memory map key. We keep model in the key so that
// switching models invalidates the in-memory rows; on disk every row
// already records its model so a re-load filters correctly even after
// an env change.
func cacheKey(text, model string) string {
	return HashText(text) + "@" + model
}

// Stats are read-only counters useful for telemetry. Not used in the
// current code path but expose enough state for future "cache usage"
// dashboards without leaking the rows map.
type Stats struct {
	Entries     int
	BytesOnDisk int64
	Path        string
}

// Stats returns a snapshot. Concurrent callers see consistent counters
// (we hold the read lock for the duration of the copy).
func (c *Cache) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Stats{
		Entries:     len(c.rows),
		BytesOnDisk: c.bytesOnDisk,
		Path:        c.path,
	}
}
