package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newNotebookTestServer wires a full httptest server with the auth-gated
// notebook routes + a running worker against a temp repo.
func newNotebookTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.requireAuth(b.handleNotebookWrite))
	mux.HandleFunc("/notebook/read", b.requireAuth(b.handleNotebookRead))
	mux.HandleFunc("/notebook/list", b.requireAuth(b.handleNotebookList))
	mux.HandleFunc("/notebook/catalog", b.requireAuth(b.handleNotebookCatalog))
	mux.HandleFunc("/notebook/search", b.requireAuth(b.handleNotebookSearch))
	mux.HandleFunc("/notebook/visual-artifacts", b.requireAuth(b.handleNotebookVisualArtifacts))
	mux.HandleFunc("/notebook/visual-artifacts/", b.requireAuth(b.handleNotebookVisualArtifactSubpath))
	mux.HandleFunc("/wiki/article", b.requireAuth(b.handleWikiArticle))
	mux.HandleFunc("/wiki/visual", b.requireAuth(b.handleWikiVisualArtifact))
	srv := httptest.NewServer(mux)

	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
	}
}

func authReq(method, url string, body io.Reader, token string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func TestBrokerNotebookHandlersEndToEnd(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()

	// Write
	writeBody, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/2026-04-20-retro.md",
		"content":        "# Retro\n\nDraft.\n",
		"mode":           "create",
		"commit_message": "draft retro",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(writeBody), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}
	var writeRes map[string]any
	_ = json.NewDecoder(res.Body).Decode(&writeRes)
	res.Body.Close()
	if writeRes["commit_sha"] == "" {
		t.Fatalf("expected commit_sha in response: %+v", writeRes)
	}

	// Read
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/read?slug=pm&path=agents/pm/notebook/2026-04-20-retro.md", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read status %d: %s", res.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "Draft") {
		t.Fatalf("unexpected body: %q", string(body))
	}

	// List
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/list?slug=pm", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listRes struct {
		Entries []map[string]any `json:"entries"`
	}
	_ = json.NewDecoder(res.Body).Decode(&listRes)
	res.Body.Close()
	if len(listRes.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(listRes.Entries))
	}

	// Search
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/search?slug=pm&q=Draft", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searchRes struct {
		Hits []map[string]any `json:"hits"`
	}
	_ = json.NewDecoder(res.Body).Decode(&searchRes)
	res.Body.Close()
	if len(searchRes.Hits) == 0 {
		t.Fatal("expected at least one search hit")
	}
}

func TestBrokerNotebookVisualArtifactCreateReadPromote(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()

	writeBody, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/retro.md",
		"content":        "# Retro\n\nDraft source.\n",
		"mode":           "create",
		"commit_message": "draft retro",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(writeBody), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("notebook write: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("notebook write status=%d", res.StatusCode)
	}

	createBody, _ := json.Marshal(map[string]any{
		"slug":                 "pm",
		"title":                "Retro visual plan",
		"summary":              "A visual review of the retro plan.",
		"html":                 "<!doctype html><html><body><h1>Retro visual</h1><button>Approve</button></body></html>",
		"source_markdown_path": "agents/pm/notebook/retro.md",
		"related_receipt_ids":  []string{"rcpt-1", "rcpt-1", "rcpt-2"},
	})
	req, _ = authReq(http.MethodPost, srv.URL+"/notebook/visual-artifacts", bytes.NewReader(createBody), token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create visual artifact: %v", err)
	}
	var created struct {
		Artifact  RichArtifact `json:"artifact"`
		CommitSHA string       `json:"commit_sha"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", res.StatusCode)
	}
	if created.Artifact.ID == "" || created.Artifact.TrustLevel != richArtifactTrustDraft || created.CommitSHA == "" {
		t.Fatalf("unexpected artifact response: %+v", created)
	}
	if len(created.Artifact.RelatedReceiptIDs) != 2 {
		t.Fatalf("receipt ids should be deduped: %+v", created.Artifact.RelatedReceiptIDs)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/visual-artifacts?source_path=agents/pm/notebook/retro.md", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list visual artifacts: %v", err)
	}
	var list struct {
		Artifacts []RichArtifact `json:"artifacts"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	_ = res.Body.Close()
	if len(list.Artifacts) != 1 || list.Artifacts[0].ID != created.Artifact.ID {
		t.Fatalf("expected created artifact in list, got %+v", list.Artifacts)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/visual-artifacts/"+created.Artifact.ID, nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read visual artifact: %v", err)
	}
	var detail struct {
		Artifact RichArtifact `json:"artifact"`
		HTML     string       `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	_ = res.Body.Close()
	if !strings.Contains(detail.HTML, "Retro visual") {
		t.Fatalf("expected html body, got %q", detail.HTML)
	}

	promoteBody, _ := json.Marshal(map[string]any{
		"target_wiki_path": "team/drafts/retro-visual.md",
		"markdown_summary": "# Retro visual plan\n\nA reviewed summary.\n",
	})
	req, _ = authReq(http.MethodPost, srv.URL+"/notebook/visual-artifacts/"+created.Artifact.ID+"/promote", bytes.NewReader(promoteBody), token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("promote visual artifact: %v", err)
	}
	var promoted struct {
		Artifact  RichArtifact `json:"artifact"`
		CommitSHA string       `json:"commit_sha"`
	}
	if err := json.NewDecoder(res.Body).Decode(&promoted); err != nil {
		t.Fatalf("decode promote: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("promote status=%d", res.StatusCode)
	}
	if promoted.Artifact.TrustLevel != richArtifactTrustPromoted || promoted.Artifact.PromotedWikiPath != "team/drafts/retro-visual.md" {
		t.Fatalf("unexpected promoted artifact: %+v", promoted.Artifact)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/wiki/article?path=team/drafts/retro-visual.md", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read promoted wiki article: %v", err)
	}
	var article ArticleMeta
	if err := json.NewDecoder(res.Body).Decode(&article); err != nil {
		t.Fatalf("decode article: %v", err)
	}
	_ = res.Body.Close()
	if !strings.Contains(article.Content, created.Artifact.ID) || !strings.Contains(article.Content, "Visual Artifact Provenance") {
		t.Fatalf("promoted article missing provenance: %s", article.Content)
	}

	req, _ = authReq(http.MethodGet, srv.URL+"/wiki/visual?path=team/drafts/retro-visual.md", nil, token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("read wiki visual: %v", err)
	}
	var visual struct {
		Artifact RichArtifact `json:"artifact"`
		HTML     string       `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&visual); err != nil {
		t.Fatalf("decode wiki visual: %v", err)
	}
	_ = res.Body.Close()
	if visual.Artifact.ID != created.Artifact.ID || !strings.Contains(visual.HTML, "Retro visual") {
		t.Fatalf("unexpected wiki visual response: %+v html=%q", visual.Artifact, visual.HTML)
	}
}

func TestBrokerNotebookVisualArtifactHumanCreateUsesSessionSlug(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	sessionToken, _, err := b.acceptHumanInvite(token, "Mira", "browser")
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	createBody, _ := json.Marshal(map[string]any{
		"slug":    "pm",
		"title":   "Human visual plan",
		"summary": "Created from a team-member session.",
		"html":    "<!doctype html><html><body><h1>Human visual</h1></body></html>",
	})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/notebook/visual-artifacts", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: humanSessionCookie, Value: sessionToken})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create visual artifact as human: %v", err)
	}
	var created struct {
		Artifact RichArtifact `json:"artifact"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d artifact=%+v", res.StatusCode, created.Artifact)
	}
	if created.Artifact.CreatedBy != "mira" {
		t.Fatalf("CreatedBy = %q, want authenticated human slug mira", created.Artifact.CreatedBy)
	}
}

func TestBrokerNotebookVisualArtifactRejectsUnsafeInput(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()

	for _, tc := range []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "title NUL",
			body: map[string]any{
				"slug":  "pm",
				"title": "Bad\x00Title",
				"html":  "<!doctype html><html><body><h1>Bad</h1></body></html>",
			},
			want: "title must not contain NUL",
		},
		{
			name: "external script",
			body: map[string]any{
				"slug":  "pm",
				"title": "External script",
				"html":  `<!doctype html><html><body><script src="https://example.com/app.js"></script></body></html>`,
			},
			want: "external script src is not allowed",
		},
		{
			name: "css import",
			body: map[string]any{
				"slug":  "pm",
				"title": "CSS import",
				"html":  `<!doctype html><html><head><style>@import url("https://example.com/style.css");</style></head><body></body></html>`,
			},
			want: "css @import is not allowed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			req, _ := authReq(http.MethodPost, srv.URL+"/notebook/visual-artifacts", bytes.NewReader(raw), token)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			data, _ := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 body=%s", res.StatusCode, string(data))
			}
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("body %q missing %q", string(data), tc.want)
			}
		})
	}
}

func TestBrokerNotebookWriteAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{"slug": "pm", "path": "agents/pm/notebook/x.md"})
	res, err := http.Post(srv.URL+"/notebook/write", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/read?path=agents/pm/notebook/x.md")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/list?slug=pm")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookCatalogAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/catalog")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookSearchAuthRequired(t *testing.T) {
	srv, _, teardown := newNotebookTestServer(t)
	defer teardown()
	res, err := http.Get(srv.URL + "/notebook/search?slug=pm&q=x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookWriteRejectsBadJSON(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader([]byte("{not-json")), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookWriteMethodNotAllowed(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/write", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadMethodNotAllowed(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/read", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListRequiresSlug(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/list", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookListEmptyReturnsEmptyArray(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/list?slug=nobody", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	var parsed struct {
		Entries []map[string]any `json:"entries"`
	}
	_ = json.NewDecoder(res.Body).Decode(&parsed)
	if parsed.Entries == nil {
		t.Fatal("expected [] not null")
	}
	if len(parsed.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(parsed.Entries))
	}
}

func TestBrokerNotebookCatalogIncludesRosterAgentsWithoutEntries(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "lead", CreatedAt: "2026-04-20T10:00:00Z"},
		{Slug: "pm", Name: "PM", Role: "product", CreatedAt: "2026-04-20T10:01:00Z"},
	}
	b.mu.Unlock()

	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/catalog", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}
	var parsed struct {
		Agents []struct {
			AgentSlug string           `json:"agent_slug"`
			Entries   []map[string]any `json:"entries"`
			Total     int              `json:"total"`
		} `json:"agents"`
		TotalAgents  int `json:"total_agents"`
		TotalEntries int `json:"total_entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if parsed.TotalAgents != 2 || len(parsed.Agents) != 2 {
		t.Fatalf("expected two roster shelves, got total=%d agents=%+v", parsed.TotalAgents, parsed.Agents)
	}
	if parsed.TotalEntries != 0 {
		t.Fatalf("expected no entries, got %d", parsed.TotalEntries)
	}
	for _, agent := range parsed.Agents {
		if agent.Total != 0 || len(agent.Entries) != 0 {
			t.Fatalf("expected blank shelf for %s, got %+v", agent.AgentSlug, agent)
		}
	}
}

func TestEnsureNotebookDirsForRosterCreatesBlankShelves(t *testing.T) {
	_, b, teardown := newNotebookTestServer(t)
	defer teardown()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "PM"},
	}
	b.mu.Unlock()

	b.ensureNotebookDirsForRoster()

	root := b.WikiWorker().Repo().Root()
	for _, slug := range []string{"ceo", "pm"} {
		marker := filepath.Join(root, "agents", slug, "notebook", ".gitkeep")
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("expected notebook shelf marker for %s: %v", slug, err)
		}
	}
}

func TestBrokerNotebookWriteRecordsTaskMemoryCapture(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:        "task-1",
		Channel:   "general",
		Title:     "Research passport process",
		TaskType:  "process_research",
		status:    "in_progress",
		CreatedBy: "ceo",
		Owner:     "pm",
		CreatedAt: now,
		UpdatedAt: now,
	}}
	syncTaskMemoryWorkflow(&b.tasks[0], now)
	b.mu.Unlock()

	writeBody, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"task_id":        "task-1",
		"path":           "agents/pm/notebook/passport.md",
		"content":        "# Passport\n\nReusable process notes.\n",
		"mode":           "create",
		"commit_message": "capture passport process",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(writeBody), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	wf := b.tasks[0].MemoryWorkflow
	if wf == nil || wf.Capture.CompletedAt == "" {
		t.Fatalf("expected capture workflow to be satisfied, got %+v", wf)
	}
	if len(wf.Captures) != 1 || wf.Captures[0].Path != "agents/pm/notebook/passport.md" {
		t.Fatalf("expected notebook capture artifact, got %+v", wf.Captures)
	}
}

func TestBrokerNotebookSearchAllRecordsTaskMemoryLookup(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "pm", Name: "PM"}, {Slug: "ceo", Name: "CEO"}}
	b.tasks = []teamTask{{
		ID:        "task-1",
		Channel:   "general",
		Title:     "Research passport process",
		TaskType:  "process_research",
		status:    "in_progress",
		CreatedBy: "ceo",
		Owner:     "pm",
		CreatedAt: now,
		UpdatedAt: now,
	}}
	syncTaskMemoryWorkflow(&b.tasks[0], now)
	b.mu.Unlock()
	if _, _, err := b.WikiWorker().NotebookWrite(context.Background(), "pm", "agents/pm/notebook/passport.md", "# Passport\n\nRenewal evidence.\n", "create", "capture passport"); err != nil {
		t.Fatalf("seed notebook: %v", err)
	}

	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/search?slug=all&q=Renewal&task_id=task-1&actor=pm", nil, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 200, got %d: %s", res.StatusCode, string(body))
	}
	var parsed struct {
		Hits []WikiSearchHit `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(parsed.Hits) != 1 {
		t.Fatalf("expected one hit, got %+v", parsed.Hits)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	wf := b.tasks[0].MemoryWorkflow
	if wf == nil || wf.Lookup.Query != "Renewal" || len(wf.Citations) != 1 {
		t.Fatalf("expected lookup workflow citation, got %+v", wf)
	}
	if wf.Citations[0].Path != "agents/pm/notebook/passport.md" {
		t.Fatalf("expected notebook citation path, got %+v", wf.Citations[0])
	}
}

func TestBrokerNotebookSearchRejectsMissingTaskBeforeDiscovery(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	corruptAgentsDir(t, b.WikiWorker().Repo())

	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/search?slug=all&q=Renewal&task_id=missing", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 404 before notebook discovery, got %d: %s", res.StatusCode, string(body))
	}
}

func TestBrokerNotebookSearchAllSurfacesDiscoveryFailure(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	corruptAgentsDir(t, b.WikiWorker().Repo())

	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/search?slug=all&q=Renewal", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for notebook discovery failure, got %d: %s", res.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "read agents dir") {
		t.Fatalf("expected discovery error in response, got %s", string(body))
	}
}

func corruptAgentsDir(t *testing.T, repo *Repo) {
	t.Helper()
	agentsPath := filepath.Join(repo.Root(), "agents")
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := os.RemoveAll(agentsPath); err != nil {
		t.Fatalf("remove agents dir: %v", err)
	}
	if err := os.WriteFile(agentsPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write agents file: %v", err)
	}
}

func TestEnsureBridgedMemberInitializesNotebookAfterUnlock(t *testing.T) {
	_, b, teardown := newNotebookTestServer(t)
	defer teardown()

	done := make(chan error, 1)
	go func() {
		done <- b.EnsureBridgedMember("openclaw-bot", "OpenClaw Bot", "openclaw")
	}()

	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("test deadline unavailable")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatal("test deadline exceeded")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ensure bridged member: %v", err)
		}
	case <-time.After(remaining):
		t.Fatal("EnsureBridgedMember deadlocked while initializing notebook shelves")
	}

	marker := filepath.Join(b.WikiWorker().Repo().Root(), "agents", "openclaw-bot", "notebook", ".gitkeep")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected bridged member notebook shelf marker: %v", err)
	}
}

func TestBrokerNotebookWriteSlugMismatchReturns403(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/ceo/notebook/x.md",
		"content":        "# x\n",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("expected 403, got %d: %s", res.StatusCode, string(b))
	}
}

func TestBrokerNotebookWriteValidationReturns400(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/x.txt", // not markdown
		"content":        "x",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadBadSlugHint(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	// slug hint != path owner — should 400
	req, _ := authReq(http.MethodGet,
		srv.URL+"/notebook/read?slug=pm&path=agents/ceo/notebook/x.md", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookReadMissingPath(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/read", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookSearchRequiresSlugAndPattern(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	// missing slug
	req, _ := authReq(http.MethodGet, srv.URL+"/notebook/search?q=x", nil, b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing slug, got %d", res.StatusCode)
	}
	// missing q
	req, _ = authReq(http.MethodGet, srv.URL+"/notebook/search?slug=pm", nil, b.Token())
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing q, got %d", res.StatusCode)
	}
}

func TestBrokerNotebookServiceUnavailable(t *testing.T) {
	// Block wiki init so ensureWikiWorker (called by requireWikiWorker
	// inside each handler) cannot bring the worker up. Pre-fix this test
	// relied on "wikiWorker is nil" being a permanent state; post-fix the
	// handlers retry init, so we have to actively prevent success to
	// observe the 503 path.
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)
	t.Setenv("WUPHF_MEMORY_BACKEND", "markdown")
	if err := os.WriteFile(filepath.Join(home, ".wuphf"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	b := newTestBroker(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/notebook/write", b.handleNotebookWrite)
	mux.HandleFunc("/notebook/read", b.handleNotebookRead)
	mux.HandleFunc("/notebook/list", b.handleNotebookList)
	mux.HandleFunc("/notebook/catalog", b.handleNotebookCatalog)
	mux.HandleFunc("/notebook/search", b.handleNotebookSearch)
	mux.HandleFunc("/notebook/visual-artifacts", b.handleNotebookVisualArtifacts)
	mux.HandleFunc("/notebook/visual-artifacts/", b.handleNotebookVisualArtifactSubpath)
	mux.HandleFunc("/wiki/visual", b.handleWikiVisualArtifact)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		method string
		url    string
	}{
		{http.MethodPost, "/notebook/write"},
		{http.MethodGet, "/notebook/read?path=agents/pm/notebook/x.md"},
		{http.MethodGet, "/notebook/list?slug=pm"},
		{http.MethodGet, "/notebook/catalog"},
		{http.MethodGet, "/notebook/search?slug=pm&q=x"},
		{http.MethodGet, "/notebook/visual-artifacts"},
		{http.MethodGet, "/notebook/visual-artifacts/ra_0123456789abcdef"},
		{http.MethodGet, "/wiki/visual?path=team/drafts/x.md"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.url, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.url, err)
		}
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s %s expected 503, got %d", tc.method, tc.url, res.StatusCode)
		}
		res.Body.Close()
	}
}

// TestBrokerNotebookSSEEvent subscribes via SubscribeNotebookEvents and
// confirms a write publishes on the right channel (not wiki).
func TestBrokerNotebookSSEEvent(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	nbCh, unsub := b.SubscribeNotebookEvents(16)
	defer unsub()
	wikiCh, unsubWiki := b.SubscribeWikiEvents(16)
	defer unsubWiki()

	body, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"path":           "agents/pm/notebook/x.md",
		"content":        "# x\n",
		"mode":           "create",
		"commit_message": "m",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/write", bytes.NewReader(body), b.Token())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	select {
	case evt := <-nbCh:
		if evt.Slug != "pm" || evt.Path != "agents/pm/notebook/x.md" || evt.CommitSHA == "" {
			t.Fatalf("unexpected event: %+v", evt)
		}
	case <-context.Background().Done():
	}
	// Drain the wiki channel; it should NOT have fired.
	select {
	case evt := <-wikiCh:
		t.Fatalf("wiki channel fired unexpectedly: %+v", evt)
	default:
	}
}
