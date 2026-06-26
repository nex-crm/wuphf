package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
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

// TestOwnerlessTaskCreate_TriageMessageWakesCEO pins the FULL wake trace for
// an ownerless ("auto") start-now task, at the live layer:
//
//	POST /task-plan (assignee=auto)
//	  → requestAutoAssignmentLocked posts a human-authored @ceo message
//	  → the broker publishes it on the message stream notifyAgentsLoop reads
//	  → deliverMessageNotification dispatches a headless turn to the CEO.
//
// Without the dispatch the staffing copy on the task page ("CEO is picking
// the owner") would be a lie — the task would sit parked forever, which is
// exactly the approval-wall regression the composer fix removes.
func TestOwnerlessTaskCreate_TriageMessageWakesCEO(t *testing.T) {
	withWuphfHomeDir(t)
	if err := config.Save(config.Config{LLMProvider: "codex"}); err != nil {
		t.Fatalf("seed provider config: %v", err)
	}
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "general", "ceo", "CEO")
	ensureTestMemberAccess(b, "general", "builder", "Builder")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	l := newHeadlessLauncherForTest(t)
	l.broker = b
	l.provider = "codex" // headless dispatch for every agent
	l.notifyLastDelivered = make(map[notifyDedupKey]time.Time)
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "builder", Name: "Builder"},
		},
	}

	processed := make(chan string, 8)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, _ context.Context, slug, _ string, _ ...string) error {
		processed <- slug
		return nil
	})

	// Subscribe BEFORE the create so the published triage message is observed
	// exactly the way notifyAgentsLoop observes it.
	msgs, unsubscribe := b.SubscribeMessages(32)
	defer unsubscribe()

	body, _ := json.Marshal(map[string]any{
		"channel":    "general",
		"created_by": "human",
		"tasks": []map[string]any{
			{"title": "Ownerless start-now task", "assignee": "auto"},
		},
	})
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/task-plan", b.Addr()), bytes.NewReader(body))
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

	// The triage message must arrive on the published stream and must NOT be
	// authored by "system" — notifyAgentsLoop drops From=system posts, so a
	// system author would silently never wake the CEO.
	var triage channelMessage
	deadline := time.After(2 * time.Second)
waitTriage:
	for {
		select {
		case m := <-msgs:
			if containsSlug(m.Tagged, "ceo") {
				triage = m
				break waitTriage
			}
		case <-deadline:
			t.Fatalf("no @ceo triage message published for the ownerless task")
		}
	}
	if triage.From == "system" {
		t.Fatalf("triage message authored by %q — notifyAgentsLoop drops system posts, the CEO would never wake", triage.From)
	}

	// Run the exact delivery step notifyAgentsLoop performs for this message.
	l.deliverMessageNotification(triage)

	dispatchDeadline := time.After(2 * time.Second)
	for {
		select {
		case slug := <-processed:
			if slug == "ceo" {
				return // CEO woke — full trace verified
			}
		case <-dispatchDeadline:
			t.Fatalf("CEO never received a headless turn for the ownerless-task triage message")
		}
	}
}
