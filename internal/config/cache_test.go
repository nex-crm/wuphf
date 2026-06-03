package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCacheDirUsesRuntimeHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	want := filepath.Join(home, ".wuphf", "cache")
	if got := RuntimeCacheDir(); got != want {
		t.Fatalf("RuntimeCacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDirForRuntimeHomeEmpty(t *testing.T) {
	if got := CacheDirForRuntimeHome(""); got != "" {
		t.Fatalf("CacheDirForRuntimeHome(\"\") = %q, want empty", got)
	}
}

func TestEnsureCacheDirCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".wuphf", "cache")

	if err := EnsureCacheDir(dir); err != nil {
		t.Fatalf("EnsureCacheDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("cache path is not a directory: %s", dir)
	}
}

func TestMarkPathExcludedFromBackupIgnoresMissingPath(t *testing.T) {
	if err := MarkPathExcludedFromBackup(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("MarkPathExcludedFromBackup missing path: %v", err)
	}
}
