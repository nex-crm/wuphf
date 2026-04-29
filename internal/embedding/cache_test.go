package embedding

import (
	"os"
	"path/filepath"
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
