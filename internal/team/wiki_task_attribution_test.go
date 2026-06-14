package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveArticleAttribution_ByTaskArtifactPointer(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "OFFICE-1", Title: "Q2 pricing launch", Owner: "revops", Channel: "general", Artifact: "team/briefs/launch.md"},
		{ID: "OFFICE-2", Title: "Other", Owner: "ceo", Channel: "general"},
	}
	b.mu.Unlock()

	att, ok := b.resolveArticleAttribution("team/briefs/launch.md")
	if !ok {
		t.Fatalf("expected attribution for declared artifact path")
	}
	if att.TaskID != "OFFICE-1" || att.TaskTitle != "Q2 pricing launch" || att.Owner != "revops" {
		t.Fatalf("wrong attribution: %+v", att)
	}

	if _, ok := b.resolveArticleAttribution("team/unknown.md"); ok {
		t.Fatalf("unknown ref should not resolve")
	}
	if _, ok := b.resolveArticleAttribution("  "); ok {
		t.Fatalf("blank ref should not resolve")
	}
}

func TestHandleArticleAttribution_NullWhenNone(t *testing.T) {
	b := newTestBroker(t)

	rec := httptest.NewRecorder()
	b.handleArticleAttribution(rec, httptest.NewRequest(http.MethodGet, "/article-attribution?ref=ra_missing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	var resp struct {
		Attribution *ArticleAttribution `json:"attribution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Attribution != nil {
		t.Fatalf("want null attribution, got %+v", resp.Attribution)
	}
}

func TestHandleArticleAttribution_LivePath(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "OFFICE-7", Title: "Write playbook", Owner: "ops", Channel: "general", Artifact: "team/playbooks/p.md"},
	}
	b.mu.Unlock()

	rec := httptest.NewRecorder()
	b.handleArticleAttribution(rec, httptest.NewRequest(http.MethodGet, "/article-attribution?ref=team/playbooks/p.md", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Attribution *ArticleAttribution `json:"attribution"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Attribution == nil || resp.Attribution.TaskID != "OFFICE-7" {
		t.Fatalf("wrong attribution: %+v", resp.Attribution)
	}
}
