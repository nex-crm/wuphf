package team

// Regression tests for the request-changes → follow-up-task composition
// (founder directive: notebook review in place). A HUMAN request-changes on
// a promotion must (a) record the state change with the rationale AND
// (b) create a task owned by the notebook's author agent carrying the
// comment verbatim, the notebook path, and the resubmit instruction.
// Driven through the live HTTP handler, not internal calls, because the
// bug class this guards against lives on the wire path.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// seedGeneralChannel gives the bare review-test broker the #general channel
// the follow-up task composition creates into.
func seedGeneralChannel(b *Broker) {
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug: "general", Name: "general",
		Members: []string{"human", "ceo", "pm"},
	})
	b.mu.Unlock()
}

func requestChangesViaHTTP(t *testing.T, srv *httptest.Server, token, id, actor, rationale string) *http.Response {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"actor_slug": actor,
		"rationale":  rationale,
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/review/"+id+"/request-changes", bytes.NewReader(payload), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request-changes: %v", err)
	}
	return res
}

func promoteForChangeTaskTest(t *testing.T, srv *httptest.Server, b *Broker) string {
	t.Helper()
	token := b.Token()
	seedNotebookViaHTTP(t, srv, token, "pm", "agents/pm/notebook/retro.md", "# Retro learnings\n\nbody\n")
	res := submitPromotion(t, srv, token, map[string]any{
		"my_slug":          "pm",
		"source_path":      "agents/pm/notebook/retro.md",
		"target_wiki_path": "team/playbooks/retro.md",
		"rationale":        "Ready for review.",
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("promote status=%d body=%s", res.StatusCode, string(raw))
	}
	var parsed struct {
		PromotionID string `json:"promotion_id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil || parsed.PromotionID == "" {
		t.Fatalf("promote decode: id=%q err=%v", parsed.PromotionID, err)
	}
	return parsed.PromotionID
}

func TestReviewRequestChanges_HumanCreatesOwnerTask(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	seedGeneralChannel(b)
	id := promoteForChangeTaskTest(t, srv, b)

	const comment = "Merge with the existing Corti brief — don't create a duplicate."
	res := requestChangesViaHTTP(t, srv, b.Token(), id, "", comment)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("request-changes status=%d body=%s", res.StatusCode, string(raw))
	}

	// (a) review state recorded with the rationale.
	p, err := b.ReviewLog().Get(id)
	if err != nil {
		t.Fatalf("get promotion: %v", err)
	}
	if p.State != PromotionChangesRequested {
		t.Fatalf("state=%s, want changes-requested", p.State)
	}

	// (b) a follow-up task owned by the notebook's author exists.
	var task *teamTask
	for _, candidate := range b.AllTasks() {
		if strings.HasPrefix(candidate.Title, "Update notebook item:") {
			cp := candidate
			task = &cp
			break
		}
	}
	if task == nil {
		t.Fatalf("no follow-up task created; tasks=%d", len(b.AllTasks()))
	}
	if task.Title != "Update notebook item: Retro learnings" {
		t.Errorf("title=%q", task.Title)
	}
	if task.Owner != "pm" {
		t.Errorf("owner=%q, want pm (notebook author)", task.Owner)
	}
	if task.CreatedBy != "human" {
		t.Errorf("created_by=%q, want human", task.CreatedBy)
	}
	if !strings.Contains(task.Details, comment) {
		t.Errorf("details missing the human comment verbatim: %q", task.Details)
	}
	if !strings.Contains(task.Details, "agents/pm/notebook/retro.md") {
		t.Errorf("details missing the notebook path: %q", task.Details)
	}
	if !strings.Contains(task.Details, "resubmit it for review") {
		t.Errorf("details missing the resubmit instruction: %q", task.Details)
	}
	// Creation is the authorization: a create with a real owner lands
	// running (legacy status "in_progress"), no approve gate.
	if strings.TrimSpace(task.status) != "in_progress" {
		t.Errorf("status=%q, want in_progress (running)", task.status)
	}
}

func TestReviewRequestChanges_AgentActorDoesNotCreateTask(t *testing.T) {
	srv, b, teardown := newReviewTestServer(t)
	defer teardown()
	seedGeneralChannel(b)
	id := promoteForChangeTaskTest(t, srv, b)

	// "ceo" is the resolved reviewer in this fixture — an agent actor, not
	// the human. The composition is human-only by contract (created_by =
	// the human actor); agent reviewers keep the existing notify-only path.
	res := requestChangesViaHTTP(t, srv, b.Token(), id, "ceo", "Tighten the summary.")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("request-changes status=%d body=%s", res.StatusCode, string(raw))
	}
	for _, candidate := range b.AllTasks() {
		if strings.HasPrefix(candidate.Title, "Update notebook item:") {
			t.Fatalf("agent request-changes must not create a follow-up task; got %q", candidate.Title)
		}
	}
}
