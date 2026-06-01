package team

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPickAvailableNotebookEntrySlugSkipsExisting is the baseline collision
// test: with {base} and {base}-2 already on disk, the next pick must be the
// first absent slug ({base}-3) and its path must not exist.
func TestPickAvailableNotebookEntrySlugSkipsExisting(t *testing.T) {
	repo := newTestRepo(t)
	owner := "pm"
	base := "launch-risk"

	// Materialize {base}.md and {base}-2.md so the picker must skip both.
	for _, name := range []string{base + ".md", base + "-2.md"} {
		rel := filepath.Join("agents", owner, "notebook", name)
		full := filepath.Join(repo.Root(), rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("existing"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	slug, rel := repo.pickAvailableNotebookEntrySlug(owner, base)
	if slug != base+"-3" {
		t.Fatalf("slug = %q, want %q", slug, base+"-3")
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), filepath.FromSlash(rel))); !os.IsNotExist(err) {
		t.Fatalf("picked path must not exist: stat err = %v", err)
	}
}

// TestPickAvailableNotebookEntrySlugNthCollisionNeverClobbers is the
// regression for the unsafe fixed fallback: even past the old 100-attempt cap
// the picker must keep walking and return a slug whose file is confirmed
// absent — never an unconditional {base}-fallback that could overwrite a
// stranger's note. We seed 100 colliding entries ({base}, {base}-2 ..
// {base}-100), each of which corresponds to an attempt below the old cap, and
// also seed the old hardcoded {base}-fallback so a regression that returns it
// would point at an existing file.
func TestPickAvailableNotebookEntrySlugNthCollisionNeverClobbers(t *testing.T) {
	repo := newTestRepo(t)
	owner := "pm"
	base := "hot-title"

	dir := filepath.Join(repo.Root(), "agents", owner, "notebook")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	seed := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte("existing"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	// attempt 0 -> base; attempt n>0 -> base-(n+1). Seed attempts 0..99,
	// i.e. base and base-2 .. base-100, exactly filling the old 100-cap.
	seed(base)
	for n := 1; n < 100; n++ {
		seed(base + "-" + itoa(n+1))
	}
	// The unsafe old fallback slug — if a regression returned it, this seeded
	// file would be clobbered.
	seed(base + "-fallback")

	slug, rel := repo.pickAvailableNotebookEntrySlug(owner, base)

	// Must be the next free numbered slug (base-101), NOT the hardcoded
	// fallback, and the path must not already exist.
	if slug == base+"-fallback" {
		t.Fatal("picker returned the unsafe hardcoded fallback slug; it could clobber an existing note")
	}
	if slug != base+"-101" {
		t.Fatalf("slug = %q, want %q", slug, base+"-101")
	}
	full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Fatalf("picked path %q must not exist: stat err = %v", rel, err)
	}
}
