package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func TestIsAutoOwner(t *testing.T) {
	for _, s := range []string{"auto", "Auto", " AUTO ", "AuTo"} {
		if !isAutoOwner(s) {
			t.Errorf("isAutoOwner(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "ceo", "builder", "automation"} {
		if isAutoOwner(s) {
			t.Errorf("isAutoOwner(%q) = true, want false", s)
		}
	}
}

// TestBrokerTaskPlanParkAndAuto covers the Backlog/Auto create semantics:
//   - Backlog (park) + specialist → parked in the backlog stage, not
//     dispatched, with per-task provider/model/effort persisted.
//   - Backlog (park) + Auto → owner "auto", parked, not dispatched.
//   - Start now + Auto → owner "auto", not dispatched, and a @ceo triage
//     message posted in the task's channel.
func TestBrokerTaskPlanParkAndAuto(t *testing.T) {
	withWuphfHomeDir(t)
	if err := config.Save(config.Config{LLMProvider: "codex"}); err != nil {
		t.Fatalf("seed provider config: %v", err)
	}
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	plan := func(t *testing.T, task map[string]any) teamTask {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"channel":    "general",
			"created_by": "human",
			"tasks":      []map[string]any{task},
		})
		req, _ := http.NewRequest(http.MethodPost, base+"/task-plan", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task plan request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Tasks []teamTask `json:"tasks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task plan response: %v", err)
		}
		if len(result.Tasks) != 1 {
			t.Fatalf("expected one task, got %+v", result.Tasks)
		}
		return result.Tasks[0]
	}

	// Backlog + specialist + per-task runtime.
	bt := plan(t, map[string]any{
		"title": "Backlog specialist task", "assignee": "builder", "park": true,
		"provider": "codex", "model": "gpt-5.5", "effort": "medium",
	})
	if bt.Owner != "builder" {
		t.Fatalf("owner = %q, want builder", bt.Owner)
	}
	if bt.Status() == "in_progress" {
		t.Fatalf("parked task must not be in_progress, got %q", bt.Status())
	}
	if got := lifecycleStageFor(bt.LifecycleState); got != StageBacklog {
		t.Fatalf("stage = %q (lifecycle %q), want backlog", got, bt.LifecycleState)
	}
	if bt.Provider != "codex" || bt.Model != "gpt-5.5" || bt.Effort != "medium" {
		t.Fatalf("per-task runtime not persisted: provider=%q model=%q effort=%q", bt.Provider, bt.Model, bt.Effort)
	}

	// Backlog + Auto.
	at := plan(t, map[string]any{"title": "Backlog auto task", "assignee": "auto", "park": true})
	if !isAutoOwner(at.Owner) {
		t.Fatalf("owner = %q, want auto", at.Owner)
	}
	if at.Status() == "in_progress" {
		t.Fatalf("auto-parked task must not be in_progress, got %q", at.Status())
	}

	// Start now + Auto: not dispatched, and a @ceo triage message lands in the
	// task's channel.
	st := plan(t, map[string]any{"title": "Start auto task", "assignee": "auto"})
	if !isAutoOwner(st.Owner) {
		t.Fatalf("owner = %q, want auto", st.Owner)
	}
	if st.Status() == "in_progress" {
		t.Fatalf("auto task must not dispatch directly, got %q", st.Status())
	}
	b.mu.Lock()
	found := false
	for _, m := range b.messages {
		if normalizeChannelSlug(m.Channel) == normalizeChannelSlug(st.Channel) && containsSlug(m.Tagged, "ceo") {
			found = true
			break
		}
	}
	b.mu.Unlock()
	if !found {
		t.Fatalf("expected a @ceo triage message in channel %q for auto task %s", st.Channel, st.ID)
	}
}
