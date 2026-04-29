package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestHandlePostSkill_RejectsDuplicateName pins the 409 path. Two skill
// records sharing a name break findSkillByNameLocked's "first non-archived
// match wins" semantics — every callsite would observe a stale reference.
// Keep this guard in place.
func TestHandlePostSkill_RejectsDuplicateName(t *testing.T) {
	b := newTestBroker(t)

	mk := func(name string) *httptest.ResponseRecorder {
		body := bytes.NewBufferString(fmt.Sprintf(`{
			"name":%q,
			"title":"Dup",
			"content":"do the thing",
			"created_by":"ceo",
			"channel":"general"
		}`, name))
		req := httptest.NewRequest(http.MethodPost, "/skills", body)
		rec := httptest.NewRecorder()
		b.handlePostSkill(rec, req)
		return rec
	}

	if rec := mk("dup-skill"); rec.Code != http.StatusOK {
		t.Fatalf("first create: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec := mk("dup-skill")
	if rec.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSeedDefaultSkills_IsIdempotent locks the contract documented on
// SeedDefaultSkills: a second call with the same specs MUST NOT create
// duplicate skill entries. The Launcher invokes this every boot, so a
// regression here would multiply seeded skills across restarts.
func TestSeedDefaultSkills_IsIdempotent(t *testing.T) {
	b := newTestBroker(t)
	specs := []agent.PackSkillSpec{
		{Name: "deploy", Title: "Deploy", Description: "Deploy app", Content: "1. push tag"},
		{Name: "rollback", Title: "Rollback", Description: "Roll back app", Content: "1. revert"},
	}

	b.SeedDefaultSkills(specs)
	b.SeedDefaultSkills(specs)

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.skills) != 2 {
		t.Fatalf("expected 2 seeded skills after idempotent reseed, got %d: %+v", len(b.skills), b.skills)
	}
	names := map[string]int{}
	for _, sk := range b.skills {
		names[sk.Name]++
	}
	if names["deploy"] != 1 || names["rollback"] != 1 {
		t.Errorf("expected one of each, got %+v", names)
	}
}

// TestFindSkillByWorkflowKey_PrefersNonArchived guards a subtle precedence
// rule: archived skills are invisible to lookup. A new active skill that
// reuses an archived skill's workflow_key should be returned by
// findSkillByWorkflowKeyLocked rather than the archived original.
func TestFindSkillByWorkflowKey_PrefersNonArchived(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.skills = append(b.skills,
		teamSkill{ID: "old", Name: "old-deploy", WorkflowKey: "deploy", Status: "archived"},
		teamSkill{ID: "new", Name: "new-deploy", WorkflowKey: "deploy", Status: "active"},
	)
	got := b.findSkillByWorkflowKeyLocked("deploy")
	b.mu.Unlock()

	if got == nil {
		t.Fatal("expected to find non-archived skill")
	}
	if got.ID != "new" {
		t.Errorf("expected non-archived skill, got %+v", got)
	}
}

func TestPostSkillProposeCreatesApprovalRequest(t *testing.T) {
	tmpDir := t.TempDir()
	b := NewBrokerAt(filepath.Join(tmpDir, "broker-state.json"))
	body := bytes.NewBufferString(`{
		"action":"propose",
		"name":"deterministic-skill",
		"title":"Deterministic Skill",
		"description":"Created through the skill API.",
		"content":"1. Do the deterministic thing",
		"created_by":"ceo",
		"channel":"general"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/skills", body)
	rec := httptest.NewRecorder()

	b.handlePostSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.skills) != 1 {
		t.Fatalf("expected proposed skill, got %+v", b.skills)
	}
	if b.skills[0].Status != "proposed" {
		t.Fatalf("expected proposed status, got %q", b.skills[0].Status)
	}
	if len(b.requests) != 1 {
		t.Fatalf("expected approval request, got %+v", b.requests)
	}
	if b.requests[0].Kind != "skill_proposal" || b.requests[0].ReplyTo != "deterministic-skill" {
		t.Fatalf("unexpected skill proposal request: %+v", b.requests[0])
	}
}

// Test 7: Answering "accept" via HTTP activates the skill.
func TestSkillProposalAcceptCallbackActivatesSkill(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	// Seed a proposed skill and matching interview request.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID: "deploy-check", Name: "deploy-check", Title: "Deploy Check",
		Status: "proposed", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	b.counter++
	reqID := fmt.Sprintf("request-%d", b.counter)
	b.requests = append(b.requests, humanInterview{
		ID:        reqID,
		Kind:      "skill_proposal",
		Status:    "pending",
		From:      "ceo",
		Channel:   "general",
		Title:     "Approve skill: Deploy Check",
		Question:  "Activate?",
		ReplyTo:   "deploy-check",
		Blocking:  false,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Options:   []interviewOption{{ID: "accept", Label: "Accept"}, {ID: "reject", Label: "Reject"}},
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          reqID,
		"choice_id":   "accept",
		"choice_text": "Accept",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	status := b.skills[0].Status
	b.mu.Unlock()
	if status != "active" {
		t.Fatalf("expected skill status 'active' after accept, got %q", status)
	}
}

// Test 8: Answering "reject" via HTTP archives the skill.
func TestSkillProposalRejectCallbackArchivesSkill(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID: "risky-skill", Name: "risky-skill", Title: "Risky Skill",
		Status: "proposed", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	b.counter++
	reqID := fmt.Sprintf("request-%d", b.counter)
	b.requests = append(b.requests, humanInterview{
		ID:        reqID,
		Kind:      "skill_proposal",
		Status:    "pending",
		From:      "ceo",
		Channel:   "general",
		Title:     "Approve skill: Risky Skill",
		Question:  "Activate?",
		ReplyTo:   "risky-skill",
		Blocking:  false,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
		Options:   []interviewOption{{ID: "accept", Label: "Accept"}, {ID: "reject", Label: "Reject"}},
	})
	b.mu.Unlock()

	base := fmt.Sprintf("http://%s", b.Addr())
	answerBody, _ := json.Marshal(map[string]any{
		"id":          reqID,
		"choice_id":   "reject",
		"choice_text": "Reject",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	b.mu.Lock()
	status := b.skills[0].Status
	b.mu.Unlock()
	if status != "archived" {
		t.Fatalf("expected skill status 'archived' after reject, got %q", status)
	}
}

func TestInvokeSkillTracksInvokerChannelAndExecutionMetadata(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		ID:        "skill-youtube-factory-bootstrap",
		Name:      "youtube-factory-bootstrap",
		Title:     "Bootstrap Automated YouTube Factory",
		Status:    "active",
		Channel:   "general",
		CreatedBy: "ceo",
	})
	b.channels = append(b.channels, teamChannel{
		Slug:    "youtube-factory",
		Name:    "YouTube Factory",
		Members: []string{"ceo", "ops"},
	})
	b.mu.Unlock()

	body := bytes.NewBufferString(`{"name":"youtube-factory-bootstrap","invoked_by":"you","channel":"youtube-factory"}`)
	req := httptest.NewRequest(http.MethodPost, "/skills/youtube-factory-bootstrap/invoke", body)
	rec := httptest.NewRecorder()

	b.handleInvokeSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.skills[0].UsageCount != 1 {
		t.Fatalf("expected usage count 1, got %d", b.skills[0].UsageCount)
	}
	if b.skills[0].LastExecutionStatus != "invoked" {
		t.Fatalf("expected last execution status invoked, got %q", b.skills[0].LastExecutionStatus)
	}
	if b.skills[0].LastExecutionAt == "" {
		t.Fatal("expected last execution timestamp to be set")
	}
	last := b.messages[len(b.messages)-1]
	if last.Channel != "youtube-factory" {
		t.Fatalf("expected invocation message in youtube-factory, got %q", last.Channel)
	}
	if last.From != "you" {
		t.Fatalf("expected invocation from you, got %q", last.From)
	}
	if !strings.Contains(last.Content, "@you") {
		t.Fatalf("expected invocation content to reference @you, got %q", last.Content)
	}
}

func TestInvokeSkillCreatesSkillRunTask(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	b.skills = append(b.skills, teamSkill{
		ID:      "skill-deploy",
		Name:    "deploy",
		Title:   "Deploy to Production",
		Status:  "active",
		Channel: "general",
		Content: "Step 1: Run tests. Step 2: Push tag.",
	})
	b.mu.Unlock()

	body := bytes.NewBufferString(`{"invoked_by":"eng","channel":"general"}`)
	req := httptest.NewRequest(http.MethodPost, "/skills/deploy/invoke", body)
	rec := httptest.NewRecorder()

	b.handleInvokeSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Response must include task_id.
	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	taskID, ok := out["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected task_id in response, got %v", out["task_id"])
	}

	// A task with TaskType=skill_run must exist in b.tasks.
	b.mu.Lock()
	defer b.mu.Unlock()
	var found *teamTask
	for i := range b.tasks {
		if b.tasks[i].ID == taskID {
			found = &b.tasks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("task %q not found in b.tasks", taskID)
	}
	if found.TaskType != "skill_run" {
		t.Errorf("expected TaskType=skill_run, got %q", found.TaskType)
	}
	if found.PipelineID != "skill_invocation" {
		t.Errorf("expected PipelineID=skill_invocation, got %q", found.PipelineID)
	}
	if found.Owner != "ceo" {
		t.Errorf("expected owner=ceo (office lead), got %q", found.Owner)
	}
	if !strings.Contains(found.Title, "Deploy to Production") {
		t.Errorf("expected task title to contain skill title, got %q", found.Title)
	}
	if !strings.Contains(found.Details, "Invoked by @eng") {
		t.Errorf("expected details to include invoker header, got %q", found.Details)
	}
	if !strings.Contains(found.Details, "Step 1: Run tests") {
		t.Errorf("expected details to include skill content, got %q", found.Details)
	}
}

// Test 10: buildPrompt for the lead includes SKILL & AGENT AWARENESS section.
func TestBuildPromptLeadIncludesSkillAwareness(t *testing.T) {
	l := &Launcher{
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
			},
		},
	}
	prompt := l.buildPrompt("ceo")
	if !strings.Contains(prompt, "SKILL & AGENT AWARENESS") {
		t.Fatalf("expected SKILL & AGENT AWARENESS block in lead prompt")
	}
	if !strings.Contains(prompt, "team_skill_create(action=create)") {
		t.Fatalf("expected team_skill_create guidance in lead prompt")
	}
}

// Test 10: Skill proposal and interview persist and reload correctly.
func TestSkillProposalPersistenceRoundTrip(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = []officeMember{{Slug: "ceo", Name: "CEO", Role: "lead"}}
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			b.channels[i].Members = append(b.channels[i].Members, "ceo")
		}
	}
	b.mu.Unlock()
	body := bytes.NewBufferString(`{
		"action":"propose",
		"name":"persist-skill",
		"title":"Persist Skill",
		"description":"Persisted proposed skill",
		"content":"1. Do the thing",
		"created_by":"ceo",
		"channel":"general"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/skills", body)
	rec := httptest.NewRecorder()
	b.handlePostSkill(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handlePostSkill: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	reloaded := reloadedBroker(t, b)
	reloaded.mu.Lock()
	skills := append([]teamSkill(nil), reloaded.skills...)
	requests := append([]humanInterview(nil), reloaded.requests...)
	reloaded.mu.Unlock()

	if len(skills) != 1 || skills[0].Name != "persist-skill" {
		t.Fatalf("expected persisted skill 'persist-skill', got %d skills", len(skills))
	}
	if len(requests) != 1 || requests[0].Kind != "skill_proposal" {
		t.Fatalf("expected persisted skill_proposal request, got %d requests", len(requests))
	}
}
