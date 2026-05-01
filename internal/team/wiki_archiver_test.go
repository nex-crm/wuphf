package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newArchiverRepo initialises a git repo at a temp dir and returns the repo.
func newArchiverRepo(t *testing.T) *Repo {
	t.Helper()
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return repo
}

// commitOld commits an article and then backdates its oldest-commit timestamp
// by manipulating the git log via an amended commit so commitBoundsByPath
// returns an age older than the cutoff. We do this by writing a second commit
// with GIT_AUTHOR_DATE set far in the past.
func commitArticleWithAge(t *testing.T, repo *Repo, relPath, content, slug string, daysOld int) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := repo.Commit(ctx, slug, relPath, content, "create", "add "+relPath); err != nil {
		t.Fatalf("Commit %s: %v", relPath, err)
	}
	// Backdate the commit by amending with a past author date.
	past := time.Now().UTC().Add(-time.Duration(daysOld) * 24 * time.Hour).Format(time.RFC3339)
	repo.mu.Lock()
	_, err := repo.runGitLocked(ctx, slug, "commit", "--amend", "--no-edit",
		"--date="+past)
	repo.mu.Unlock()
	if err != nil {
		t.Fatalf("amend date: %v", err)
	}
}

// TestWikiArchiver_ICP1_StaleArticleArchived covers ICP Example 1:
// a stale article (old + zero reads + ≥50 words) is archived and replaced
// with a tombstone.
func TestWikiArchiver_ICP1_StaleArticiveArchived(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	repo := newArchiverRepo(t)
	ctx := context.Background()

	// Article with 60 words, committed 120 days ago.
	content := "# NovaTech Solutions\n\n" + strings.Repeat("word ", 60) + "\n"
	commitArticleWithAge(t, repo, "team/company/novatech-solutions.md", content, "archivist", 120)

	// No reads — readLog is nil (treated as zero reads).
	archiver := NewWikiArchiver(repo, nil, 90*24*time.Hour)
	result, err := archiver.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if result.Archived != 1 {
		t.Errorf("Archived = %d, want 1", result.Archived)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}

	// Tombstone at original path.
	tombData, err := os.ReadFile(filepath.Join(repo.Root(), "team/company/novatech-solutions.md"))
	if err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if !parseFrontmatterBool(string(tombData), "archived") {
		t.Error("tombstone missing archived: true in frontmatter")
	}
	if !strings.Contains(string(tombData), ".archive/team/company/novatech-solutions.md") {
		t.Error("tombstone missing archive_path reference")
	}

	// Full content preserved in .archive/.
	archData, err := os.ReadFile(filepath.Join(repo.Root(), ".archive/team/company/novatech-solutions.md"))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if !strings.Contains(string(archData), "NovaTech Solutions") {
		t.Error("archive content missing original title")
	}
}

// TestWikiArchiver_ICP2_ShortStubSkipped covers ICP Example 2:
// a ghost stub with < 50 words is skipped even if old and unread.
func TestWikiArchiver_ICP2_ShortStubSkipped(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	repo := newArchiverRepo(t)
	ctx := context.Background()

	// 30-word stub — below archiveMinWordCount.
	short := "---\nslug: acme-micro\nghost: true\n---\n\n# Acme Micro\n\n" + strings.Repeat("word ", 20) + "\n"
	commitArticleWithAge(t, repo, "team/company/acme-micro.md", short, "archivist", 120)

	archiver := NewWikiArchiver(repo, nil, 90*24*time.Hour)
	result, err := archiver.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if result.Archived != 0 {
		t.Errorf("Archived = %d, want 0 (short stub must be skipped)", result.Archived)
	}

	// File unchanged.
	data, err := os.ReadFile(filepath.Join(repo.Root(), "team/company/acme-micro.md"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if parseFrontmatterBool(string(data), "archived") {
		t.Error("short stub was incorrectly archived")
	}
}

// TestWikiArchiver_ICP3_RecentlyReadKept covers ICP Example 3:
// an article read within the cutoff window is kept regardless of file age.
func TestWikiArchiver_ICP3_RecentlyReadKept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	repo := newArchiverRepo(t)
	ctx := context.Background()

	content := "# BlueSky Corp\n\n" + strings.Repeat("word ", 60) + "\n"
	commitArticleWithAge(t, repo, "team/company/bluesky-corp.md", content, "archivist", 200)

	// Simulate a read 15 days ago.
	rl := newTestReadLog(t)
	recent := time.Now().UTC().Add(-15 * 24 * time.Hour)
	ev := ReadEvent{
		Path:      "team/company/bluesky-corp.md",
		Timestamp: recent,
		Reader:    "web",
		IsAgent:   false,
	}
	line, _ := json.Marshal(ev)
	if err := os.MkdirAll(filepath.Dir(rl.path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rl.path, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("write read log: %v", err)
	}

	archiver := NewWikiArchiver(repo, rl, 90*24*time.Hour)
	result, err := archiver.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if result.Archived != 0 {
		t.Errorf("Archived = %d, want 0 (recently-read article must be kept)", result.Archived)
	}
}

// TestBuildCatalog_ExcludesArchived verifies tombstones are excluded by default
// and included when includeArchived=true.
func TestBuildCatalog_ExcludesArchived(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	repo := newArchiverRepo(t)
	ctx := context.Background()

	normal := "# Real Article\n\nSome content here.\n"
	tombstone := "---\narchived: true\narchived_at: 2026-01-01T00:00:00Z\narchive_path: .archive/team/people/old.md\n---\n\n*Archived.*\n"

	if _, _, err := repo.Commit(ctx, "ceo", "team/people/real.md", normal, "create", "add real"); err != nil {
		t.Fatalf("Commit real: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "archivist", "team/people/old.md", tombstone, "create", "add tombstone"); err != nil {
		t.Fatalf("Commit tombstone: %v", err)
	}

	// Default: tombstone excluded.
	entries, err := repo.BuildCatalog(ctx, "", nil, false)
	if err != nil {
		t.Fatalf("BuildCatalog(false): %v", err)
	}
	for _, e := range entries {
		if e.Path == "team/people/old.md" {
			t.Error("tombstone appeared in default catalog (want excluded)")
		}
	}
	found := false
	for _, e := range entries {
		if e.Path == "team/people/real.md" {
			found = true
		}
	}
	if !found {
		t.Error("real article missing from default catalog")
	}

	// With include_archived: tombstone appears with Archived=true.
	all, err := repo.BuildCatalog(ctx, "", nil, true)
	if err != nil {
		t.Fatalf("BuildCatalog(true): %v", err)
	}
	var gotArchived bool
	for _, e := range all {
		if e.Path == "team/people/old.md" {
			gotArchived = true
			if !e.Archived {
				t.Error("tombstone entry has Archived=false, want true")
			}
		}
	}
	if !gotArchived {
		t.Error("tombstone missing from include_archived catalog")
	}
}
