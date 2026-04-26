package team

// End-to-end broker skill-invocation integration test. Exercises the
// full lifecycle a team_skill_run / MCP skill tool flows through:
// HTTP /skills/{name}/invoke, in-memory accessors (AllMessages,
// Actions), action-log linkage via RelatedID, and disk persistence
// across a broker restart.
//
// Sibling to TestBrokerStatePersistsAcrossReload_ChannelAndMember:
// covers a third saveLocked call site (handleInvokeSkill), with the
// added wrinkle of asserting per-invoker From accounting and the
// usage_count rehydration after restart. Uses reloadedBroker(t, b)
// for the restart leg — same opt-in-to-disk-load pattern the team
// package's other persistence tests rely on.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestBrokerSkillInvocationE2E_PersistsAcrossReload(t *testing.T) {
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}

	const (
		skillName  = "investigate"
		skillTitle = "Investigate a Bug"
		channel    = "general"
	)

	b.SeedDefaultSkills([]agent.PackSkillSpec{{
		Name:        skillName,
		Title:       skillTitle,
		Description: "Systematic debugging with root cause analysis.",
		Trigger:     "When a bug or error is reported",
		Tags:        []string{"engineering", "debugging"},
		Content:     "Step 1: Reproduce. Step 2: Isolate. Step 3: Root cause. Step 4: Fix.",
	}})

	invoke := func(t *testing.T, invokedBy string) (skillID string, usageCount int) {
		t.Helper()
		body, err := json.Marshal(map[string]string{
			"invoked_by": invokedBy,
			"channel":    channel,
		})
		if err != nil {
			t.Fatalf("marshal invoke body: %v", err)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
			"http://"+b.Addr()+"/skills/"+skillName+"/invoke", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("build invoke request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("invoke skill: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("invoke skill: status=%d", resp.StatusCode)
		}
		var out struct {
			Skill struct {
				ID         string `json:"id"`
				UsageCount int    `json:"usage_count"`
			} `json:"skill"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode invoke response: %v", err)
		}
		return out.Skill.ID, out.Skill.UsageCount
	}

	skillID, count := invoke(t, "eng")
	if count != 1 {
		t.Fatalf("first invoke: expected usage_count=1, got %d", count)
	}
	if skillID == "" {
		t.Fatal("first invoke: skill ID missing from response")
	}

	if _, count2 := invoke(t, "qa"); count2 != 2 {
		t.Fatalf("second invoke: expected usage_count=2, got %d", count2)
	}

	// Validate observable state: two skill_invocation messages with
	// distinct From slugs, attributed to the requested channel.
	invocations := map[string]int{}
	for _, m := range b.AllMessages() {
		if m.Kind != "skill_invocation" {
			continue
		}
		if m.Channel != channel {
			t.Errorf("skill_invocation in unexpected channel %q (want %q)", m.Channel, channel)
		}
		if m.Title != skillTitle {
			t.Errorf("skill_invocation title=%q, want %q", m.Title, skillTitle)
		}
		invocations[m.From]++
	}
	if invocations["eng"] != 1 {
		t.Errorf("expected exactly 1 invocation by eng, got %d", invocations["eng"])
	}
	if invocations["qa"] != 1 {
		t.Errorf("expected exactly 1 invocation by qa, got %d", invocations["qa"])
	}

	// Action log should also show two skill_invocation entries linked
	// to the seeded skill ID via RelatedID, sourced from "office".
	var actionsForSkill []officeActionLog
	for _, a := range b.Actions() {
		if a.Kind == "skill_invocation" && a.RelatedID == skillID {
			actionsForSkill = append(actionsForSkill, a)
		}
	}
	if len(actionsForSkill) != 2 {
		t.Errorf("expected 2 action-log entries for skill %s, got %d (%+v)",
			skillID, len(actionsForSkill), actionsForSkill)
	}
	for _, a := range actionsForSkill {
		if a.Source != "office" {
			t.Errorf("skill_invocation action source=%q, want %q", a.Source, "office")
		}
	}

	// Restart the broker against the same state path via the documented
	// helper — opts back in to loadState(), which test-mode NewBrokerAt
	// otherwise skips to prevent cross-test state leakage.
	b2 := reloadedBroker(t, b)

	var rehydrated int
	for _, m := range b2.AllMessages() {
		if m.Kind == "skill_invocation" {
			rehydrated++
		}
	}
	if rehydrated != 2 {
		t.Errorf("expected 2 skill_invocation messages to rehydrate, got %d", rehydrated)
	}

	// Confirm the skill itself survived and the usage count persisted.
	if err := b2.StartOnPort(0); err != nil {
		t.Fatalf("restart broker: %v", err)
	}
	defer b2.Stop()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"http://"+b2.Addr()+"/skills?channel="+channel, nil)
	if err != nil {
		t.Fatalf("build skills request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b2.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var skillsBody struct {
		Skills []struct {
			Name       string `json:"name"`
			UsageCount int    `json:"usage_count"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&skillsBody); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	var found bool
	for _, s := range skillsBody.Skills {
		if s.Name != skillName {
			continue
		}
		found = true
		if s.UsageCount != 2 {
			t.Errorf("rehydrated skill usage_count=%d, want 2", s.UsageCount)
		}
	}
	if !found {
		names := make([]string, 0, len(skillsBody.Skills))
		for _, s := range skillsBody.Skills {
			names = append(names, s.Name)
		}
		t.Errorf("seeded skill %q not found after restart; skills=%s",
			skillName, strings.Join(names, ","))
	}
}
