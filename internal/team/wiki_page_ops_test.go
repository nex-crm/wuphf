package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// initPageRepo returns an initialised wiki repo for page-ops tests.
func initPageRepo(t *testing.T) *Repo {
	t.Helper()
	repo := newTestRepo(t)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	return repo
}

// seedArticle commits an article via the standard write primitive so it has
// git history, mirroring how real articles arrive.
func seedPageArticle(t *testing.T, repo *Repo, relPath, content string) {
	t.Helper()
	if _, _, err := repo.Commit(context.Background(), "seed", relPath, content, "create", "seed: "+relPath); err != nil {
		t.Fatalf("seed %s: %v", relPath, err)
	}
}

// commitCount returns the number of commits reachable from HEAD.
func commitCount(t *testing.T, repo *Repo) int {
	t.Helper()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out, err := repo.runGitLocked(context.Background(), "system", "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("rev-list count: %v: %s", err, out)
	}
	n := 0
	for _, c := range strings.TrimSpace(out) {
		if c < '0' || c > '9' {
			t.Fatalf("non-numeric rev-list output: %q", out)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func readDisk(t *testing.T, repo *Repo, relPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Root(), filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return string(data)
}

func TestWikiPageCreate(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	before := commitCount(t, repo)

	sha, err := repo.CreatePage(ctx, "team/people/nazz.md", "Nazz", "Body text", HumanIdentity{})
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if sha == "" {
		t.Fatal("CreatePage returned empty sha")
	}
	body := readDisk(t, repo, "team/people/nazz.md")
	if !strings.HasPrefix(body, "# Nazz\n") {
		t.Fatalf("expected seeded H1 title, got %q", body)
	}
	if !strings.Contains(body, "Body text") {
		t.Fatalf("expected body content, got %q", body)
	}
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d (exactly one new commit)", after, before+1)
	}
}

func TestWikiPageCreate_ConflictOnExisting(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")

	_, err := repo.CreatePage(ctx, "team/people/nazz.md", "Nazz", "", HumanIdentity{})
	if !errors.Is(err, errWikiPageExists) {
		t.Fatalf("CreatePage over existing: err = %v, want errWikiPageExists", err)
	}
}

func TestWikiPageCreate_BadPath(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()

	cases := []string{
		"people/nazz.md",    // not under team/
		"team/people/nazz",  // missing .md
		"../etc/passwd",     // traversal + not team/
		"team/../secret.md", // traversal
		"/abs/team/x.md",    // absolute
	}
	for _, p := range cases {
		if _, err := repo.CreatePage(ctx, p, "", "x", HumanIdentity{}); !errors.Is(err, errWikiFSBadPath) {
			t.Fatalf("CreatePage(%q): err = %v, want errWikiFSBadPath", p, err)
		}
	}
}

func TestWikiPageMove_RewritesBareSlug(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md", "context [[nazz]] more")
	before := commitCount(t, repo)

	res, err := repo.MovePage(ctx, "team/people/nazz.md", "team/companies/nazz.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	// The bare slug retargets from people/nazz to companies/nazz but its TEXT
	// stays byte-identical (companies/nazz wins the bare resolution post-move),
	// so no reference byte changed: references_rewritten is 0 and the referring
	// article is not in rewritten_paths.
	if res.ReferencesRewritten != 0 {
		t.Fatalf("references_rewritten = %d, want 0 (bare slug stays byte-identical)", res.ReferencesRewritten)
	}
	if len(res.RewrittenPaths) != 0 {
		t.Fatalf("rewritten_paths = %v, want empty", res.RewrittenPaths)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/people/nazz.md")); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
	if got := readDisk(t, repo, "team/companies/nazz.md"); !strings.Contains(got, "# Nazz") {
		t.Fatalf("destination missing content: %q", got)
	}
	// Bare slug stays bare because companies/nazz now wins the bare resolution.
	if got := readDisk(t, repo, "team/projects/x.md"); got != "context [[nazz]] more" {
		t.Fatalf("rewrite = %q, want bare [[nazz]] preserved", got)
	}
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d (exactly one commit)", after, before+1)
	}
}

func TestWikiPageMove_SameBasenameCollisionUntouched(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	// Both nazz pages present; people wins the bare candidate order.
	seedPageArticle(t, repo, "team/people/nazz.md", "# People Nazz\n")
	seedPageArticle(t, repo, "team/companies/nazz.md", "# Company Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md", "ref [[nazz]] here")

	// Move companies/nazz (the SHADOWED one). [[nazz]] resolves to people/nazz,
	// so it must NOT be rewritten.
	res, err := repo.MovePage(ctx, "team/companies/nazz.md", "team/companies/renamed.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if res.ReferencesRewritten != 0 {
		t.Fatalf("references_rewritten = %d, want 0 (bare slug resolves to people/nazz)", res.ReferencesRewritten)
	}
	if got := readDisk(t, repo, "team/projects/x.md"); got != "ref [[nazz]] here" {
		t.Fatalf("collision rewrite = %q, want untouched", got)
	}
}

func TestWikiPageMove_SameBasenameMovingResolvedPageRewrites(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# People Nazz\n")
	seedPageArticle(t, repo, "team/companies/nazz.md", "# Company Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md", "ref [[nazz]] here")

	// Move people/nazz (the one [[nazz]] resolves to). It must be rewritten.
	res, err := repo.MovePage(ctx, "team/people/nazz.md", "team/people/renamed.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if res.ReferencesRewritten != 1 {
		t.Fatalf("references_rewritten = %d, want 1", res.ReferencesRewritten)
	}
	// Bare "renamed" resolves to people/renamed post-move (companies/nazz does
	// not shadow "renamed"), so it stays bare.
	if got := readDisk(t, repo, "team/projects/x.md"); got != "ref [[renamed]] here" {
		t.Fatalf("rewrite = %q, want [[renamed]]", got)
	}
}

func TestWikiPageMove_FullPathAndDisplayPreserved(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md",
		"full [[team/people/nazz.md]] kinded [[people/nazz|Display]]")

	res, err := repo.MovePage(ctx, "team/people/nazz.md", "team/people/naz.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if res.ReferencesRewritten != 2 {
		t.Fatalf("references_rewritten = %d, want 2", res.ReferencesRewritten)
	}
	want := "full [[team/people/naz.md]] kinded [[people/naz|Display]]"
	if got := readDisk(t, repo, "team/projects/x.md"); got != want {
		t.Fatalf("rewrite = %q, want %q", got, want)
	}
}

func TestWikiPageMove_ConflictWhenDestinationExists(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/a.md", "# A\n")
	seedPageArticle(t, repo, "team/people/b.md", "# B\n")

	if _, err := repo.MovePage(ctx, "team/people/a.md", "team/people/b.md", HumanIdentity{}); !errors.Is(err, errWikiPageExists) {
		t.Fatalf("MovePage onto existing: err = %v, want errWikiPageExists", err)
	}
}

func TestWikiPageMove_MissingSource(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	if _, err := repo.MovePage(ctx, "team/people/ghost.md", "team/people/x.md", HumanIdentity{}); !errors.Is(err, errWikiPageMissing) {
		t.Fatalf("MovePage missing source: err = %v, want errWikiPageMissing", err)
	}
}

func TestWikiPageMove_FolderCascade(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/a.md", "# A\n")
	seedPageArticle(t, repo, "team/people/nested/b.md", "# B\n")
	seedPageArticle(t, repo, "team/projects/x.md", "[[people/a]] and [[people/nested/b]] and [[other]]")
	seedPageArticle(t, repo, "team/other.md", "# Other\n")
	before := commitCount(t, repo)

	res, err := repo.MovePage(ctx, "team/people", "team/staff", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage folder: %v", err)
	}
	if res.ReferencesRewritten != 2 {
		t.Fatalf("references_rewritten = %d, want 2", res.ReferencesRewritten)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/staff/a.md")); err != nil {
		t.Fatalf("staff/a.md missing after cascade: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/staff/nested/b.md")); err != nil {
		t.Fatalf("staff/nested/b.md missing after cascade: %v", err)
	}
	want := "[[staff/a]] and [[staff/nested/b]] and [[other]]"
	if got := readDisk(t, repo, "team/projects/x.md"); got != want {
		t.Fatalf("cascade rewrite = %q, want %q", got, want)
	}
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d (single cascade commit)", after, before+1)
	}
}

// A directory move populates MovedPaths with every descendant's POST-move path
// so the SSE fan-out (which can no longer key off res.To, a directory with no
// .md suffix) reaches every moved page.
func TestWikiPageMove_DirectoryPopulatesMovedPaths(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/a.md", "# A\n")
	seedPageArticle(t, repo, "team/people/nested/b.md", "# B\n")

	res, err := repo.MovePage(ctx, "team/people", "team/staff", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage folder: %v", err)
	}
	// res.To is the directory "team/staff" (no .md), so MovedPaths is the only
	// source for the destination SSE events.
	if strings.HasSuffix(strings.ToLower(res.To), ".md") {
		t.Fatalf("directory move res.To = %q, expected a directory path", res.To)
	}
	wantMoved := []string{"team/staff/a.md", "team/staff/nested/b.md"}
	if !reflect.DeepEqual(res.MovedPaths, wantMoved) {
		t.Fatalf("MovedPaths = %v, want %v (sorted post-move descendant pages)", res.MovedPaths, wantMoved)
	}
}

// publishPageMoveEvents must fan out a destination SSE for every moved page even
// when res.To is a DIRECTORY (no .md suffix), driving off MovedPaths. The
// previous .md-suffix guard on res.To dropped the destination events entirely
// for a directory move.
func TestPublishPageMoveEvents_DirectoryFansOutPerPage(t *testing.T) {
	b := newTestBroker(t)
	events, unsubscribe := b.SubscribeWikiEvents(8)
	defer unsubscribe()

	res := PageMoveResult{
		To:             "team/staff", // directory, no .md
		CommitSHA:      "abc123",
		MovedPaths:     []string{"team/staff/a.md", "team/staff/nested/b.md"},
		RewrittenPaths: []string{"team/projects/x.md"},
	}
	b.publishPageMoveEvents(res, "nazz")

	got := make(map[string]wikiWriteEvent)
	want := []string{"team/staff/a.md", "team/staff/nested/b.md", "team/projects/x.md"}
	for range want {
		select {
		case ev := <-events:
			got[ev.Path] = ev
		default:
			t.Fatalf("expected %d events, got %d: %v", len(want), len(got), got)
		}
	}
	for _, p := range want {
		ev, ok := got[p]
		if !ok {
			t.Fatalf("missing SSE event for %q (got %v)", p, got)
		}
		if ev.CommitSHA != "abc123" || ev.AuthorSlug != "nazz" {
			t.Fatalf("event for %q = %+v, want sha=abc123 slug=nazz", p, ev)
		}
	}
	// No extra events (e.g. a phantom event for the bare directory path).
	select {
	case extra := <-events:
		t.Fatalf("unexpected extra SSE event: %+v", extra)
	default:
	}
}

func TestWikiPageMove_SelfLinkInMovedArticle(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	// The moved article links to itself (kinded) and to a sibling that stays.
	seedPageArticle(t, repo, "team/people/a.md", "# A\nself [[people/a]] sibling [[people/b]]")
	seedPageArticle(t, repo, "team/people/b.md", "# B\n")

	res, err := repo.MovePage(ctx, "team/people/a.md", "team/staff/a.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if res.ReferencesRewritten != 1 {
		t.Fatalf("references_rewritten = %d, want 1 (self-link only)", res.ReferencesRewritten)
	}
	got := readDisk(t, repo, "team/staff/a.md")
	want := "# A\nself [[staff/a]] sibling [[people/b]]"
	if got != want {
		t.Fatalf("self/sibling rewrite = %q, want %q", got, want)
	}
}

// undoRewritesLocked must restore a MOVED article whose own body was rewritten
// (a self-link). The pre-move snapshot is keyed by the PRE-move path, but the
// rewritten body lives at the POST-move path; the reverse (To->From) map is the
// only way to recover the original body for that file. This is the rollback the
// commit-failure path depends on.
func TestUndoRewritesLocked_RestoresMovedSelfLinkedArticle(t *testing.T) {
	repo := initPageRepo(t)

	const preBody = "# A\nself [[people/a]] sibling [[people/b]]"
	const postPath = "team/staff/a.md"
	// Snapshot is keyed by PRE-move path (matches scanArticlesLocked output).
	snapshot := map[string]string{
		"team/people/a.md": preBody,
		"team/people/b.md": "# B\n",
	}
	moves := []pageMove{{From: "team/people/a.md", To: postPath}}

	// Simulate the on-disk state right before commit failure: the article has
	// been physically moved to its post-move path and its self-link rewritten.
	absPost := filepath.Join(repo.Root(), filepath.FromSlash(postPath))
	if err := os.MkdirAll(filepath.Dir(absPost), 0o700); err != nil {
		t.Fatalf("mkdir staff: %v", err)
	}
	const rewrittenBody = "# A\nself [[staff/a]] sibling [[people/b]]"
	if err := os.WriteFile(absPost, []byte(rewrittenBody), 0o600); err != nil {
		t.Fatalf("write rewritten body: %v", err)
	}

	repo.mu.Lock()
	repo.undoRewritesLocked(snapshot, []string{postPath}, moves)
	repo.mu.Unlock()

	got := readDisk(t, repo, postPath)
	if got != preBody {
		t.Fatalf("moved self-linked body not restored: got %q, want %q", got, preBody)
	}
}

// End-to-end: a commit failure during a self-link move must leave the moved
// article's body restored to its pre-move content (not the rewritten content).
// We force the failure by making the git object store unwritable after seeding,
// so the rewrite + on-disk move happen but the commit step fails.
func TestWikiPageMove_CommitFailureRestoresSelfLinkBody(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	const preBody = "# A\nself [[people/a]] sibling [[people/b]]"
	seedPageArticle(t, repo, "team/people/a.md", preBody)
	seedPageArticle(t, repo, "team/people/b.md", "# B\n")

	// Make the git object directory read-only so `git commit` cannot write new
	// objects. The rewrite + rename happen first, then the commit fails, driving
	// the rollback path. Restore perms in cleanup so TempDir teardown succeeds.
	objectsDir := filepath.Join(repo.Root(), ".git", "objects")
	info, err := os.Stat(objectsDir)
	if err != nil {
		t.Fatalf("stat objects dir: %v", err)
	}
	if err := os.Chmod(objectsDir, 0o500); err != nil {
		t.Fatalf("chmod objects dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(objectsDir, info.Mode().Perm()) })

	if _, err := repo.MovePage(ctx, "team/people/a.md", "team/staff/a.md", HumanIdentity{}); err == nil {
		t.Fatal("MovePage: expected commit failure, got nil error")
	}

	// Restore perms before reading so any path lookups work; the move was undone,
	// so the body must be back at the PRE-move path with its ORIGINAL content.
	_ = os.Chmod(objectsDir, info.Mode().Perm())
	got := readDisk(t, repo, "team/people/a.md")
	if got != preBody {
		t.Fatalf("after failed move, body = %q, want pre-move %q (self-link rollback)", got, preBody)
	}
}

func TestWikiPageRename_DelegatesToMove(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md", "ref [[people/nazz]]")

	to, err := renameTarget("team/people/nazz.md", "naz")
	if err != nil {
		t.Fatalf("renameTarget: %v", err)
	}
	if to != "team/people/naz.md" {
		t.Fatalf("renameTarget = %q, want team/people/naz.md", to)
	}
	res, err := repo.MovePage(ctx, "team/people/nazz.md", to, HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage (rename): %v", err)
	}
	if res.To != "team/people/naz.md" || res.ReferencesRewritten != 1 {
		t.Fatalf("rename result = %+v, want to=team/people/naz.md refs=1", res)
	}
	if got := readDisk(t, repo, "team/projects/x.md"); got != "ref [[people/naz]]" {
		t.Fatalf("rename rewrite = %q", got)
	}
}

func TestRenameTarget_SanitizesAndRejects(t *testing.T) {
	if _, err := renameTarget("team/people/nazz.md", "../escape"); !errors.Is(err, errWikiFSBadPath) {
		t.Fatal("renameTarget traversal not rejected")
	}
	if _, err := renameTarget("team/people/nazz.md", "a/b"); !errors.Is(err, errWikiFSBadPath) {
		t.Fatal("renameTarget separator not rejected")
	}
	if _, err := renameTarget("team/people/nazz.md", "  "); !errors.Is(err, errWikiFSBadPath) {
		t.Fatal("renameTarget empty not rejected")
	}
	// .md provided explicitly is preserved (not doubled).
	to, err := renameTarget("team/people/nazz.md", "renamed.md")
	if err != nil || to != "team/people/renamed.md" {
		t.Fatalf("renameTarget(renamed.md) = %q, %v", to, err)
	}
}

func TestWikiPageDelete_LeavesBrokenLinks(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")
	seedPageArticle(t, repo, "team/projects/x.md", "ref [[nazz]] survives")
	before := commitCount(t, repo)

	sha, err := repo.DeletePage(ctx, "team/people/nazz.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if sha == "" {
		t.Fatal("DeletePage returned empty sha")
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/people/nazz.md")); !os.IsNotExist(err) {
		t.Fatalf("deleted file still present: %v", err)
	}
	// Link is intentionally NOT rewritten — it is now broken, by design.
	if got := readDisk(t, repo, "team/projects/x.md"); got != "ref [[nazz]] survives" {
		t.Fatalf("delete must not rewrite links, got %q", got)
	}
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d (single delete commit)", after, before+1)
	}
}

func TestWikiPageDelete_Subtree(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/a.md", "# A\n")
	seedPageArticle(t, repo, "team/people/b.md", "# B\n")
	before := commitCount(t, repo)

	if _, err := repo.DeletePage(ctx, "team/people", HumanIdentity{}); err != nil {
		t.Fatalf("DeletePage subtree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/people/a.md")); !os.IsNotExist(err) {
		t.Fatalf("subtree file a still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root(), "team/people/b.md")); !os.IsNotExist(err) {
		t.Fatalf("subtree file b still present: %v", err)
	}
	// The whole subtree deletion is a single commit, mirroring the single-file
	// delete in TestWikiPageDelete_LeavesBrokenLinks.
	if after := commitCount(t, repo); after != before+1 {
		t.Fatalf("commit count = %d, want %d (single subtree-delete commit)", after, before+1)
	}
}

func TestWikiPageDelete_Missing(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	if _, err := repo.DeletePage(ctx, "team/people/ghost.md", HumanIdentity{}); !errors.Is(err, errWikiPageMissing) {
		t.Fatalf("DeletePage missing: err = %v, want errWikiPageMissing", err)
	}
}

// The move commit must be authored as a human identity, not a system author.
func TestWikiPageMove_CommitAuthoredAsHuman(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	seedPageArticle(t, repo, "team/people/nazz.md", "# Nazz\n")

	res, err := repo.MovePage(ctx, "team/people/nazz.md", "team/people/naz.md", HumanIdentity{})
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	refs, err := repo.Log(ctx, "team/people/naz.md")
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("no log entries for moved file")
	}
	if refs[0].SHA != res.CommitSHA && !shaEquivalent(refs[0].SHA, res.CommitSHA) {
		t.Fatalf("latest log sha %q != move sha %q", refs[0].SHA, res.CommitSHA)
	}
	if refs[0].Author != FallbackHumanIdentity.Name {
		t.Fatalf("move author = %q, want %q", refs[0].Author, FallbackHumanIdentity.Name)
	}
}
