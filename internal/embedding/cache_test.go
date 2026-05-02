package embedding

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(filepath.Join(dir, "embeddings.jsonl"))

	want := []float32{0.1, 0.2, 0.3, 0.4}
	if err := c.Set("hello", "test-model", want); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, ok := c.Get("hello", "test-model")
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i, x := range want {
		if got[i] != x {
			t.Errorf("at %d: got %v want %v", i, got[i], x)
		}
	}
}

func TestCache_PersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.jsonl")
	c1 := NewCache(path)
	want := []float32{1, 2, 3}
	if err := c1.Set("durable", "m", want); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Fresh instance — should re-read the file.
	c2 := NewCache(path)
	got, ok := c2.Get("durable", "m")
	if !ok {
		t.Fatal("expected hit on fresh instance")
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
}

func TestCache_ModelKeyedSeparately(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "embeddings.jsonl"))
	if err := c.Set("text", "model-a", []float32{1}); err != nil {
		t.Fatalf("set a: %v", err)
	}
	if err := c.Set("text", "model-b", []float32{2}); err != nil {
		t.Fatalf("set b: %v", err)
	}

	a, okA := c.Get("text", "model-a")
	b, okB := c.Get("text", "model-b")
	if !okA || !okB {
		t.Fatalf("expected both models cached, got %v / %v", okA, okB)
	}
	if a[0] == b[0] {
		t.Error("expected different vectors per model")
	}
}

func TestCache_DisabledPathIsNoOp(t *testing.T) {
	c := NewCache("")
	if err := c.Set("anything", "any-model", []float32{1, 2}); err != nil {
		t.Errorf("set on disabled cache should be a no-op, got %v", err)
	}
	if _, ok := c.Get("anything", "any-model"); ok {
		t.Error("disabled cache should always miss")
	}
}

func TestCache_MissOnEmptyFile(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "embeddings.jsonl"))
	if _, ok := c.Get("nope", "m"); ok {
		t.Error("expected miss on empty file")
	}
}

func TestCache_RotatesOverSizeCap(t *testing.T) {
	// Write a row, then cheat the size accounting so the next Set
	// triggers rotation. Keeps the test fast — exercising the real
	// 10MiB threshold would take megabytes of inputs.
	c := NewCache(filepath.Join(t.TempDir(), "embeddings.jsonl"))
	if err := c.Set("first", "m", []float32{1}); err != nil {
		t.Fatalf("first: %v", err)
	}
	c.mu.Lock()
	c.bytesOnDisk = maxCacheBytes - 1
	c.mu.Unlock()

	if err := c.Set("second", "m", []float32{2}); err != nil {
		t.Fatalf("second: %v", err)
	}

	if _, ok := c.Get("first", "m"); ok {
		t.Error("after rotation, old rows should be gone")
	}
	if _, ok := c.Get("second", "m"); !ok {
		t.Error("after rotation, new row should be present")
	}

	// .old should now exist.
	matches, _ := filepath.Glob(c.path + ".old")
	if len(matches) != 1 {
		t.Errorf("expected 1 .old file, got %d", len(matches))
	}
}

// TestCache_RotatesEvenWhenLoadFails pins the regression: if
// ensureLoaded errors at os.Open (permission failure mid-flight, racy
// remove, etc.), bytesOnDisk would stay at 0 — the rotation check
// thinks the file is empty and the cache grows unbounded on disk
// while the in-memory map stays cold.
//
// Earlier this test used a corrupt-but-readable JSONL file to simulate
// the failure, which didn't actually exercise the new fix code:
// ensureLoaded does f.Stat() BEFORE the JSONL scan, so bytesOnDisk
// gets set from Stat regardless of scanner errors. The real fix path
// is only hit when os.Open itself fails — we now simulate that with
// chmod 000.
func TestCache_RotatesEvenWhenLoadFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based unreadable-file simulation is not portable to windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 doesn't block reads")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.jsonl")

	garbage := bytes.Repeat([]byte{'x'}, maxCacheBytes-100)
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Make the file unreadable so os.Open returns EACCES inside
	// ensureLoaded — this is the path the production fix actually
	// covers (Stat itself still works on 0o000 files because it only
	// needs metadata access from the parent directory).
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	c := NewCache(path)

	// Drive a Set against the chmod-000 file. With the bug,
	// ensureLoaded errors at os.Open and bytesOnDisk stays 0 →
	// rotation never fires → the .old file is never created and the
	// live file grows unbounded. With the fix, Set's post-load Stat
	// fallback fills bytesOnDisk from the filesystem (Stat itself
	// still works on a 0o000 file because it only needs metadata
	// access through the parent dir), so the cap check trips and
	// rotation runs.
	//
	// os.Rename and os.OpenFile(O_CREATE|O_WRONLY) both succeed
	// because they need write+exec on the PARENT directory, not on
	// the (about-to-be-removed) 0o000 file. The kernel never tries
	// to read the contents during rename/create.
	if err := c.Set("trigger", "m", []float32{1, 2, 3}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// .old must exist — proves rotation triggered despite the
	// initial load failure.
	if _, err := os.Stat(path + ".old"); err != nil {
		t.Fatalf("expected .old to exist after rotation, got %v", err)
	}
	// And the live file should be small (just the new row).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat live: %v", err)
	}
	if info.Size() >= int64(maxCacheBytes-100) {
		t.Errorf("live file should have been rotated, got size=%d", info.Size())
	}
}

func TestCache_HashTextStable(t *testing.T) {
	a := HashText("hello")
	b := HashText("hello")
	if a != b {
		t.Errorf("HashText should be deterministic: got %q vs %q", a, b)
	}
	if HashText("hello") == HashText("world") {
		t.Error("HashText collisions across distinct inputs")
	}
}

func TestCache_StatsReflectInserts(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "embeddings.jsonl"))
	if err := c.Set("a", "m", []float32{1}); err != nil {
		t.Fatalf("set: %v", err)
	}
	stats := c.Stats()
	if stats.Entries != 1 {
		t.Errorf("entries: got %d want 1", stats.Entries)
	}
	if stats.BytesOnDisk == 0 {
		t.Error("bytesOnDisk should be > 0 after Set")
	}
	if !strings.HasSuffix(stats.Path, "embeddings.jsonl") {
		t.Errorf("path: got %q", stats.Path)
	}
}

func TestCache_TolerateCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.jsonl")
	corrupt := []byte("not json\n{\"sha256\":\"abc\",\"model\":\"m\",\"vector\":[1,2,3]}\n")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	c := NewCache(path)
	// Force load.
	c.Get("anything", "anything")

	stats := c.Stats()
	if stats.Entries != 1 {
		t.Errorf("entries after corrupt+good rows: got %d want 1", stats.Entries)
	}
}

func TestDefaultCachePath_DisabledViaEnv(t *testing.T) {
	t.Setenv("WUPHF_EMBEDDING_CACHE", "disabled")
	if got := DefaultCachePath(); got != "" {
		t.Errorf("disabled cache path: got %q want empty", got)
	}
}

func TestDefaultCachePath_HonoursOverride(t *testing.T) {
	t.Setenv("WUPHF_EMBEDDING_CACHE", "")
	t.Setenv("WUPHF_EMBEDDING_CACHE_PATH", "/tmp/custom-cache.jsonl")
	if got := DefaultCachePath(); got != "/tmp/custom-cache.jsonl" {
		t.Errorf("override: got %q want /tmp/custom-cache.jsonl", got)
	}
}
