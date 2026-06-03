//go:build darwin

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkPathExcludedFromBackupRecreatedSamePathDarwin(t *testing.T) {
	oldTmutilAddExclusion := tmutilAddExclusion
	t.Cleanup(func() {
		tmutilAddExclusion = oldTmutilAddExclusion
	})

	var calls []string
	tmutilAddExclusion = func(path string) error {
		calls = append(calls, path)
		return nil
	}

	dir := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	firstKey := backupExclusionCacheKey(dir)
	if err := MarkPathExcludedFromBackup(dir); err != nil {
		t.Fatalf("first MarkPathExcludedFromBackup: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("tmutil calls after first exclusion = %d, want 1", len(calls))
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove cache: %v", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("recreate cache: %v", err)
	}
	secondKey := backupExclusionCacheKey(dir)
	if firstKey == secondKey {
		t.Skipf("filesystem reused backup exclusion cache key %q", firstKey)
	}
	if err := MarkPathExcludedFromBackup(dir); err != nil {
		t.Fatalf("second MarkPathExcludedFromBackup: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("tmutil calls after recreated exclusion = %d, want 2", len(calls))
	}
}
