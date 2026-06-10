package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// postAgentFile is a small helper that POSTs to /agent-files/write and returns
// the recorder. expectedSha == "" means "create".
func postAgentFile(t *testing.T, b *Broker, path, content, expectedSha string) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"path":           path,
		"content":        content,
		"commit_message": "edit",
		"expected_sha":   expectedSha,
	})
	req := httptest.NewRequest(http.MethodPost, "/agent-files/write", bytes.NewReader(raw))
	// The write handler hard-requires a human-session actor; inject one the same
	// way withAuth does in production.
	req = requestWithActor(req, requestActor{Kind: requestActorKindHuman, Slug: "human", DisplayName: "Human"})
	rec := httptest.NewRecorder()
	b.handleAgentFileWrite(rec, req)
	return rec
}

func getAgentFile(t *testing.T, b *Broker, path string) (*httptest.ResponseRecorder, agentFileReadResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/agent-files/read?path="+path, nil)
	rec := httptest.NewRecorder()
	b.handleAgentFileRead(rec, req)
	var out agentFileReadResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal read body: %v (body=%s)", err, rec.Body.String())
		}
	}
	return rec, out
}

// TestAgentFileWriteThenRead covers the core editor round-trip: create a file
// via the write endpoint, then read it back with content + a real SHA + exists.
func TestAgentFileWriteThenRead(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	path := "agents/ceo/SOUL.md"
	if rec := postAgentFile(t, b, path, "# SOUL — @ceo\nbe excellent", ""); rec.Code != http.StatusOK {
		t.Fatalf("create write: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec, got := getAgentFile(t, b, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !got.Exists {
		t.Errorf("expected exists=true after create")
	}
	if got.SHA == "" {
		t.Errorf("expected a non-empty sha for a committed file")
	}
	if !strings.Contains(got.Content, "be excellent") {
		t.Errorf("content round-trip failed: %q", got.Content)
	}
}

// TestAgentFileReadMissSeedsOfficeUser verifies that reading a not-yet-written
// office/USER.md returns the deterministic seed (never a blank editor) with
// exists=false and an empty sha so the first save creates it.
func TestAgentFileReadMissSeedsOfficeUser(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	rec, got := getAgentFile(t, b, officeUserFileRel)
	if rec.Code != http.StatusOK {
		t.Fatalf("read miss: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got.Exists {
		t.Errorf("expected exists=false for an un-written file")
	}
	if got.SHA != "" {
		t.Errorf("expected empty sha for an un-written file, got %q", got.SHA)
	}
	if !strings.Contains(got.Content, "USER") {
		t.Errorf("expected seeded USER content, got %q", got.Content)
	}
}

// TestAgentFileReadMissSeedsAgentFromRoster verifies the agents/{slug}/*.md
// seed path: with a roster member present, a read-miss renders that member's
// deterministic SOUL so the editor opens with real text.
func TestAgentFileReadMissSeedsAgentFromRoster(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)
	b.members = []officeMember{{
		Slug:        "growth",
		Name:        "Growth Lead",
		Role:        "growth lead",
		Personality: "Relentless about pipeline.",
		Provider:    provider.ProviderBinding{Kind: "codex"},
	}}

	rec, got := getAgentFile(t, b, "agents/growth/SOUL.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("read miss: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got.Exists {
		t.Errorf("expected exists=false before any write")
	}
	if !strings.Contains(got.Content, "# SOUL — @growth") || !strings.Contains(got.Content, "Relentless about pipeline") {
		t.Errorf("expected seeded SOUL for the roster member, got %q", got.Content)
	}
}

// TestAgentFileWrite409OnStaleSHA verifies optimistic concurrency: a replace
// against a stale sha is rejected with 409 + the current sha and bytes.
func TestAgentFileWrite409OnStaleSHA(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)
	path := "agents/ceo/OPERATIONS.md"

	if rec := postAgentFile(t, b, path, "# OPERATIONS — @ceo\nv1", ""); rec.Code != http.StatusOK {
		t.Fatalf("create: %d (%s)", rec.Code, rec.Body.String())
	}
	_, first := getAgentFile(t, b, path)
	// Land a second edit so HEAD moves past `first.SHA`.
	if rec := postAgentFile(t, b, path, "# OPERATIONS — @ceo\nv2", first.SHA); rec.Code != http.StatusOK {
		t.Fatalf("second edit: %d (%s)", rec.Code, rec.Body.String())
	}
	// A save still using the stale first.SHA must 409.
	rec := postAgentFile(t, b, path, "# OPERATIONS — @ceo\nstale", first.SHA)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale save: want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	var conflict map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("unmarshal conflict: %v", err)
	}
	if conflict["current_sha"] == "" {
		t.Errorf("conflict missing current_sha: %v", conflict)
	}
	if cur, ok := conflict["current_content"].(string); !ok || !strings.Contains(cur, "v2") {
		t.Errorf("conflict current_content stale/missing: %v", conflict["current_content"])
	}
}

// TestAgentFileWriteRejectsNonAgentPath ensures the strict validator blocks any
// attempt to use this endpoint to write outside the agent-file allowlist.
func TestAgentFileWriteRejectsNonAgentPath(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	for _, bad := range []string{
		"team/people/ceo.md",       // wiki article subtree
		"agents/ceo/MEMORY.md",     // not a canonical file
		"agents/ceo/notebook/x.md", // notebook subtree
		"../etc/passwd",            // traversal
		"agents/ceo/../eng/SOUL.md",
	} {
		rec := postAgentFile(t, b, bad, "x", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("path %q: want 400, got %d (%s)", bad, rec.Code, rec.Body.String())
		}
	}
}

// TestAgentFileReadRejectsNonAgentPath mirrors the write guard for reads.
func TestAgentFileReadRejectsNonAgentPath(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	rec, _ := getAgentFile(t, b, "team/people/ceo.md")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-agent read path, got %d", rec.Code)
	}
}

// TestAgentFileWriteRequiresHumanActor verifies the write handler rejects a
// non-human (e.g. broker-token) actor — an agent must never rewrite an
// instruction file, since those feed the system prompt.
func TestAgentFileWriteRequiresHumanActor(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)

	raw, _ := json.Marshal(map[string]any{
		"path":    "agents/ceo/SOUL.md",
		"content": "# SOUL — @ceo\nagent self-edit",
	})
	// No actor in context (or a broker actor) must be rejected.
	for _, actor := range []*requestActor{nil, {Kind: requestActorKindBroker}} {
		req := httptest.NewRequest(http.MethodPost, "/agent-files/write", bytes.NewReader(raw))
		if actor != nil {
			req = requestWithActor(req, *actor)
		}
		rec := httptest.NewRecorder()
		b.handleAgentFileWrite(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("actor=%v: want 403, got %d (%s)", actor, rec.Code, rec.Body.String())
		}
	}
}

// TestHumanRouteAllowedAgentFiles locks the human/web token's access to the new
// endpoints: GET read and POST write are allowed; other methods are not.
func TestHumanRouteAllowedAgentFiles(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/agent-files/read", true},
		{http.MethodPost, "/agent-files/write", true},
		{http.MethodPost, "/agent-files/read", false},
		{http.MethodGet, "/agent-files/write", false},
		{http.MethodDelete, "/agent-files/write", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		if got := humanRouteAllowed(req); got != c.want {
			t.Errorf("humanRouteAllowed(%s %s) = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

func postGenerate(t *testing.T, b *Broker, path string, human bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"path": path, "hint": ""})
	req := httptest.NewRequest(http.MethodPost, "/agent-files/generate", bytes.NewReader(raw))
	if human {
		req = requestWithActor(req, requestActor{Kind: requestActorKindHuman, Slug: "human"})
	}
	rec := httptest.NewRecorder()
	b.handleAgentFileGenerate(rec, req)
	return rec
}

// TestAgentFileGenerateEndpoint covers the draft-authoring endpoint: a human
// gets the generated content, a non-human is rejected, a missing generator is
// 503, a bad path is 400, and a generator error surfaces as 500.
func TestAgentFileGenerateEndpoint(t *testing.T) {
	worker, _, _, teardown := newStartedWorker(t)
	defer teardown()
	b := brokerForTest(t, worker)
	b.SetGenerateAgentFileFn(func(_ context.Context, relPath, _ string) (string, error) {
		return "# SOUL — generated\nrich content for " + relPath, nil
	})

	// Happy path: human gets a draft, never committed.
	rec := postGenerate(t, b, "agents/growth/SOUL.md", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if c, _ := out["content"].(string); !strings.Contains(c, "rich content") {
		t.Errorf("expected generated content, got %v", out["content"])
	}
	// The file must NOT have been written (generate is review-only).
	if data, _ := worker.AgentFileRead("agents/growth/SOUL.md"); len(data) != 0 {
		t.Errorf("generate must not commit; file exists: %q", data)
	}

	// Non-human is rejected.
	if rec := postGenerate(t, b, "agents/growth/SOUL.md", false); rec.Code != http.StatusForbidden {
		t.Errorf("non-human generate: want 403, got %d", rec.Code)
	}
	// Bad path is rejected.
	if rec := postGenerate(t, b, "team/people/x.md", true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad path: want 400, got %d", rec.Code)
	}
	// Generator error surfaces as 500.
	b.SetGenerateAgentFileFn(func(_ context.Context, _, _ string) (string, error) {
		return "", errors.New("model unavailable")
	})
	if rec := postGenerate(t, b, "agents/growth/SOUL.md", true); rec.Code != http.StatusInternalServerError {
		t.Errorf("generator error: want 500, got %d", rec.Code)
	}
	// No generator wired -> 503.
	b.SetGenerateAgentFileFn(nil)
	if rec := postGenerate(t, b, "agents/growth/SOUL.md", true); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil generator: want 503, got %d", rec.Code)
	}
}

// TestGenerateAgentFileGating verifies the prose-only allowlist (SOUL/OPERATIONS
// + office USER) and the test stub short-circuit, without needing a real model.
func TestGenerateAgentFileGating(t *testing.T) {
	t.Setenv("WUPHF_AGENT_FILE_STUB", "# stub content")
	l := &Launcher{}
	ctx := context.Background()

	for _, ok := range []string{"agents/x/SOUL.md", "agents/x/OPERATIONS.md", officeUserFileRel} {
		got, err := l.GenerateAgentFileFromContext(ctx, ok, "")
		if err != nil || got != "# stub content" {
			t.Errorf("path %q: want stub content, got %q err=%v", ok, got, err)
		}
	}
	// Factual files are not AI-generatable.
	for _, bad := range []string{"agents/x/IDENTITY.md", "agents/x/TOOLS.md"} {
		if _, err := l.GenerateAgentFileFromContext(ctx, bad, ""); err == nil {
			t.Errorf("path %q: expected gating error, got nil", bad)
		}
	}
	// An invalid path is rejected before anything else.
	if _, err := l.GenerateAgentFileFromContext(ctx, "team/people/x.md", ""); err == nil {
		t.Error("invalid path must be rejected")
	}
}

// TestStripMarkdownFences guards the fence-stripping helper used on model output.
func TestStripMarkdownFences(t *testing.T) {
	cases := map[string]string{
		"# Title\nbody":             "# Title\nbody",
		"```markdown\n# Title\n```": "# Title",
		"```\n# Title\nbody\n```":   "# Title\nbody",
		"  \n# Title\n":             "# Title",
	}
	for in, want := range cases {
		if got := stripMarkdownFences(in); got != want {
			t.Errorf("stripMarkdownFences(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCommitAgentFileHumanRoundTrip exercises the repo-level human-write path:
// create, replace with the correct sha, and a stale-sha replace that must be
// rejected with ErrWikiSHAMismatch.
func TestCommitAgentFileHumanRoundTrip(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	rel := agentFileRel("ceo", "TOOLS")

	sha1, n, err := repo.CommitAgentFileHuman(ctx, rel, "# TOOLS — @ceo\nv1", "", "seed", HumanIdentity{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sha1 == "" || n == 0 {
		t.Fatalf("unexpected create result sha=%q n=%d", sha1, n)
	}
	// Correct-sha replace succeeds and advances HEAD.
	sha2, _, err := repo.CommitAgentFileHuman(ctx, rel, "# TOOLS — @ceo\nv2", sha1, "edit", HumanIdentity{})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if sha2 == "" || sha2 == sha1 {
		t.Fatalf("expected new sha after replace, got %q (was %q)", sha2, sha1)
	}
	// Stale-sha replace is rejected.
	if _, _, err := repo.CommitAgentFileHuman(ctx, rel, "# TOOLS — @ceo\nstale", sha1, "stale", HumanIdentity{}); !errors.Is(err, ErrWikiSHAMismatch) {
		t.Fatalf("want ErrWikiSHAMismatch on stale write, got %v", err)
	}
}
