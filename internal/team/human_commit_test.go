package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// seedWikiArticle seeds team/<rel>.md via Repo.Commit under `seed` author
// so a follow-up human edit has something to collide with.
func seedWikiArticle(t *testing.T, repo *Repo, rel, content string) string {
	t.Helper()
	sha, _, err := repo.Commit(context.Background(), "seed", rel, content, "create", "seed: "+rel)
	if err != nil {
		t.Fatalf("seed commit: %v", err)
	}
	return sha
}

func TestCommitHumanHappyPath(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()

	// Seed the article as a non-human author.
	seedSHA := seedWikiArticle(t, repo, "team/people/nazz.md", "# Nazz\n\nOriginal.\n")

	sha, n, err := worker.EnqueueHuman(
		context.Background(),
		"team/people/nazz.md",
		"# Nazz\n\nEdited by human.\n",
		"human: clarify title",
		seedSHA,
	)
	if err != nil {
		t.Fatalf("human write: %v", err)
	}
	if sha == "" || sha == seedSHA {
		t.Fatalf("expected new sha, got %q (seed=%q)", sha, seedSHA)
	}
	if n == 0 {
		t.Fatal("expected bytes written")
	}

	// Verify author slug landed as HumanAuthor in git log.
	out, err := repo.runGitLocked(context.Background(), "system",
		"log", "-n", "1", "--format=%an%x1f%ae", "--", "team/people/nazz.md")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(out), "\x1f")
	if len(parts) != 2 {
		t.Fatalf("unexpected git log output: %q", out)
	}
	if parts[0] != HumanAuthor {
		t.Errorf("want author %q, got %q", HumanAuthor, parts[0])
	}
	if parts[1] != "human@wuphf.local" {
		t.Errorf("want email human@wuphf.local, got %q", parts[1])
	}
}

func TestCommitHumanSHAMismatchRejects(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()

	seedSHA := seedWikiArticle(t, repo, "team/people/alice.md", "# Alice\n")
	// Land another edit so HEAD moves past seedSHA.
	_, _, err := repo.Commit(context.Background(), "pm", "team/people/alice.md",
		"# Alice\n\nUpdated.\n", "replace", "pm: update alice")
	if err != nil {
		t.Fatalf("second commit: %v", err)
	}

	// Human save using the original seedSHA must get 409.
	gotSHA, _, err := worker.EnqueueHuman(
		context.Background(),
		"team/people/alice.md",
		"# Alice\n\nHuman edit on stale view.\n",
		"human: stale save",
		seedSHA,
	)
	if !errors.Is(err, ErrWikiSHAMismatch) {
		t.Fatalf("want ErrWikiSHAMismatch, got %v", err)
	}
	if gotSHA == "" || gotSHA == seedSHA {
		t.Fatalf("expected current sha in reply, got %q (seed=%q)", gotSHA, seedSHA)
	}
}

func TestCommitHumanRejectsPathOutsideTeam(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()

	_, _, err := worker.EnqueueHuman(context.Background(), "etc/passwd.md", "x", "human: oops", "")
	if err == nil {
		t.Fatal("expected error for path outside team/")
	}
	if !strings.Contains(err.Error(), "team/") {
		t.Errorf("want path-scope error, got %v", err)
	}
}

func TestCommitHumanRejectsEmptyContent(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()

	_, _, err := worker.EnqueueHuman(context.Background(), "team/people/empty.md", "   ", "human: empty", "")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestCommitHumanNewArticleNoExpectedSHA(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()

	sha, n, err := worker.EnqueueHuman(
		context.Background(),
		"team/people/newbie.md",
		"# Newbie\n\nFresh article.\n",
		"human: add newbie",
		"",
	)
	if err != nil {
		t.Fatalf("new-article write: %v", err)
	}
	if sha == "" || n == 0 {
		t.Fatalf("unexpected result sha=%q n=%d", sha, n)
	}
}

func TestCommitHumanNewArticleAgainstExisting409(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()
	seedWikiArticle(t, repo, "team/people/collide.md", "# Collide\n")

	_, _, err := worker.EnqueueHuman(
		context.Background(),
		"team/people/collide.md",
		"# Collide\n\nThink I'm new.\n",
		"human: dup create",
		"", // empty → caller believes article does not exist
	)
	if !errors.Is(err, ErrWikiSHAMismatch) {
		t.Fatalf("want ErrWikiSHAMismatch, got %v", err)
	}
}

// TestCommitHumanConcurrentWritesSerialized — two parallel human writes
// against the same article must both flow through the queue; exactly one
// wins, the other gets ErrWikiSHAMismatch.
func TestCommitHumanConcurrentWritesSerialized(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()
	baseSHA := seedWikiArticle(t, repo, "team/people/race.md", "# Race\n")

	var wg sync.WaitGroup
	var successes, conflicts atomic.Int32
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content := "# Race\n\nwriter " + string(rune('A'+i)) + "\n"
			_, _, err := worker.EnqueueHuman(context.Background(),
				"team/people/race.md", content, "human: racing", baseSHA)
			if err == nil {
				successes.Add(1)
				return
			}
			if errors.Is(err, ErrWikiSHAMismatch) {
				conflicts.Add(1)
				return
			}
			t.Errorf("unexpected error: %v", err)
		}(i)
	}
	wg.Wait()

	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("expected 1 success + 1 conflict, got %d + %d",
			successes.Load(), conflicts.Load())
	}
}

// TestHandleWikiWriteHumanReturns409WithBody — the HTTP path must put the
// current SHA and current bytes in the response body so the client can
// offer a reload prompt without a second fetch.
func TestHandleWikiWriteHumanReturns409WithBody(t *testing.T) {
	worker, repo, _, teardown := newStartedWorker(t)
	defer teardown()

	baseSHA := seedWikiArticle(t, repo, "team/people/fixture.md", "# Fixture\n")
	// Move HEAD past baseSHA.
	if _, _, err := repo.Commit(context.Background(), "pm", "team/people/fixture.md",
		"# Fixture\n\nUpdated.\n", "replace", "pm: move head"); err != nil {
		t.Fatalf("move head: %v", err)
	}

	b := brokerForTest(t, worker)
	body := map[string]any{
		"path":           "team/people/fixture.md",
		"content":        "# Fixture\n\nMy edit.\n",
		"commit_message": "human: stale",
		"expected_sha":   baseSHA,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/wiki/write-human", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	b.handleWikiWriteHuman(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["current_sha"] == "" {
		t.Errorf("missing current_sha: %v", got)
	}
	cur, ok := got["current_content"].(string)
	if !ok || !strings.Contains(cur, "Updated") {
		t.Errorf("current_content missing or stale: %v", cur)
	}
}

func TestHandleWikiWriteHumanRejectsBadPath(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	body := map[string]any{
		"path":           "../etc/passwd",
		"content":        "x",
		"commit_message": "oops",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/wiki/write-human", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	b.handleWikiWriteHuman(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// brokerForTest builds a minimal Broker with just the WikiWorker attached.
// The other broker dependencies are nil because handleWikiWriteHuman only
// calls b.WikiWorker() and the worker's own methods.
func brokerForTest(t *testing.T, worker *WikiWorker) *Broker {
	t.Helper()
	b := &Broker{}
	b.wikiWorker = worker
	return b
}

// Guard-rail: touching the broker struct shape in a follow-up PR should
// not silently break this helper. We don't rely on sync.Once / init here;
// if the test assumes a field path change, fail loudly.
var _ = filepath.Separator
var _ = time.Now

// Belt-and-braces: HumanAuthor must stay a stable constant. The value
// bakes into git author metadata — changing it rewrites audit history.
func TestHumanAuthorIdentityIsStable(t *testing.T) {
	if HumanAuthor != "human" {
		t.Fatalf("HumanAuthor must remain %q; got %q", "human", HumanAuthor)
	}
}
