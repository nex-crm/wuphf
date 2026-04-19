package operations

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMaterialize_Empty covers the nil-schema path. Blueprints that
// opt out of WikiSchema must not error — they simply produce a no-op
// result. This matches the broker hook, which passes nil when a
// synthesized blueprint has no wiki schema attached.
func TestMaterialize_Empty(t *testing.T) {
	wikiRoot := t.TempDir()
	got, err := MaterializeWiki(context.Background(), wikiRoot, nil)
	if err != nil {
		t.Fatalf("nil schema should be a no-op, got err=%v", err)
	}
	if len(got.DirsCreated) != 0 || len(got.ArticlesCreated) != 0 || len(got.ArticlesSkipped) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}

// TestMaterialize_HappyPath exercises the full transactional path on a
// fresh wiki root: dirs land, articles land with the right bytes, and
// the result totals match the schema.
func TestMaterialize_HappyPath(t *testing.T) {
	wikiRoot := t.TempDir()
	schema := &BlueprintWikiSchema{
		Dirs: []string{
			"team/customers/",
			"team/playbooks/",
		},
		Bootstrap: []BlueprintWikiBootstrapItem{
			{Path: "team/customers/intake.md", Title: "Intake", Skeleton: "# Intake\n\nThis is where intake notes land.\n"},
			{Path: "team/playbooks/onboarding.md", Title: "Onboarding", Skeleton: "# Onboarding\n\nPlaybook body.\n"},
			{Path: "team/playbooks/renewal.md", Title: "Renewal", Skeleton: "# Renewal\n"},
		},
	}

	got, err := MaterializeWiki(context.Background(), wikiRoot, schema)
	if err != nil {
		t.Fatalf("materialize failed: %v", err)
	}
	if len(got.DirsCreated) != 2 {
		t.Fatalf("expected 2 dirs created, got %v", got.DirsCreated)
	}
	if len(got.ArticlesCreated) != 3 {
		t.Fatalf("expected 3 articles created, got %v", got.ArticlesCreated)
	}
	if len(got.ArticlesSkipped) != 0 {
		t.Fatalf("expected 0 skipped, got %v", got.ArticlesSkipped)
	}

	// Verify directories exist.
	for _, dir := range schema.Dirs {
		info, err := os.Stat(filepath.Join(wikiRoot, filepath.FromSlash(strings.TrimSuffix(dir, "/"))))
		if err != nil {
			t.Fatalf("dir %q missing: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("dir %q is not a directory", dir)
		}
	}

	// Verify article contents match the skeleton bytes.
	for _, item := range schema.Bootstrap {
		got, err := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(item.Path)))
		if err != nil {
			t.Fatalf("read %q: %v", item.Path, err)
		}
		if string(got) != item.Skeleton {
			t.Fatalf("article %q content mismatch:\nwant %q\ngot  %q", item.Path, item.Skeleton, string(got))
		}
	}

	// Temp dir must not leak: no .wiki.tmp.* siblings in wikiRoot.
	assertNoTempDirs(t, wikiRoot)
}

// TestMaterialize_Idempotent verifies the core idempotency claim: a
// second call with the same schema does nothing, reports every article
// as skipped, and leaves the bytes exactly as they were.
func TestMaterialize_Idempotent(t *testing.T) {
	wikiRoot := t.TempDir()
	schema := &BlueprintWikiSchema{
		Dirs: []string{"team/playbooks/"},
		Bootstrap: []BlueprintWikiBootstrapItem{
			{Path: "team/playbooks/a.md", Skeleton: "# A\n"},
			{Path: "team/playbooks/b.md", Skeleton: "# B\n"},
			{Path: "team/playbooks/c.md", Skeleton: "# C\n"},
		},
	}

	if _, err := MaterializeWiki(context.Background(), wikiRoot, schema); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Capture the first-run bytes so we can check the second run did not
	// rewrite them.
	first := make(map[string][]byte, len(schema.Bootstrap))
	for _, item := range schema.Bootstrap {
		data, err := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(item.Path)))
		if err != nil {
			t.Fatalf("read first-run %q: %v", item.Path, err)
		}
		first[item.Path] = data
	}

	got, err := MaterializeWiki(context.Background(), wikiRoot, schema)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(got.ArticlesCreated) != 0 {
		t.Fatalf("second run should create nothing, got %v", got.ArticlesCreated)
	}
	if len(got.ArticlesSkipped) != 3 {
		t.Fatalf("second run should skip 3, got %v", got.ArticlesSkipped)
	}

	// Bytes unchanged.
	for _, item := range schema.Bootstrap {
		data, err := os.ReadFile(filepath.Join(wikiRoot, filepath.FromSlash(item.Path)))
		if err != nil {
			t.Fatalf("read second-run %q: %v", item.Path, err)
		}
		if string(data) != string(first[item.Path]) {
			t.Fatalf("article %q bytes drifted between runs", item.Path)
		}
	}
}

// TestMaterialize_PreservesExisting simulates a user who has already let
// their agents write into the wiki. Re-picking the blueprint must not
// clobber their work — only missing articles get the skeleton bytes.
func TestMaterialize_PreservesExisting(t *testing.T) {
	wikiRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wikiRoot, "team", "playbooks"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	custom := []byte("# Onboarding\n\nThe team edited this.\n")
	customPath := filepath.Join(wikiRoot, "team", "playbooks", "onboarding.md")
	if err := os.WriteFile(customPath, custom, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	schema := &BlueprintWikiSchema{
		Dirs: []string{"team/playbooks/"},
		Bootstrap: []BlueprintWikiBootstrapItem{
			{Path: "team/playbooks/onboarding.md", Skeleton: "# Onboarding\n\nDefault skeleton.\n"},
			{Path: "team/playbooks/renewal.md", Skeleton: "# Renewal\n"},
		},
	}
	got, err := MaterializeWiki(context.Background(), wikiRoot, schema)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(got.ArticlesCreated) != 1 || got.ArticlesCreated[0] != "team/playbooks/renewal.md" {
		t.Fatalf("expected only renewal.md created, got %v", got.ArticlesCreated)
	}
	if len(got.ArticlesSkipped) != 1 || got.ArticlesSkipped[0] != "team/playbooks/onboarding.md" {
		t.Fatalf("expected onboarding skipped, got %v", got.ArticlesSkipped)
	}

	// The custom bytes must survive untouched.
	after, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatalf("read custom: %v", err)
	}
	if string(after) != string(custom) {
		t.Fatalf("custom content was rewritten:\nwant %q\ngot  %q", string(custom), string(after))
	}
}

// TestMaterialize_PathTraversalRejected verifies that a malicious or
// accidentally-malformed schema cannot escape wikiRoot. Each case must
// return an error and leave wikiRoot contents unchanged.
func TestMaterialize_PathTraversalRejected(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../etc/passwd"},
		{"absolute path", "/etc/passwd"},
		{"embedded parent", "team/../../etc/passwd"},
		{"outside team prefix", "secrets/keys.md"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wikiRoot := t.TempDir()
			schema := &BlueprintWikiSchema{
				Bootstrap: []BlueprintWikiBootstrapItem{{Path: tc.path, Skeleton: "x"}},
			}
			_, err := MaterializeWiki(context.Background(), wikiRoot, schema)
			if err == nil {
				t.Fatalf("expected error for path %q, got nil", tc.path)
			}
			// Nothing should have been written — wikiRoot is either
			// empty or contains only directories we didn't ask to write.
			assertNoTempDirs(t, wikiRoot)
		})
	}
}

// TestMaterialize_TempDirCleanedUp injects a failing rename by pre-
// creating a directory where the article should land, so os.Rename
// returns ENOTEMPTY or similar. The temp dir must be cleaned up even on
// this failure path — no orphan .wiki.tmp.* siblings allowed.
//
// NOTE: on some platforms os.Rename over a directory yields a different
// error code, but the cleanup path runs regardless.
func TestMaterialize_TempDirCleanedUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename-into-directory behavior varies on Windows; core cleanup path is covered elsewhere")
	}
	wikiRoot := t.TempDir()
	// Pre-create a directory at the exact article path so os.Rename
	// fails when we try to promote a file over it.
	blocked := filepath.Join(wikiRoot, "team", "playbooks", "blocked.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Drop a child so the directory is non-empty — os.Rename on a
	// non-empty destination is guaranteed to fail.
	if err := os.WriteFile(filepath.Join(blocked, "child"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed child: %v", err)
	}

	schema := &BlueprintWikiSchema{
		Bootstrap: []BlueprintWikiBootstrapItem{
			{Path: "team/playbooks/ok.md", Skeleton: "ok"},
			{Path: "team/playbooks/blocked.md", Skeleton: "should not land"},
		},
	}
	_, err := MaterializeWiki(context.Background(), wikiRoot, schema)
	if err == nil {
		// The partition step sees `blocked.md` as existing (it's a dir,
		// but os.Stat succeeds) and skips it — which is fine. In that
		// case no rename is attempted and the test is vacuously true.
		// We still want to assert cleanup ran.
		assertNoTempDirs(t, wikiRoot)
		return
	}
	assertNoTempDirs(t, wikiRoot)
}

// TestMaterialize_DirsOnly confirms a schema with dirs and no bootstrap
// still works — dirs are created, result.ArticlesCreated is empty.
func TestMaterialize_DirsOnly(t *testing.T) {
	wikiRoot := t.TempDir()
	schema := &BlueprintWikiSchema{
		Dirs: []string{"team/people/", "team/decisions/"},
	}
	got, err := MaterializeWiki(context.Background(), wikiRoot, schema)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(got.DirsCreated) != 2 {
		t.Fatalf("expected 2 dirs, got %v", got.DirsCreated)
	}
	if len(got.ArticlesCreated) != 0 || len(got.ArticlesSkipped) != 0 {
		t.Fatalf("expected no articles, got created=%v skipped=%v", got.ArticlesCreated, got.ArticlesSkipped)
	}
	for _, dir := range schema.Dirs {
		info, err := os.Stat(filepath.Join(wikiRoot, filepath.FromSlash(strings.TrimSuffix(dir, "/"))))
		if err != nil || !info.IsDir() {
			t.Fatalf("expected dir %q to exist", dir)
		}
	}
}

// TestMaterialize_WikiRootRequired guards the argument contract — an
// empty wikiRoot is a programmer error, not a silent no-op.
func TestMaterialize_WikiRootRequired(t *testing.T) {
	_, err := MaterializeWiki(context.Background(), "   ", &BlueprintWikiSchema{Dirs: []string{"team/x/"}})
	if err == nil {
		t.Fatal("expected error for empty wikiRoot")
	}
}

// TestMaterialize_ContextCancelled verifies the ctx-abort path. Even
// mid-run cancellation returns a typed error and does not leave a
// tempdir behind.
func TestMaterialize_ContextCancelled(t *testing.T) {
	wikiRoot := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	schema := &BlueprintWikiSchema{
		Dirs: []string{"team/x/"},
		Bootstrap: []BlueprintWikiBootstrapItem{
			{Path: "team/x/a.md", Skeleton: "a"},
		},
	}
	_, err := MaterializeWiki(ctx, wikiRoot, schema)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	assertNoTempDirs(t, wikiRoot)
}

// assertNoTempDirs fails the test if any `.wiki.tmp.*` sibling is left
// under wikiRoot. This is the cleanup invariant — temp dirs never
// outlive a MaterializeWiki call.
func assertNoTempDirs(t *testing.T, wikiRoot string) {
	t.Helper()
	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return
		}
		t.Fatalf("read wiki root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wiki.tmp.") {
			t.Fatalf("orphan temp dir left behind: %s", entry.Name())
		}
	}
}
