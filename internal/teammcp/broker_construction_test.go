package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/team"
)

// These tests pin the broker-construction invariants that every other
// teammcp test implicitly depends on:
//
//  1. A freshly-constructed broker starts with no skills, no actions,
//     and no skill-invocation messages — nothing leaks in from any
//     prior test in the package binary.
//
//  2. Two brokers constructed in the same test process do not see each
//     other's mutations. State is per-broker, not shared across
//     constructions.
//
// Each test runs under both construction strategies:
//   - "legacy": `t.Setenv("HOME", t.TempDir())` + `team.NewBroker()` —
//     the pre-#316 pattern; behavioral isolation via env shim.
//   - "helper": `newTestBroker(t)` — calls `team.NewBrokerAt(...)` with a
//     per-t tempdir; structural isolation via bound `b.statePath`.
//
// Both must pass. The legacy variant is the regression detector if a
// future change to defaultBrokerStatePath() / RuntimeHomeDir() breaks
// HOME-scoped isolation. The helper variant directly asserts that
// newTestBroker(t) preserves the same invariants.

type brokerCtor struct {
	name string
	new  func(*testing.T) *team.Broker
}

func brokerCtors() []brokerCtor {
	return []brokerCtor{
		{
			name: "legacy",
			new: func(t *testing.T) *team.Broker {
				t.Helper()
				t.Setenv("HOME", t.TempDir())
				return team.NewBroker()
			},
		},
		{
			name: "helper",
			new: func(t *testing.T) *team.Broker {
				t.Helper()
				return newTestBroker(t)
			},
		},
	}
}

func TestBrokerConstructionFreshHasZeroState(t *testing.T) {
	for _, ctor := range brokerCtors() {
		t.Run(ctor.name, func(t *testing.T) {
			b := ctor.new(t)
			if err := b.StartOnPort(0); err != nil {
				t.Fatalf("start broker: %v", err)
			}
			defer b.Stop()

			if got := len(b.Actions()); got != 0 {
				t.Fatalf("fresh broker should have zero actions, got %d (%+v)", got, b.Actions())
			}
			for _, m := range b.AllMessages() {
				switch m.Kind {
				case "skill_invocation", "external_action_planned":
					t.Fatalf("fresh broker should have no skill/action messages, found %+v", m)
				}
			}
			if got := len(b.Requests("general", true)); got != 0 {
				t.Fatalf("fresh broker should have zero requests in general, got %d", got)
			}
			if skills := fetchSkillNames(t, b, "general"); len(skills) != 0 {
				t.Fatalf("fresh broker should expose zero skills in general, got %v", skills)
			}
		})
	}
}

func TestBrokerConstructionTwoBrokersDoNotShareState(t *testing.T) {
	for _, ctor := range brokerCtors() {
		t.Run(ctor.name, func(t *testing.T) {
			b1 := ctor.new(t)
			if err := b1.StartOnPort(0); err != nil {
				t.Fatalf("start b1: %v", err)
			}
			defer b1.Stop()
			b1.SeedDefaultSkills([]agent.PackSkillSpec{{
				Name:        "iso-marker-skill",
				Title:       "Isolation Marker",
				Description: "If b2 sees this name, the construction-isolation invariant has regressed.",
				Trigger:     "n/a",
				Tags:        []string{"characterization"},
				Content:     "n/a",
			}})

			// Sanity: b1 itself sees its own seeded skill.
			if got := fetchSkillNames(t, b1, "general"); !sliceContains(got, "iso-marker-skill") {
				t.Fatalf("b1 should see its own seeded skill, got %v", got)
			}

			b2 := ctor.new(t)
			if err := b2.StartOnPort(0); err != nil {
				t.Fatalf("start b2: %v", err)
			}
			defer b2.Stop()

			// b2 must NOT see b1's marker — that would mean state leaked
			// across constructions through a shared default state path.
			if got := fetchSkillNames(t, b2, "general"); sliceContains(got, "iso-marker-skill") {
				t.Fatalf("b2 leaked b1's marker skill (state-isolation regression); b2 skills=%v", got)
			}
			for _, m := range b2.AllMessages() {
				if m.Kind == "skill_invocation" || m.Kind == "external_action_planned" {
					t.Fatalf("b2 should be empty but observed leaked message %+v", m)
				}
			}
		})
	}
}

func fetchSkillNames(t *testing.T, b *team.Broker, channel string) []string {
	t.Helper()
	skillsURL := "http://" + b.Addr() + "/skills?channel=" + url.QueryEscape(channel)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, skillsURL, nil)
	if err != nil {
		t.Fatalf("build skills request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	names := make([]string, 0, len(body.Skills))
	for _, s := range body.Skills {
		names = append(names, s.Name)
	}
	return names
}

func sliceContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
