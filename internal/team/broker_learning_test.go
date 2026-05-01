package team

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newLearningTestServer(t *testing.T) (*httptest.Server, *Broker, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init wiki repo: %v", err)
	}
	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/learning/record", b.requireAuth(b.handleLearningRecord))
	mux.HandleFunc("/learning/search", b.requireAuth(b.handleLearningSearch))
	srv := httptest.NewServer(mux)
	return srv, b, func() {
		srv.Close()
		cancel()
		worker.Stop()
		<-worker.Done()
	}
}

func TestBrokerLearningHandlersEndToEnd(t *testing.T) {
	srv, b, teardown := newLearningTestServer(t)
	defer teardown()

	body, err := json.Marshal(map[string]any{
		"type":          "pitfall",
		"key":           "active-skill-filter",
		"insight":       "Filter inactive skills before prompt injection.",
		"confidence":    8,
		"source":        "observed",
		"scope":         "playbook:ship-pr",
		"playbook_slug": "ship-pr",
		"created_by":    "codex",
	})
	if err != nil {
		t.Fatalf("marshal learning payload: %v", err)
	}
	req, err := authReq(http.MethodPost, srv.URL+"/learning/record", bytes.NewReader(body), b.Token())
	if err != nil {
		t.Fatalf("build record request: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("record learning: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("record learning status = %d", res.StatusCode)
	}
	res.Body.Close()

	req, err = authReq(http.MethodGet, srv.URL+"/learning/search?playbook_slug=ship-pr", nil, b.Token())
	if err != nil {
		t.Fatalf("build search request: %v", err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("search learning: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("search learning status = %d", res.StatusCode)
	}
	var payload struct {
		Learnings []LearningSearchResult `json:"learnings"`
		Count     int                    `json:"count"`
		WikiPath  string                 `json:"wiki_path"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	if payload.Count != 1 || len(payload.Learnings) != 1 {
		t.Fatalf("learning response count = %d len = %d", payload.Count, len(payload.Learnings))
	}
	if payload.Learnings[0].Key != "active-skill-filter" || payload.Learnings[0].EffectiveConfidence != 8 {
		t.Fatalf("unexpected learning payload: %+v", payload.Learnings[0])
	}
	if payload.WikiPath != TeamLearningsPagePath {
		t.Fatalf("wiki_path = %q, want %q", payload.WikiPath, TeamLearningsPagePath)
	}
}

func TestBrokerLearningRecordMapsBackendFailuresToServiceUnavailable(t *testing.T) {
	b := newTestBroker(t)
	b.SetTeamLearningLog(NewLearningLog(nil))
	body, err := json.Marshal(map[string]any{
		"type":       "pitfall",
		"key":        "backend-down",
		"insight":    "Backend write failures should be retriable by callers.",
		"confidence": 8,
		"source":     "observed",
		"scope":      "repo",
		"created_by": "codex",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/learning/record", bytes.NewReader(body))
	res := httptest.NewRecorder()

	b.handleLearningRecord(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusServiceUnavailable, res.Body.String())
	}
}
