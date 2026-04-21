package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScanManifestRoundTrip(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())

	// Start with an empty manifest.
	m, err := ReadScanManifest()
	if err != nil {
		t.Fatalf("read empty: %v", err)
	}
	if len(m.Files) != 0 {
		t.Fatalf("empty manifest should have zero files, got %d", len(m.Files))
	}

	// Record an entry.
	p := filepath.Join(t.TempDir(), "sample.md")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, _ := os.Stat(p)
	m.MarkIngested(p, info, "test:1")
	if err := WriteScanManifest(m); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Reload and verify.
	m2, err := ReadScanManifest()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(m2.Files) != 1 {
		t.Fatalf("expected 1 entry after reload, got %d", len(m2.Files))
	}
	if m2.Files[p].Context != "test:1" {
		t.Fatalf("context round-trip failed: %+v", m2.Files[p])
	}
}

func TestScanManifestMalformedJSONRecovers(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	// Seed the manifest dir with garbage.
	mp := ScanManifestPath()
	if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(mp, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := ReadScanManifest()
	if err != nil {
		t.Fatalf("malformed JSON should recover silently, got err %v", err)
	}
	if len(m.Files) != 0 {
		t.Fatalf("expected empty manifest after recovery, got %+v", m.Files)
	}
}

func TestScanManifestWrongVersionRecovers(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	mp := ScanManifestPath()
	if err := os.MkdirAll(filepath.Dir(mp), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"version": 999, "files": map[string]any{"x": "y"}})
	if err := os.WriteFile(mp, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := ReadScanManifest()
	if err != nil {
		t.Fatalf("wrong version should recover: %v", err)
	}
	if len(m.Files) != 0 {
		t.Fatalf("expected empty manifest after version mismatch")
	}
}

func TestScanManifestIsChangedShape(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	m := emptyManifest()
	p := filepath.Join(t.TempDir(), "f.md")
	if err := os.WriteFile(p, []byte("a"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, _ := os.Stat(p)
	if !m.IsChanged(p, info) {
		t.Fatalf("unknown path should be changed")
	}
	m.MarkIngested(p, info, "ctx")
	if m.IsChanged(p, info) {
		t.Fatalf("same stat should not be changed")
	}
}

func TestScanManifestRoots(t *testing.T) {
	m := emptyManifest()
	m.Files["/a/b/x.md"] = ScanManifestEntry{}
	m.Files["/a/b/y.md"] = ScanManifestEntry{}
	m.Files["/c/z.md"] = ScanManifestEntry{}
	roots := m.Roots()
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %v", roots)
	}
}
