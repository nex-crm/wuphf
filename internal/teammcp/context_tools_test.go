package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

func TestContextLookupMarkdownReturnsTypedNotebookAndWikiCitations(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendMarkdown)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	var workflowCalled bool
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notebook/catalog":
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": []map[string]any{{"agent_slug": "pm"}}})
		case "/notebook/search":
			if r.URL.Query().Get("slug") != "pm" || r.URL.Query().Get("q") != "passport" {
				t.Fatalf("unexpected notebook search query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": []map[string]any{{
				"path": "agents/pm/notebook/passport.md", "line": 3, "snippet": "passport checklist from prior run",
			}}})
		case "/wiki/search":
			if r.URL.Query().Get("pattern") != "passport" {
				t.Fatalf("unexpected wiki search query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hits": []map[string]any{{
				"path": "team/processes/passport.md", "line": 9, "snippet": "canonical passport process note",
			}}})
		case "/tasks/memory-workflow":
			workflowCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	res, data, err := handleContextLookup(context.Background(), nil, ContextLookupArgs{
		Query: "passport", TaskID: "task-1", TaskType: "research", Limit: 2,
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("tool error: %s", toolErrorText(res))
	}
	result := data.(ContextLookupResult)
	if !workflowCalled || result.Workflow == nil || !result.Workflow.Updated {
		t.Fatalf("expected workflow update, got called=%v workflow=%+v", workflowCalled, result.Workflow)
	}
	if len(result.Citations) != 2 {
		t.Fatalf("expected notebook + wiki citation, got %+v", result.Citations)
	}
	corpuses := []string{result.Citations[0].Corpus, result.Citations[1].Corpus}
	if !slices.Contains(corpuses, "notebook") || !slices.Contains(corpuses, "wiki") {
		t.Fatalf("expected notebook and wiki corpuses, got %+v", result.Citations)
	}
	for _, citation := range result.Citations {
		if citation.Snippet == "" || citation.Path == "" || citation.Line == 0 {
			t.Fatalf("expected typed citation fields from broker JSON, got %+v", citation)
		}
	}
}

func TestContextLookupGBrainMapsTypedCitationFields(t *testing.T) {
	oldStatus := contextResolveMemoryBackendStatus
	oldQuery := contextQuerySharedMemory
	t.Cleanup(func() {
		contextResolveMemoryBackendStatus = oldStatus
		contextQuerySharedMemory = oldQuery
	})
	contextResolveMemoryBackendStatus = func() team.MemoryBackendStatus {
		return team.MemoryBackendStatus{SelectedKind: config.MemoryBackendGBrain, SelectedLabel: "GBrain", ActiveKind: config.MemoryBackendGBrain, ActiveLabel: "GBrain"}
	}
	score := 0.87
	stale := true
	contextQuerySharedMemory = func(context.Context, string, int) ([]team.ScopedMemoryHit, error) {
		return []team.ScopedMemoryHit{{
			Scope: "shared", Backend: config.MemoryBackendGBrain, Identifier: "people/nazz", Title: "Nazz", Snippet: "typed chunk",
			Slug: "people/nazz", PageID: 42, ChunkID: 7, ChunkIndex: 3, Source: "compiled_truth", Score: &score, Stale: &stale,
		}}, nil
	}

	_, data, err := handleContextLookup(context.Background(), nil, ContextLookupArgs{Query: "nazz", IncludeShared: true})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	citation := data.(ContextLookupResult).Citations[0]
	if citation.Slug != "people/nazz" || citation.PageID != 42 || citation.ChunkID != 7 || citation.ChunkIndex != 3 || citation.Source != "compiled_truth" {
		t.Fatalf("missing gbrain typed fields: %+v", citation)
	}
	if citation.Score == nil || *citation.Score != score || citation.Stale == nil || !*citation.Stale {
		t.Fatalf("missing gbrain score/stale: %+v", citation)
	}
}

func TestContextLookupInactiveBackendReturnsTypedEmptyResult(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendNone)
	res, data, err := handleContextLookup(context.Background(), nil, ContextLookupArgs{Query: "anything"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("inactive backend should be typed data, not tool error: %s", toolErrorText(res))
	}
	result := data.(ContextLookupResult)
	if result.Status.OK || result.Status.Code != "backend_disabled" {
		t.Fatalf("expected backend_disabled status, got %+v", result.Status)
	}
	if len(result.Citations) != 0 || len(result.PartialErrors) == 0 {
		t.Fatalf("expected empty citations plus partial status, got %+v", result)
	}
}

func TestContextCaptureWritesNotebookAndUpdatesWorkflow(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendMarkdown)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	oldNow := contextNow
	contextNow = func() time.Time { return time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { contextNow = oldNow })

	var paths []string
	var workflowBody string
	var auth *testingAuth
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/notebook/write":
			_ = json.NewEncoder(w).Encode(map[string]any{"path": "agents/pm/notebook/2026-04-30-passport-notes.md", "commit_sha": "abc123", "bytes_written": 44})
		case "/tasks/memory-workflow":
			workflowBody = auth.lastBody
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	_, data, err := handleContextCapture(context.Background(), nil, ContextCaptureArgs{TaskID: "task-1", Title: "Passport notes", Content: "Stable process details."})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result := data.(ContextCaptureResult)
	if !result.Status.OK || result.Notebook == nil || result.Notebook.Path == "" {
		t.Fatalf("expected notebook capture, got %+v", result)
	}
	if result.Workflow == nil || !result.Workflow.Updated {
		t.Fatalf("expected workflow update, got %+v", result.Workflow)
	}
	if !slices.Contains(paths, "/notebook/write") || !slices.Contains(paths, "/tasks/memory-workflow") {
		t.Fatalf("expected notebook and workflow broker paths, got %v", paths)
	}
	if !strings.Contains(workflowBody, `"event":"capture"`) || !strings.Contains(workflowBody, `"task_id":"task-1"`) {
		t.Fatalf("unexpected workflow body: %s", workflowBody)
	}
}

func TestContextPromoteUsesNotebookPromotionAndUpdatesWorkflow(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendMarkdown)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	var paths []string
	var workflowBody string
	var auth *testingAuth
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/notebook/promote":
			_ = json.NewEncoder(w).Encode(map[string]any{"promotion_id": "rvw-1", "reviewer_slug": "ceo", "state": "pending", "human_only": false})
		case "/tasks/memory-workflow":
			workflowBody = auth.lastBody
			_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	_, data, err := handleContextPromote(context.Background(), nil, ContextPromoteArgs{
		TaskID: "task-1", SourcePath: "agents/pm/notebook/passport.md", TargetWikiPath: "team/processes/passport.md", Rationale: "ready",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result := data.(ContextPromoteResult)
	if !result.Status.OK || result.Promotion == nil || result.Promotion.PromotionID != "rvw-1" {
		t.Fatalf("expected promotion result, got %+v", result)
	}
	if result.Workflow == nil || !result.Workflow.Updated {
		t.Fatalf("expected workflow update, got %+v", result.Workflow)
	}
	if !slices.Contains(paths, "/notebook/promote") || !slices.Contains(paths, "/tasks/memory-workflow") {
		t.Fatalf("expected promotion and workflow broker paths, got %v", paths)
	}
	if !strings.Contains(workflowBody, `"event":"promote"`) || !strings.Contains(workflowBody, `"promotion_id":"rvw-1"`) {
		t.Fatalf("unexpected workflow body: %s", workflowBody)
	}
}

func TestContextCaptureAndPromoteUnsupportedOnExternalBackend(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	oldStatus := contextResolveMemoryBackendStatus
	t.Cleanup(func() { contextResolveMemoryBackendStatus = oldStatus })
	contextResolveMemoryBackendStatus = func() team.MemoryBackendStatus {
		return team.MemoryBackendStatus{SelectedKind: config.MemoryBackendGBrain, ActiveKind: config.MemoryBackendGBrain}
	}

	_, captureData, err := handleContextCapture(context.Background(), nil, ContextCaptureArgs{Content: "do not pretend this wrote a notebook"})
	if err != nil {
		t.Fatalf("capture handler: %v", err)
	}
	capture := captureData.(ContextCaptureResult)
	if capture.Status.Code != "unsupported_backend" || len(capture.PartialErrors) == 0 {
		t.Fatalf("expected typed unsupported capture status, got %+v", capture)
	}

	_, promoteData, err := handleContextPromote(context.Background(), nil, ContextPromoteArgs{
		SourcePath: "agents/pm/notebook/x.md", TargetWikiPath: "team/x.md", Rationale: "ready",
	})
	if err != nil {
		t.Fatalf("promote handler: %v", err)
	}
	promote := promoteData.(ContextPromoteResult)
	if promote.Status.Code != "unsupported_backend" || len(promote.PartialErrors) == 0 {
		t.Fatalf("expected typed unsupported promote status, got %+v", promote)
	}
}

func TestContextCaptureHandlesNotebookBackendError(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendMarkdown)
	t.Setenv("WUPHF_AGENT_SLUG", "pm")
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"notebook backend is not active"}`, http.StatusServiceUnavailable)
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	_, data, err := handleContextCapture(context.Background(), nil, ContextCaptureArgs{Title: "x", Content: "body"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result := data.(ContextCaptureResult)
	if result.Status.Code != "backend_error" || len(result.PartialErrors) == 0 {
		t.Fatalf("expected typed backend error, got %+v", result)
	}
}

func TestContextHealthIncludesTaskMemoryWorkflow(t *testing.T) {
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendMarkdown)
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{{
			"id": "task-1", "title": "Research", "memory_workflow": map[string]any{"required": true, "lookup": "done"},
		}}})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	_, data, err := handleContextHealth(context.Background(), nil, ContextHealthArgs{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	workflow, ok := data.(ContextHealthResult).MemoryWorkflow.(map[string]any)
	if !ok || workflow["lookup"] != "done" {
		t.Fatalf("expected task memory workflow, got %+v", data.(ContextHealthResult).MemoryWorkflow)
	}
}
