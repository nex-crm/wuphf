package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fetchJSON GETs path with the broker bearer and decodes into out.
func fetchJSON(t *testing.T, b *Broker, path string, out any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+path, nil)
	if err != nil {
		t.Fatalf("build request %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// TestOfficeStats_MatchesListEndpoints is the C1 regression pin: the
// derived-stats endpoint must agree with the truth each list endpoint
// serves, bucketed with the same projection the board uses. If the
// stats computation ever drifts from the list endpoints (new lifecycle
// state, changed spec-task filter, request predicate change), this
// fails.
func TestOfficeStats_MatchesListEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: boots a real broker listener")
	}

	b := newTestBrokerForReview(t)

	// Roster: two agents + the human seat. ceo has a live "active"
	// snapshot; ada is idle.
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "ada", Name: "Ada"},
		{Slug: "human", Name: "You"},
	}
	b.activity = map[string]agentActivitySnapshot{
		"ceo": {Slug: "ceo", Status: "active", Activity: "tool_use"},
		"ada": {Slug: "ada", Status: "idle"},
	}

	now := time.Now().UTC()
	seedTask := func(id, title, taskType string, state LifecycleState, parent string) {
		task := teamTask{
			ID:            id,
			Title:         title,
			TaskType:      taskType,
			Channel:       "general",
			ParentIssueID: parent,
			CreatedAt:     now.Format(time.RFC3339),
		}
		b.tasks = append(b.tasks, task)
		if state != "" {
			if _, err := b.transitionLifecycleLocked(id, state, "stats seed"); err != nil {
				b.mu.Unlock()
				t.Fatalf("seed %s -> %s: %v", id, state, err)
			}
		}
	}
	seedTask("task-backlog", "Backlog spec", "issue", LifecycleStateIntake, "")
	seedTask("task-running", "Running spec", "issue", LifecycleStateRunning, "")
	seedTask("task-review", "Review spec", "issue", LifecycleStateReview, "")
	seedTask("task-blocked", "Blocked spec", "issue", LifecycleStateBlockedOnPRMerge, "")
	seedTask("task-decision", "Decision spec", "issue", LifecycleStateDecision, "")
	seedTask("task-done", "Done spec", "issue", LifecycleStateApproved, "")
	// Sub-task and non-spec execution task: visible to /tasks but NOT
	// board cards — must be excluded from the stats buckets.
	seedTask("task-sub", "Sub-task", "issue", LifecycleStateRunning, "task-running")
	seedTask("task-followup", "Follow up", "follow_up", LifecycleStateRunning, "")
	// Legacy task with bare status only (no lifecycle state).
	legacy := teamTask{ID: "task-legacy", Title: "Legacy open", TaskType: "issue", Channel: "general", CreatedAt: now.Format(time.RFC3339)}
	legacy.status = "open"
	b.tasks = append(b.tasks, legacy)

	b.requests = []humanInterview{
		{ID: "req-blocking", From: "ceo", Channel: "general", Question: "Approve spend?", Kind: "approval", Blocking: true},
		{ID: "req-notice", From: "ada", Channel: "general", Question: "FYI", Kind: "notice"},
		{ID: "req-answered", From: "ceo", Channel: "general", Question: "Old", Kind: "approval", Blocking: true, Status: "answered"},
	}
	b.mu.Unlock()

	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	var stats OfficeStats
	fetchJSON(t, b, "/office/stats", &stats)

	// ── Tasks: derive truth from the same list endpoint the board reads.
	var taskList TaskListResponse
	fetchJSON(t, b, "/tasks?all_channels=true&include_done=true&viewer_slug=human", &taskList)
	var want OfficeStatsTasks
	for i := range taskList.Tasks {
		task := &taskList.Tasks[i]
		if !isBoardSpecTask(task) {
			continue
		}
		switch taskBoardStage(task) {
		case boardStageBacklog:
			want.Backlog++
		case boardStageInProgress:
			want.Active++
		case boardStageBlocked:
			want.Blocked++
		case boardStageNeedsHuman:
			want.NeedsHuman++
		case boardStageDone:
			want.Done++
		case boardStageArchive:
			want.Archive++
		}
		if taskInReview(task) {
			want.Review++
		}
	}
	if stats.Tasks != want {
		t.Fatalf("stats.Tasks = %+v, want list-derived %+v", stats.Tasks, want)
	}
	// Sanity-pin absolute values so the test cannot pass vacuously on
	// an empty bucket set: backlog = intake + legacy-open, active =
	// running + review, etc.
	expected := OfficeStatsTasks{Backlog: 2, Active: 2, Blocked: 1, Review: 1, NeedsHuman: 1, Done: 1}
	if stats.Tasks != expected {
		t.Fatalf("stats.Tasks = %+v, want seeded %+v", stats.Tasks, expected)
	}

	// ── Requests: derive truth from /requests?scope=all.
	var reqList struct {
		Requests []humanInterview `json:"requests"`
	}
	fetchJSON(t, b, "/requests?scope=all&viewer_slug=human", &reqList)
	var wantReq OfficeStatsRequests
	for _, req := range reqList.Requests {
		if !requestIsActive(req) {
			continue
		}
		if req.Blocking || req.Required {
			wantReq.Blocking++
		} else {
			wantReq.Notices++
		}
	}
	if stats.Requests != wantReq {
		t.Fatalf("stats.Requests = %+v, want list-derived %+v", stats.Requests, wantReq)
	}
	if (stats.Requests != OfficeStatsRequests{Blocking: 1, Notices: 1}) {
		t.Fatalf("stats.Requests = %+v, want seeded {Blocking:1 Notices:1}", stats.Requests)
	}

	// ── Inbox attention: derive truth from /inbox/items with the badge
	// predicate.
	var inbox struct {
		Items []InboxItem `json:"items"`
	}
	fetchJSON(t, b, "/inbox/items?filter=all", &inbox)
	wantAttention := 0
	for _, item := range inbox.Items {
		if inboxItemNeedsAttention(item) {
			wantAttention++
		}
	}
	if stats.InboxAttention != wantAttention {
		t.Fatalf("stats.InboxAttention = %d, want inbox-derived %d", stats.InboxAttention, wantAttention)
	}
	if wantAttention == 0 {
		t.Fatal("inbox-derived attention count is 0; seed should have produced attention items")
	}

	// ── Agents: derive truth from /office-members live status.
	var memberList struct {
		Members []struct {
			Slug   string `json:"slug"`
			Status string `json:"status"`
		} `json:"members"`
	}
	fetchJSON(t, b, "/office-members", &memberList)
	wantActive := 0
	for _, m := range memberList.Members {
		if m.Slug == "" || m.Slug == "human" || m.Slug == "you" || m.Slug == "system" {
			continue
		}
		if m.Status != "" && m.Status != "idle" && m.Status != "offline" {
			wantActive++
		}
	}
	if stats.AgentsActive != wantActive {
		t.Fatalf("stats.AgentsActive = %d, want members-derived %d", stats.AgentsActive, wantActive)
	}
	if stats.AgentsActive != 1 {
		t.Fatalf("stats.AgentsActive = %d, want seeded 1 (ceo active, ada idle)", stats.AgentsActive)
	}

	// No wiki worker in this fixture: count must degrade to zero, not error.
	if stats.WikiArticles != 0 {
		t.Fatalf("stats.WikiArticles = %d, want 0 without a wiki worker", stats.WikiArticles)
	}
}

// TestOfficeStats_MethodNotAllowed pins the GET-only contract.
func TestOfficeStats_MethodNotAllowed(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/office/stats", b.Addr()), nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /office/stats: want 405, got %d", resp.StatusCode)
	}
}

// TestCountArticles_MatchesBuildCatalog pins the wiki half of C1: the
// stats count walks with the exact filter /wiki/catalog applies, so the
// wiki home "N articles" can never disagree with the catalog list.
func TestCountArticles_MatchesBuildCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: needs git for repo fixture")
	}
	root := t.TempDir()
	repo := NewRepoAt(root, filepath.Join(t.TempDir(), "bak"))
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	commits := []struct{ path, body string }{
		{"team/people/nazz.md", "# Nazz\n\nFounder.\n"},
		{"team/accounts/corti.md", "# Corti\n\nAccount brief.\n"},
		{"team/playbooks/renewals.md", "# Renewals\n\nPlaybook.\n"},
		// Archived tombstone — excluded from both catalog and count.
		{"team/old/retired.md", "---\narchived: true\n---\n# Retired\n"},
		// Skills + inbox content — excluded from both.
		{"team/skills/renewal-outreach.md", "# SKILL\n"},
		{"team/inbox/raw/episode.md", "# Episode\n"},
	}
	for _, c := range commits {
		if _, _, err := repo.Commit(ctx, "ceo", c.path, c.body, "create", "seed "+c.path); err != nil {
			t.Fatalf("Commit %s: %v", c.path, err)
		}
	}
	// Non-markdown asset written directly to disk (Commit only accepts
	// .md) — excluded from both catalog and count.
	assetsDir := filepath.Join(root, "team", "assets")
	if err := os.MkdirAll(assetsDir, 0o700); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "logo.txt"), []byte("logo\n"), 0o600); err != nil {
		t.Fatalf("write logo.txt: %v", err)
	}

	entries, err := repo.BuildCatalog(ctx, "", nil, false)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	count, err := repo.CountArticles()
	if err != nil {
		t.Fatalf("CountArticles: %v", err)
	}
	if count != len(entries) {
		t.Fatalf("CountArticles = %d, BuildCatalog entries = %d — filters drifted", count, len(entries))
	}
	if count != 3 {
		t.Fatalf("CountArticles = %d, want 3 curated articles", count)
	}
}
