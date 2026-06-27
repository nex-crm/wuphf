package team

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDevTreeFresh pins the live-preview reinstall trigger: a republish that
// changes deps (a fresh bun.lock newer than the installed node_modules) must be
// treated as STALE so the dev server reinstalls. The original bug only checked
// node_modules presence, so a plain-React app republished onto refine kept the
// wrong deps and the Live preview rendered blank.
func TestDevTreeFresh(t *testing.T) {
	dir := t.TempDir()
	nm := filepath.Join(dir, "node_modules")
	lock := filepath.Join(dir, "bun.lock")

	if devTreeFresh(dir) {
		t.Fatal("empty dir (no node_modules) must not be fresh")
	}

	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	if !devTreeFresh(dir) {
		t.Fatal("node_modules present with no lockfile should be usable (fresh)")
	}

	if err := os.WriteFile(lock, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Lockfile newer than node_modules == deps changed on republish == stale.
	older := time.Now().Add(-time.Hour)
	if err := os.Chtimes(nm, older, older); err != nil {
		t.Fatal(err)
	}
	if devTreeFresh(dir) {
		t.Fatal("a lockfile newer than node_modules must force a reinstall")
	}

	// node_modules newer than the lockfile == already installed for these deps.
	newer := time.Now().Add(time.Hour)
	if err := os.Chtimes(nm, newer, newer); err != nil {
		t.Fatal(err)
	}
	if !devTreeFresh(dir) {
		t.Fatal("node_modules newer than the lockfile should be fresh")
	}
}
