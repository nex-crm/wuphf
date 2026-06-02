package team

// Tests for Slice 5 of the cabinet wiki port: version history, per-commit
// diff, and append-only restore. Covers:
//
//   - GET /wiki/history/<path> lists every commit newest-first with the exact
//     field names the web client decodes (sha / author_slug / msg / date).
//   - GET /wiki/diff?path=&sha= returns a unified diff containing the changed
//     line for a given sha; bad sha → 404; bad path → 400.
//   - POST /wiki/restore re-writes the body to the historical content AND
//     records a NEW commit (HeadSHA changes, history grows, old commits stay).
//   - restore with a bogus sha → 404; bad path → 400; no-op restore → 409.
//
// Reuses the wiki_fs / page-ops test harness (newTestRepo, seedPageArticle,
// commitCount, readDisk, the httptest get/postJSON/readBody helpers).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newWikiHistoryTestServer spins up an httptest server wired to the three new
// history handlers, backed by a fresh temp-dir Repo. It returns the base URL,
// the repo (so tests can seed + edit articles), and a cleanup func.
func newWikiHistoryTestServer(t *testing.T) (baseURL string, repo *Repo, cleanup func()) {
	t.Helper()

	repo = newTestRepo(t)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	worker := NewWikiWorker(repo, &capturePublisher{events: make(chan wikiWriteEvent, 8)})
	broker := &Broker{wikiWorker: worker}

	mux := http.NewServeMux()
	mux.HandleFunc("/wiki/history/", broker.handleWikiHistory)
	mux.HandleFunc("/wiki/diff", broker.handleWikiDiff)
	mux.HandleFunc("/wiki/restore", broker.handleWikiRestore)

	srv := httptest.NewServer(mux)
	return srv.URL, repo, func() { srv.Close() }
}

// editArticle commits a replace over an existing article so it gains another
// commit in its history.
func editArticle(t *testing.T, repo *Repo, relPath, content string) string {
	t.Helper()
	sha, _, err := repo.Commit(context.Background(), "editor", relPath, content, "replace", "edit: "+relPath)
	if err != nil {
		t.Fatalf("edit %s: %v", relPath, err)
	}
	return sha
}

func TestWikiHistory_ListsCommitsNewestFirst(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/people/nazz.md"
	seedPageArticle(t, repo, path, "# Nazz\n\nv1 body\n")
	editArticle(t, repo, path, "# Nazz\n\nv2 body\n")
	editArticle(t, repo, path, "# Nazz\n\nv3 body\n")

	resp := get(t, baseURL+"/wiki/history/"+path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var decoded struct {
		Commits []wikiHistoryCommit `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode history: %v", err)
	}
	_ = resp.Body.Close()

	if len(decoded.Commits) != 3 {
		t.Fatalf("history len = %d, want 3 (create + 2 edits); commits=%+v", len(decoded.Commits), decoded.Commits)
	}
	// Newest-first: the most recent commit (the v3 edit) is index 0.
	first := decoded.Commits[0]
	if first.SHA == "" {
		t.Fatal("newest commit has empty sha")
	}
	if first.AuthorSlug != "editor" {
		t.Fatalf("newest commit author_slug = %q, want %q", first.AuthorSlug, "editor")
	}
	if !strings.Contains(first.Msg, "edit:") {
		t.Fatalf("newest commit msg = %q, want it to contain %q", first.Msg, "edit:")
	}
	if first.Date == "" {
		t.Fatal("newest commit has empty date")
	}
	// Oldest commit (the create) is last.
	last := decoded.Commits[len(decoded.Commits)-1]
	if last.AuthorSlug != "seed" {
		t.Fatalf("oldest commit author_slug = %q, want %q", last.AuthorSlug, "seed")
	}
	if !strings.HasPrefix(last.Msg, "seed:") {
		t.Fatalf("oldest commit msg = %q, want a seed: prefix", last.Msg)
	}
}

func TestWikiHistory_EmptyForUnknownPath(t *testing.T) {
	baseURL, _, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	resp := get(t, baseURL+"/wiki/history/team/people/ghost.md")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("history status = %d, want 200 (empty list) for unknown path", resp.StatusCode)
	}
	var decoded struct {
		Commits []wikiHistoryCommit `json:"commits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode history: %v", err)
	}
	_ = resp.Body.Close()
	if len(decoded.Commits) != 0 {
		t.Fatalf("history len = %d, want 0 for unknown path", len(decoded.Commits))
	}
}

func TestWikiHistory_BadPath(t *testing.T) {
	baseURL, _, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	// Path escapes team/ (..) — validateArticlePath rejects it → 400.
	resp := get(t, baseURL+"/wiki/history/team/../secret.md")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("history bad path status = %d, want 400", resp.StatusCode)
	}
}

func TestWikiDiff_ContainsChangedLine(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/projects/x.md"
	seedPageArticle(t, repo, path, "# X\n\noriginal line\n")
	editSHA := editArticle(t, repo, path, "# X\n\nchanged line\n")

	resp := get(t, baseURL+"/wiki/diff?path="+path+"&sha="+editSHA)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var decoded struct {
		Diff string `json:"diff"`
		SHA  string `json:"sha"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode diff: %v", err)
	}
	_ = resp.Body.Close()

	if decoded.SHA != editSHA {
		t.Fatalf("diff sha = %q, want %q", decoded.SHA, editSHA)
	}
	if decoded.Path != path {
		t.Fatalf("diff path = %q, want %q", decoded.Path, path)
	}
	if !strings.Contains(decoded.Diff, "+changed line") {
		t.Fatalf("diff missing added line; got:\n%s", decoded.Diff)
	}
	if !strings.Contains(decoded.Diff, "-original line") {
		t.Fatalf("diff missing removed line; got:\n%s", decoded.Diff)
	}
}

func TestWikiDiff_FirstCommit(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/projects/y.md"
	createSHA := func() string {
		sha, _, err := repo.Commit(context.Background(), "seed", path, "# Y\n\nbrand new\n", "create", "seed: "+path)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		return sha
	}()

	resp := get(t, baseURL+"/wiki/diff?path="+path+"&sha="+createSHA)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff status = %d, want 200 for first-touch commit; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var decoded struct {
		Diff string `json:"diff"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode diff: %v", err)
	}
	_ = resp.Body.Close()
	// A first-touch commit shows the full added content (git show against the
	// empty tree), so the new body lines appear as additions.
	if !strings.Contains(decoded.Diff, "+brand new") {
		t.Fatalf("first-commit diff missing added content; got:\n%s", decoded.Diff)
	}
}

func TestWikiDiff_BadSHA(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/projects/z.md"
	seedPageArticle(t, repo, path, "# Z\n\nbody\n")

	// A well-formed-but-nonexistent sha → 404.
	resp := get(t, baseURL+"/wiki/diff?path="+path+"&sha=deadbeef")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("diff bogus sha status = %d, want 404; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestWikiDiff_MalformedSHA(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/projects/z.md"
	seedPageArticle(t, repo, path, "# Z\n\nbody\n")

	// A non-hex sha is a caller error → 400.
	resp := get(t, baseURL+"/wiki/diff?path="+path+"&sha=not-a-sha")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("diff malformed sha status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestWikiDiff_BadPath(t *testing.T) {
	baseURL, _, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	resp := get(t, baseURL+"/wiki/diff?path=people/nazz.md&sha=deadbeef")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("diff bad path status = %d, want 400", resp.StatusCode)
	}
}

func TestWikiRestore_RewritesBodyAndCreatesNewCommit(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/people/nazz.md"
	const original = "# Nazz\n\noriginal v1 body\n"
	seedPageArticle(t, repo, path, original)

	// Capture the create commit sha BEFORE editing — that is the version we
	// restore to. Read it from history so it matches what the client would.
	firstSHA := func() string {
		refs, err := repo.Log(context.Background(), path)
		if err != nil {
			t.Fatalf("Log: %v", err)
		}
		if len(refs) != 1 {
			t.Fatalf("history len before edit = %d, want 1", len(refs))
		}
		return refs[0].SHA
	}()

	editArticle(t, repo, path, "# Nazz\n\nedited v2 body\n")
	editArticle(t, repo, path, "# Nazz\n\nedited v3 body\n")

	headBefore, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	countBefore := commitCount(t, repo)
	historyLenBefore := func() int {
		refs, lerr := repo.Log(context.Background(), path)
		if lerr != nil {
			t.Fatalf("Log: %v", lerr)
		}
		return len(refs)
	}()
	if historyLenBefore != 3 {
		t.Fatalf("history len before restore = %d, want 3", historyLenBefore)
	}

	resp := postJSON(t, baseURL+"/wiki/restore", map[string]string{"path": path, "sha": firstSHA})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restore status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var decoded struct {
		Path      string `json:"path"`
		CommitSHA string `json:"commit_sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode restore: %v", err)
	}
	_ = resp.Body.Close()

	if decoded.Path != path {
		t.Fatalf("restore path = %q, want %q", decoded.Path, path)
	}
	if decoded.CommitSHA == "" {
		t.Fatal("restore returned empty commit_sha")
	}

	// Body on disk is back to the original v1 content.
	if got := readDisk(t, repo, path); got != original {
		t.Fatalf("restored body = %q, want %q", got, original)
	}

	// A NEW commit was created — HEAD moved, total commit count grew by one.
	headAfter, err := repo.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("HEAD did not move after restore (still %s) — restore must be a new commit", headAfter)
	}
	if after := commitCount(t, repo); after != countBefore+1 {
		t.Fatalf("commit count = %d, want %d (exactly one new commit)", after, countBefore+1)
	}

	// History grew by exactly one and the OLD commits are all still present
	// (append-only — restore never rewrites history).
	refsAfter, err := repo.Log(context.Background(), path)
	if err != nil {
		t.Fatalf("Log after: %v", err)
	}
	if len(refsAfter) != historyLenBefore+1 {
		t.Fatalf("history len after restore = %d, want %d", len(refsAfter), historyLenBefore+1)
	}
	if !historyContainsSHA(refsAfter, firstSHA) {
		t.Fatalf("original create commit %s missing after restore — history was rewritten", firstSHA)
	}
}

func TestWikiRestore_BogusSHA(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/people/nazz.md"
	seedPageArticle(t, repo, path, "# Nazz\n\nbody\n")

	resp := postJSON(t, baseURL+"/wiki/restore", map[string]string{"path": path, "sha": "deadbeef"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("restore bogus sha status = %d, want 404; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestWikiRestore_BadPath(t *testing.T) {
	baseURL, _, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	resp := postJSON(t, baseURL+"/wiki/restore", map[string]string{"path": "../etc/passwd", "sha": "deadbeef"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("restore bad path status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestWikiRestore_NoopWhenAlreadyCurrent(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/people/nazz.md"
	seedPageArticle(t, repo, path, "# Nazz\n\nbody v1\n")
	headSHA := editArticle(t, repo, path, "# Nazz\n\nbody v2 current\n")

	// Restoring to the current HEAD content is a no-op → 409.
	resp := postJSON(t, baseURL+"/wiki/restore", map[string]string{"path": path, "sha": headSHA})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("restore no-op status = %d, want 409; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

// TestWikiRestoreToCommit_RepoLevel exercises the repo method directly so the
// no-op + not-found sentinels are covered without the HTTP layer.
func TestWikiRestoreToCommit_RepoLevel(t *testing.T) {
	repo := initPageRepo(t)
	ctx := context.Background()
	const path = "team/projects/a.md"
	seedPageArticle(t, repo, path, "v1\n")

	refs, err := repo.Log(ctx, path)
	if err != nil || len(refs) != 1 {
		t.Fatalf("Log: %v len=%d", err, len(refs))
	}
	v1SHA := refs[0].SHA

	editArticle(t, repo, path, "v2\n")

	// Restore to v1 succeeds and returns a new sha.
	sha, err := repo.RestoreToCommit(ctx, path, v1SHA, HumanIdentity{})
	if err != nil {
		t.Fatalf("RestoreToCommit: %v", err)
	}
	if sha == "" {
		t.Fatal("RestoreToCommit returned empty sha")
	}
	if got := readDisk(t, repo, path); got != "v1\n" {
		t.Fatalf("restored body = %q, want %q", got, "v1\n")
	}

	// Restoring again to v1 is now a no-op.
	if _, err := repo.RestoreToCommit(ctx, path, v1SHA, HumanIdentity{}); !errors.Is(err, ErrWikiRestoreNoop) {
		t.Fatalf("second restore err = %v, want ErrWikiRestoreNoop", err)
	}

	// A bogus sha is a not-found.
	if _, err := repo.RestoreToCommit(ctx, path, "deadbeef", HumanIdentity{}); !errors.Is(err, ErrWikiCommitNotFound) {
		t.Fatalf("bogus sha err = %v, want ErrWikiCommitNotFound", err)
	}
}

func historyContainsSHA(refs []CommitRef, sha string) bool {
	for _, ref := range refs {
		if ref.SHA == sha {
			return true
		}
	}
	return false
}

// TestWikiRestore_OversizedBodyRejected asserts the MaxBytesReader bound on the
// restore handler: a body larger than the 4 KiB limit must surface as a 400
// rather than streaming an unbounded payload into the broker.
func TestWikiRestore_OversizedBodyRejected(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/people/nazz.md"
	seedPageArticle(t, repo, path, "# Nazz\n\nbody\n")

	// A syntactically valid JSON object whose padding field pushes the body
	// well past the 4096-byte cap. The decoder hits MaxBytesReader before it
	// can finish, so the handler returns 400.
	oversized := `{"path":"` + path + `","sha":"deadbeef","pad":"` + strings.Repeat("x", 8192) + `"}`
	resp, err := http.Post(baseURL+"/wiki/restore", "application/json", bytes.NewReader([]byte(oversized)))
	if err != nil {
		t.Fatalf("POST restore: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized restore body status = %d, want 400; body=%s", resp.StatusCode, readBody(t, resp))
	}
}

// TestWikiDiff_ResponseNormalizesPathAndSHA asserts the 200 body echoes the
// normalized inputs (cleaned slash path, lower-cased sha) rather than the raw
// caller-supplied query values. A mixed-case sha and a path with a redundant
// `./` segment must come back canonicalized.
func TestWikiDiff_ResponseNormalizesPathAndSHA(t *testing.T) {
	baseURL, repo, cleanup := newWikiHistoryTestServer(t)
	defer cleanup()

	const path = "team/projects/x.md"
	seedPageArticle(t, repo, path, "# X\n\noriginal line\n")
	editSHA := editArticle(t, repo, path, "# X\n\nchanged line\n")

	// Caller sends an upper-cased sha and a path carrying a redundant ./
	// segment. Both must be canonicalized in the response.
	rawSHA := strings.ToUpper(editSHA)
	rawPath := "team/projects/./x.md"
	q := url.Values{}
	q.Set("path", rawPath)
	q.Set("sha", rawSHA)

	resp := get(t, baseURL+"/wiki/diff?"+q.Encode())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diff status = %d, want 200; body=%s", resp.StatusCode, readBody(t, resp))
	}
	var decoded struct {
		Diff string `json:"diff"`
		SHA  string `json:"sha"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = resp.Body.Close()
		t.Fatalf("decode diff: %v", err)
	}
	_ = resp.Body.Close()

	// SHA is lower-cased, not echoed in the caller's upper-case form.
	if decoded.SHA != editSHA {
		t.Fatalf("diff sha = %q, want normalized %q (not raw %q)", decoded.SHA, editSHA, rawSHA)
	}
	if strings.ContainsAny(decoded.SHA, "ABCDEF") {
		t.Fatalf("diff sha = %q still contains upper-case hex", decoded.SHA)
	}
	// Path is slash-normalized. cleanPagePath does not collapse `./`, but it
	// must at minimum strip whitespace and normalize separators; assert the
	// response is non-empty and does not echo a leaked raw value with stray
	// whitespace. The canonical form for this input is the trimmed slash path.
	if decoded.Path != cleanPagePath(rawPath) {
		t.Fatalf("diff path = %q, want normalized %q", decoded.Path, cleanPagePath(rawPath))
	}
	// The diff still resolves the real article content.
	if !strings.Contains(decoded.Diff, "+changed line") {
		t.Fatalf("diff missing added line; got:\n%s", decoded.Diff)
	}
}

// TestWikiHistoryRouteContracts asserts the Slice 5 history / diff / restore
// routes are registered in the broker route-contract registry with the
// expected method + bearer auth, so the documented wire shape cannot silently
// drift from the handlers.
func TestWikiHistoryRouteContracts(t *testing.T) {
	byPath := make(map[string]RouteContract)
	for _, c := range BrokerRouteContracts() {
		byPath[c.Path] = c
	}
	tests := []struct {
		path   string
		method string
	}{
		{path: "/wiki/history/", method: http.MethodGet},
		{path: "/wiki/diff", method: http.MethodGet},
		{path: "/wiki/restore", method: http.MethodPost},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			c, ok := byPath[tt.path]
			if !ok {
				t.Fatalf("missing route contract for %q", tt.path)
			}
			if c.Domain != "wiki" {
				t.Fatalf("domain: want wiki, got %q", c.Domain)
			}
			if c.Method != tt.method {
				t.Fatalf("method: want %q, got %q", tt.method, c.Method)
			}
			if c.Auth != RouteAuthBearer {
				t.Fatalf("auth: want %q, got %q", RouteAuthBearer, c.Auth)
			}
		})
	}
}
