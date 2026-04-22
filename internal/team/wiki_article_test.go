package team

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseWikilinkTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single slug", "See [[people/nazz]] for context.", []string{"people/nazz"}},
		{"slug with display", "See [[people/nazz|Nazz]] here.", []string{"people/nazz"}},
		{"multiple distinct", "[[a]] and [[b/c]] and [[d|D]].", []string{"a", "b/c", "d"}},
		{"deduplicated", "[[a]] and [[a]] again.", []string{"a"}},
		{"empty rejected", "broken: [[ ]] here.", []string{}},
		{"extra pipe rejected", "bad: [[a|b|c]] here.", []string{}},
		{"path traversal rejected", "bad: [[../etc/passwd]] here.", []string{}},
		{"absolute rejected", "bad: [[/absolute]] here.", []string{}},
		{"plain text ignored", "no wikilinks here, only prose.", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWikilinkTargets([]byte(tc.in))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseWikilinkTargets(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRelPathToSlug(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"team/people/nazz.md", "people/nazz"},
		{"team/playbooks/churn.md", "playbooks/churn"},
		{"team/decisions/2026-q1.md", "decisions/2026-q1"},
		{"not-team/x.md", ""},
		{"team/no-extension", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := relPathToSlug(tc.in); got != tc.want {
				t.Fatalf("relPathToSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, relPath, content, want string
	}{
		{"first H1", "team/people/nazz.md", "# Nazz\n\nFounder.", "Nazz"},
		{"skips non-H1", "team/playbooks/x.md", "## Sub\n\n# Real\n\nbody", "Real"},
		{"filename fallback with dashes", "team/people/customer-x.md", "no heading at all", "customer x"},
		{"filename fallback with underscores", "team/people/foo_bar.md", "no heading", "foo bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTitle([]byte(tc.content), tc.relPath)
			if got != tc.want {
				t.Fatalf("extractTitle(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCountWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"# Title\n\nSome body text here.", 6}, // #, Title, Some, body, text, here.
		{"  whitespace\t\tnormalised  ", 2},
	}
	for _, tc := range cases {
		if got := countWords([]byte(tc.in)); got != tc.want {
			t.Errorf("countWords(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestUniqueAuthors(t *testing.T) {
	t.Parallel()
	refs := []CommitRef{
		{Author: "ceo"},
		{Author: "pm"},
		{Author: "ceo"}, // dup
		{Author: "cro"},
		{Author: "pm"}, // dup
	}
	got := uniqueAuthors(refs)
	want := []string{"ceo", "pm", "cro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueAuthors = %v, want %v", got, want)
	}
}

// Integration: BuildArticle over a real git repo.
//
// Arrange: init a repo with 3 articles — A references B, C references B, A/C do not
// reference each other. Act: BuildArticle(B). Assert: backlinks are [A, C] sorted.
func TestBuildArticle_Backlinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()

	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write three articles. B is the target; A and C link to B.
	articles := []struct {
		slug, path, content string
	}{
		{"ceo", "team/people/a.md", "# Article A\n\nReferences [[people/b]] here.\n"},
		{"pm", "team/people/b.md", "# Article B\n\nThe target.\n"},
		{"cro", "team/playbooks/c.md", "# Playbook C\n\nAlso sees [[people/b|B]].\n"},
	}
	for _, a := range articles {
		if _, _, err := repo.Commit(ctx, a.slug, a.path, a.content, "create", "add "+a.path); err != nil {
			t.Fatalf("Commit %s: %v", a.path, err)
		}
	}

	meta, err := repo.BuildArticle(ctx, "team/people/b.md")
	if err != nil {
		t.Fatalf("BuildArticle: %v", err)
	}

	if meta.Path != "team/people/b.md" {
		t.Errorf("Path = %q, want team/people/b.md", meta.Path)
	}
	if meta.Title != "Article B" {
		t.Errorf("Title = %q, want Article B", meta.Title)
	}
	if meta.Content == "" {
		t.Error("Content is empty")
	}
	if meta.Revisions != 1 {
		t.Errorf("Revisions = %d, want 1", meta.Revisions)
	}
	if meta.LastEditedBy != "pm" {
		t.Errorf("LastEditedBy = %q, want pm", meta.LastEditedBy)
	}
	if len(meta.Contributors) != 1 || meta.Contributors[0] != "pm" {
		t.Errorf("Contributors = %v, want [pm]", meta.Contributors)
	}
	if meta.WordCount == 0 {
		t.Error("WordCount = 0, want > 0")
	}

	if len(meta.Backlinks) != 2 {
		t.Fatalf("Backlinks = %v (len %d), want 2", meta.Backlinks, len(meta.Backlinks))
	}
	// Sorted stably by path.
	paths := []string{meta.Backlinks[0].Path, meta.Backlinks[1].Path}
	sort.Strings(paths)
	want := []string{"team/people/a.md", "team/playbooks/c.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("Backlinks paths = %v, want %v", paths, want)
	}
	// Authors come from git log.
	byPath := map[string]string{}
	for _, b := range meta.Backlinks {
		byPath[b.Path] = b.AuthorSlug
	}
	if byPath["team/people/a.md"] != "ceo" {
		t.Errorf("A author = %q, want ceo", byPath["team/people/a.md"])
	}
	if byPath["team/playbooks/c.md"] != "cro" {
		t.Errorf("C author = %q, want cro", byPath["team/playbooks/c.md"])
	}
	// Titles are extracted from first H1.
	byPathTitle := map[string]string{}
	for _, b := range meta.Backlinks {
		byPathTitle[b.Path] = b.Title
	}
	if byPathTitle["team/people/a.md"] != "Article A" {
		t.Errorf("A title = %q, want Article A", byPathTitle["team/people/a.md"])
	}
	if byPathTitle["team/playbooks/c.md"] != "Playbook C" {
		t.Errorf("C title = %q, want Playbook C", byPathTitle["team/playbooks/c.md"])
	}
}

// BuildArticle on a missing article returns an error without panicking.
func TestBuildArticle_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := repo.BuildArticle(context.Background(), "team/people/ghost.md")
	if err == nil {
		t.Fatal("BuildArticle on missing article: want error, got nil")
	}
}

// Path validation rejects bad inputs without doing any I/O.
func TestBuildArticle_RejectsBadPath(t *testing.T) {
	t.Parallel()
	repo := NewRepoAt("/nonexistent", "/nonexistent-bak")
	bad := []string{
		"../etc/passwd",
		"/absolute/path.md",
		"team/../outside.md",
		"not-team/x.md",
	}
	for _, p := range bad {
		if _, err := repo.BuildArticle(context.Background(), p); err == nil {
			t.Errorf("BuildArticle(%q): want error, got nil", p)
		}
	}
}

// BuildCatalog must not surface raw ingested source material under
// team/inbox/. That directory is the scanner's dump target — files
// there are source material, not curated wiki content, and the UI
// would drown in them on any real scan.
func TestBuildCatalog_ExcludesInbox(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Curated brief — must appear in the catalog.
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/nazz.md", "# Nazz\n\nFounder.\n", "create", "add nazz"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Simulate a scanner-written inbox file. The scanner writes these
	// directly to disk (bypassing Commit) under team/inbox/raw/...; any
	// equivalent on-disk path under team/inbox/ must be skipped.
	inboxDir := filepath.Join(root, "team", "inbox", "raw", "some-source")
	if err := os.MkdirAll(inboxDir, 0o700); err != nil {
		t.Fatalf("mkdir inbox: %v", err)
	}
	inboxFile := filepath.Join(inboxDir, "episode.md")
	if err := os.WriteFile(inboxFile, []byte("# Episode\n\nraw transcript.\n"), 0o600); err != nil {
		t.Fatalf("write inbox file: %v", err)
	}

	entries, err := repo.BuildCatalog(ctx)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Path, "team/inbox/") {
			t.Errorf("catalog contained inbox entry %q; inbox should be excluded", e.Path)
		}
	}
	var sawBrief bool
	for _, e := range entries {
		if e.Path == "team/people/nazz.md" {
			sawBrief = true
			break
		}
	}
	if !sawBrief {
		t.Error("expected team/people/nazz.md in catalog, not found")
	}
}

// BuildArticle with no backlinks returns an empty slice (non-nil, JSON-friendly).
func TestBuildArticle_NoBacklinks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	root := t.TempDir()
	backup := filepath.Join(t.TempDir(), "bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := repo.Commit(ctx, "ceo", "team/people/solo.md", "# Solo\n\nAlone.\n", "create", "add solo"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	meta, err := repo.BuildArticle(ctx, "team/people/solo.md")
	if err != nil {
		t.Fatalf("BuildArticle: %v", err)
	}
	if meta.Backlinks == nil {
		t.Error("Backlinks = nil, want []Backlink{}")
	}
	if len(meta.Backlinks) != 0 {
		t.Errorf("Backlinks len = %d, want 0", len(meta.Backlinks))
	}
}
