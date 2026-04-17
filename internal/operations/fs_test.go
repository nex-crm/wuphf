package operations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// Ensures ListBlueprints/LoadBlueprint transparently fall back to the
// registered embed FS when repoRoot has no templates/ tree. This is the
// behavior the wizard's /onboarding/blueprints handler depends on for
// installs that run outside a repo checkout (`npx wuphf`, `curl | bash`).
func TestListAndLoadBlueprintsUseFallbackFS(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Register the real on-disk tree as the fallback so the loader has
	// something valid to hand back.
	prev := fallbackFS
	SetFallbackFS(os.DirFS(repoRoot))
	t.Cleanup(func() { SetFallbackFS(prev) })

	// repoRoot="" forces the fallback path.
	blueprints, err := ListBlueprints("")
	if err != nil {
		t.Fatalf("ListBlueprints(\"\"): %v", err)
	}
	if len(blueprints) == 0 {
		t.Fatalf("expected blueprints from fallback FS, got 0")
	}

	// Also exercise LoadBlueprint by id — the loader must be able to
	// resolve employee_blueprint references via the same fallback tree.
	bp, err := LoadBlueprint("", blueprints[0].ID)
	if err != nil {
		t.Fatalf("LoadBlueprint(\"\", %q): %v", blueprints[0].ID, err)
	}
	if bp.ID != blueprints[0].ID {
		t.Fatalf("LoadBlueprint id mismatch: got %q want %q", bp.ID, blueprints[0].ID)
	}
}

// Verifies that a filesystem repoRoot overrides the fallback (preserving
// the escape hatch for users who drop a custom blueprint YAML into their
// checkout).
func TestFilesystemWinsOverFallback(t *testing.T) {
	realRoot := findRepoRoot(t)

	tmp := t.TempDir()
	opsDir := filepath.Join(tmp, "templates", "operations")
	if err := os.MkdirAll(opsDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	// Register the real tree as the fallback; if filesystem precedence
	// is broken the loader will pick up its 6 blueprints via the embed
	// path and this test will fail with >0 entries.
	prev := fallbackFS
	SetFallbackFS(os.DirFS(realRoot))
	t.Cleanup(func() { SetFallbackFS(prev) })

	blueprints, err := ListBlueprints(tmp)
	if err != nil {
		t.Fatalf("ListBlueprints(tmp): %v", err)
	}
	if len(blueprints) != 0 {
		t.Fatalf("expected 0 blueprints from empty on-disk tree, got %d — filesystem precedence broken", len(blueprints))
	}
}

// Covers the edge case where the repoRoot tree is missing templates/ but
// the fallback provides it. Ensures readTemplateFile / listTemplateDirs
// fall through rather than surfacing the ENOENT from the filesystem leg.
func TestFallbackFromIncompleteRepoRoot(t *testing.T) {
	realRoot := findRepoRoot(t)
	prev := fallbackFS
	SetFallbackFS(os.DirFS(realRoot))
	t.Cleanup(func() { SetFallbackFS(prev) })

	tmp := t.TempDir() // no templates/ directory at all

	blueprints, err := ListBlueprints(tmp)
	if err != nil {
		t.Fatalf("ListBlueprints(tmp): %v", err)
	}
	if len(blueprints) == 0 {
		t.Fatalf("expected fallback to supply blueprints when repoRoot lacks templates/")
	}
}

// Sanity: readTemplateFile returns fs.ErrNotExist when neither
// filesystem nor fallback has the file.
func TestReadTemplateFileMissingEverywhere(t *testing.T) {
	prev := fallbackFS
	SetFallbackFS(fstest.MapFS{}) // empty FS — registered but has nothing.
	t.Cleanup(func() { SetFallbackFS(prev) })

	_, err := readTemplateFile(t.TempDir(), "templates/operations/nope/blueprint.yaml")
	if err == nil {
		t.Fatalf("expected error when file is absent everywhere")
	}
	if !strings.Contains(err.Error(), "not exist") && !strings.Contains(err.Error(), "no such") {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}
