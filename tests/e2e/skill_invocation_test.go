package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/team"
)

// TestSkillInvocationE2E exercises the broker's skill lifecycle
// end-to-end across the HTTP surface, the in-memory accessors, and
// disk persistence — the same surface every team_skill_run / MCP
// invocation flows through.
//
// The flow:
//  1. Construct a broker with a bound state path (post-#316 helper
//     pattern), seed it with a skill via SeedDefaultSkills.
//  2. Invoke the skill twice via POST /skills/{name}/invoke,
//     attributed to two different agent slugs.
//  3. Assert the broker's observable state: usage_count, recorded
//     skill_invocation messages (correct From, Channel, Kind),
//     and the action log entries.
//  4. Stop the broker and reconstruct a fresh one against the same
//     state path — the persisted skill, usage count, messages, and
//     actions must all rehydrate.
//
// This pins the contract every other test in the repo only exercises
// in pieces, and asserts that the structural-isolation work (#289 +
// #316 + this branch) didn't break the persistence path.
func TestSkillInvocationE2E(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")

	b := team.NewBrokerAt(statePath)
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
		body, _ := json.Marshal(map[string]string{
			"invoked_by": invokedBy,
			"channel":    channel,
		})
		req, err := http.NewRequest(http.MethodPost,
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
	// to the seeded skill ID via RelatedID.
	var actionsForSkill int
	for _, a := range b.Actions() {
		if a.Kind == "skill_invocation" && a.RelatedID == skillID {
			actionsForSkill++
		}
	}
	if actionsForSkill != 2 {
		t.Errorf("expected 2 action-log entries for skill %s, got %d (all=%+v)",
			skillID, actionsForSkill, b.Actions())
	}

	// Stop the broker before reconstructing — closes the listener and
	// flushes any in-flight saves. Persistence must survive the
	// restart with the same bound state path.
	b.Stop()

	b2 := team.NewBrokerAt(statePath)
	if err := b2.StartOnPort(0); err != nil {
		t.Fatalf("restart broker: %v", err)
	}
	defer b2.Stop()

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
	req, err := http.NewRequest(http.MethodGet,
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
