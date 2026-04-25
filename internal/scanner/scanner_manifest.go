package scanner

// scanner_manifest.go ports file-manifest.ts to Go. The on-disk shape is
// compatible JSON so migration tooling (if any) can interop.
//
// Location: ~/.wuphf/wiki/.scan-manifest.json (honours WUPHF_RUNTIME_HOME).
//
// A manifest corruption recovers silently: we treat unparseable or
// wrong-version JSON as "no prior scan" and continue. Losing the manifest
// costs us one redundant re-scan, not correctness.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ScanManifestEntry mirrors the TS FileManifestEntry shape 1:1.
type ScanManifestEntry struct {
	Mtime      int64  `json:"mtime"` // nanoseconds since epoch
	Size       int64  `json:"size"`
	IngestedAt int64  `json:"ingestedAt"` // milliseconds since epoch
	Context    string `json:"context"`
}

// ScanManifest is the persistent state tracking ingested files. Version 1
// matches the TS layout; future migrations should bump and handle both.
type ScanManifest struct {
	mu         sync.Mutex
	Version    int                          `json:"version"`
	LastScanAt int64                        `json:"lastScanAt,omitempty"`
	Files      map[string]ScanManifestEntry `json:"files"`
}

const scanManifestVersion = 1

// ScanManifestPath returns the absolute path to the manifest file.
func ScanManifestPath() string {
	root := ScannerWikiRoot()
	return filepath.Join(root, ".scan-manifest.json")
}

// ReadScanManifest reads and parses the manifest from disk. Malformed or
// missing files yield a fresh empty manifest — never an error.
func ReadScanManifest() (*ScanManifest, error) {
	raw, err := os.ReadFile(ScanManifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return emptyManifest(), nil
		}
		return nil, fmt.Errorf("scan-manifest: read: %w", err)
	}
	var data ScanManifest
	if err := json.Unmarshal(raw, &data); err != nil {
		// Corruption recovery — start fresh.
		return emptyManifest(), nil
	}
	if data.Version != scanManifestVersion || data.Files == nil {
		return emptyManifest(), nil
	}
	return &data, nil
}

// WriteScanManifest persists the manifest to disk, creating the parent
// directory as needed. The file is written atomically via a tempfile rename.
func WriteScanManifest(m *ScanManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := ScanManifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("scan-manifest: mkdir: %w", err)
	}
	m.Version = scanManifestVersion
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("scan-manifest: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("scan-manifest: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("scan-manifest: rename: %w", err)
	}
	return nil
}

// IsChanged returns true when the manifest has no record of path, or when
// mtime/size have drifted from the recorded values.
func (m *ScanManifest) IsChanged(path string, info fs.FileInfo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.Files[path]
	if !ok {
		return true
	}
	return entry.Mtime != info.ModTime().UnixNano() || entry.Size != info.Size()
}

// MarkIngested records a successful ingest for path.
func (m *ScanManifest) MarkIngested(path string, info fs.FileInfo, ctx string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Files[path] = ScanManifestEntry{
		Mtime:      info.ModTime().UnixNano(),
		Size:       info.Size(),
		IngestedAt: time.Now().UnixMilli(),
		Context:    ctx,
	}
}

// MarkScanned updates the lastScanAt timestamp. Used by the freshness check.
func (m *ScanManifest) MarkScanned() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastScanAt = time.Now().UnixMilli()
}

// HasRoot returns true when any manifest entry lives under root. Callers
// use this to decide whether the human-confirmation gate applies.
func (m *ScanManifest) HasRoot(root string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := filepath.Clean(root) + string(filepath.Separator)
	for path := range m.Files {
		if path == root {
			return true
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Roots returns the deduplicated top-level scan roots recorded in the
// manifest. Useful for diagnostics.
func (m *ScanManifest) Roots() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	set := make(map[string]struct{}, len(m.Files))
	for path := range m.Files {
		dir := filepath.Dir(path)
		set[dir] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func emptyManifest() *ScanManifest {
	return &ScanManifest{
		Version: scanManifestVersion,
		Files:   make(map[string]ScanManifestEntry),
	}
}
