package team

// human_entity_facts_test.go covers the human side of the entity context-graph
// + wiki pipeline: an invite-admitted human is enrolled as a people/<slug>
// entity and gets a wiki article through the SAME RegenerateEntityArticle
// generator the agents use; the CEO planning prompt surfaces those humans; and
// the broker can record a human as a task participant.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newHumanEntityFixture wires a broker with the entity wiki backend (fact log,
// entity graph, wiki worker) over a fresh repo, mirroring newEntitySyncFixture
// but without the Slack transport — humans here arrive via share sessions.
func newHumanEntityFixture(t *testing.T) (*Broker, *Repo, func()) {
	t.Helper()
	repo, worker, _, teardown := newLearningFixture(t)
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	b.wikiWorker = worker
	b.factLog = NewFactLog(worker)
	b.entityGraph = NewEntityGraph(worker)
	return b, repo, teardown
}

func readHumanArticle(t *testing.T, repo *Repo, slug string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Root(), "team", "people", slug+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read article %s: %v", slug, err)
	}
	return string(data), true
}

func TestEnrollHumanEntityBuildsWikiArticle(t *testing.T) {
	b, repo, teardown := newHumanEntityFixture(t)
	defer teardown()

	b.enrollHumanEntity(context.Background(), "sarah-chen", "Sarah Chen", "joined the office via a share invite")

	article, ok := readHumanArticle(t, repo, "sarah-chen")
	if !ok {
		t.Fatal("expected a wiki article for the enrolled human")
	}
	for _, want := range []string{
		"Sarah Chen", // title + display-name fact
		"Human teammate in the office",
		"joined the office via a share invite",
		"is a person in the team knowledge graph", // shared generator lead
	} {
		if !strings.Contains(article, want) {
			t.Errorf("human article missing %q:\n%s", want, article)
		}
	}
}

func TestEnrollHumanEntityIsIdempotent(t *testing.T) {
	b, repo, teardown := newHumanEntityFixture(t)
	defer teardown()

	ctx := context.Background()
	b.enrollHumanEntity(ctx, "sarah-chen", "Sarah Chen", "joined the office via a share invite")
	first, ok := readHumanArticle(t, repo, "sarah-chen")
	if !ok {
		t.Fatal("expected an article after first enrollment")
	}

	// A second enrollment with identical inputs must not duplicate facts:
	// content-hash dedup in FactLog.Append makes the pass a no-op, so the
	// article body is byte-stable (frontmatter timestamps aside).
	b.enrollHumanEntity(ctx, "sarah-chen", "Sarah Chen", "joined the office via a share invite")
	second, _ := readHumanArticle(t, repo, "sarah-chen")

	firstBody := stripFrontmatter(first)
	secondBody := stripFrontmatter(second)
	if firstBody != secondBody {
		t.Errorf("re-enrollment churned the article body:\nfirst:\n%s\nsecond:\n%s", firstBody, secondBody)
	}
	if c := strings.Count(secondBody, "Human teammate in the office"); c != 1 {
		t.Errorf("expected exactly one identity fact after dedup, got %d:\n%s", c, secondBody)
	}
}

func TestEnrollHumanEntityNoWikiBackendIsSafe(t *testing.T) {
	// No wiki backend wired: enrollment must be a clean no-op, never a panic.
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	b.enrollHumanEntity(context.Background(), "sarah-chen", "Sarah Chen", "joined")
}

func TestFireHumanAdmitHookEnrollsHuman(t *testing.T) {
	b, repo, teardown := newHumanEntityFixture(t)
	defer teardown()

	// fireHumanAdmitHook is the integration point the HTTP accept handler
	// calls after a session is persisted; it must enroll the human into the
	// entity wiki even when no share-transport hook is installed.
	b.fireHumanAdmitHook(context.Background(), humanSession{
		ID:          "session-1",
		HumanSlug:   "sarah-chen",
		DisplayName: "Sarah Chen",
	})

	article, ok := readHumanArticle(t, repo, "sarah-chen")
	if !ok {
		t.Fatal("admit hook did not enroll the human into the entity wiki")
	}
	if !strings.Contains(article, "joined the office via a share invite") {
		t.Errorf("article missing the admit-source fact:\n%s", article)
	}
}

func TestHumansForPromptListsActiveSessionsWithContext(t *testing.T) {
	b, _, teardown := newHumanEntityFixture(t)
	defer teardown()

	// Enroll so the human has an article whose lead line becomes the context.
	b.enrollHumanEntity(context.Background(), "sarah-chen", "Sarah Chen", "joined the office via a share invite")

	b.mu.Lock()
	b.humanSessions = []humanSession{
		{ID: "session-1", HumanSlug: "sarah-chen", DisplayName: "Sarah Chen"},
		// A revoked session for the same human must not appear twice nor at all
		// if it were the only one.
		{ID: "session-2", HumanSlug: "marco-li", DisplayName: "Marco Li", RevokedAt: "2026-01-01T00:00:00Z"},
		{ID: "session-3", HumanSlug: "amy-park", DisplayName: "Amy Park"},
	}
	b.mu.Unlock()

	humans := b.HumansForPrompt()
	if len(humans) != 2 {
		t.Fatalf("expected 2 active humans (revoked dropped), got %d: %+v", len(humans), humans)
	}
	// Sorted by slug: amy-park before sarah-chen.
	if humans[0].Slug != "amy-park" || humans[1].Slug != "sarah-chen" {
		t.Fatalf("humans not sorted by slug: %+v", humans)
	}
	if humans[1].Name != "Sarah Chen" {
		t.Errorf("display name lost: %+v", humans[1])
	}
	if !strings.Contains(humans[1].Context, "Sarah Chen") {
		t.Errorf("expected context line drawn from the wiki article, got %q", humans[1].Context)
	}
	// amy-park has no article, so its context line is empty (bare-name fallback).
	if humans[0].Context != "" {
		t.Errorf("expected empty context for un-enrolled human, got %q", humans[0].Context)
	}
}

func TestRenderHumansInOfficeBlock(t *testing.T) {
	// Empty input omits the block entirely.
	if got := renderHumansInOfficeBlock(nil); got != "" {
		t.Errorf("expected empty block for no humans, got %q", got)
	}

	block := renderHumansInOfficeBlock([]humanPromptEntry{
		{Slug: "sarah-chen", Name: "Sarah Chen", Context: "Sarah Chen is a person in the team knowledge graph."},
		{Slug: "amy-park", Name: "Amy Park"},
	})
	for _, want := range []string{
		"== HUMANS IN THE OFFICE ==",
		"never set a human as a task `owner`",
		"participants",
		"@sarah-chen — Sarah Chen",
		"@amy-park — Amy Park",
		"approval or sign-off",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("humans block missing %q:\n%s", want, block)
		}
	}
}

func TestNormalizeTaskParticipants(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, nil},
		{"empties dropped", []string{"  ", ""}, nil},
		{"strip at and human prefix", []string{"@sarah-chen", "human:amy-park"}, []string{"sarah-chen", "amy-park"}},
		{"dedupe preserves order", []string{"sarah-chen", "@sarah-chen", "amy-park"}, []string{"sarah-chen", "amy-park"}},
		{"trims whitespace", []string{"  marco-li  "}, []string{"marco-li"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeTaskParticipants(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestTeamTaskParticipantsWireRoundTrip(t *testing.T) {
	task := teamTask{
		ID:           "task-1",
		Title:        "Get founder sign-off on the pricing change",
		Owner:        "ceo",
		Participants: []string{"sarah-chen", "amy-park"},
		status:       "open",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
		CreatedBy:    "ceo",
	}
	data, err := task.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"participants":["sarah-chen","amy-park"]`) {
		t.Errorf("participants not on the wire: %s", data)
	}
	var back teamTask
	if err := back.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Participants) != 2 || back.Participants[0] != "sarah-chen" || back.Participants[1] != "amy-park" {
		t.Errorf("participants lost on round-trip: %+v", back.Participants)
	}
}
