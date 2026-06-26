package team

// slack_entity_facts_test.go covers the entity-wiki pass: humans and bots in
// bridged channels plus office/foreign agents land as people facts and
// regenerate team/people/<slug>.md articles; the pass is idempotent (dedup
// makes the second run silent — no new facts, no article churn).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/provider"
)

func newEntitySyncFixture(t *testing.T) (*SlackTransport, *Broker, *Repo, *fakeSlackAPI, func()) {
	t.Helper()
	repo, worker, _, teardown := newLearningFixture(t)

	api := newFakeSlackAPI()
	api.users["U1HUMAN"] = &slack.User{
		ID:      "U1HUMAN",
		Name:    "naj",
		TZ:      "America/Los_Angeles",
		Profile: slack.UserProfile{DisplayName: "Naj Mohammad", RealName: "Najmuzzaman Mohammad", Title: "Founder"},
	}
	api.users["U2STRAYBOT"] = &slack.User{
		ID: "U2STRAYBOT", IsBot: true,
		Profile: slack.UserProfile{DisplayName: "Some Vendor Bot"},
	}
	api.users["U3HERMES"] = &slack.User{
		ID: "U3HERMES", IsBot: true,
		Profile: slack.UserProfile{DisplayName: "hermes"},
	}
	api.members["C0123"] = []string{"U1HUMAN", "U2STRAYBOT", "U3HERMES", "UBOT"}

	tr, b := newTestSlackTransport(t, "C0123", api)
	tr.botUserID = "UBOT"
	b.wikiWorker = worker
	b.factLog = NewFactLog(worker)
	b.entityGraph = NewEntityGraph(worker)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "ceo", Name: "CEO", Role: "Coordinator", Provider: provider.ProviderBinding{Kind: "codex"}},
		officeMember{Slug: "hermes", Name: "Hermes", Provider: provider.ProviderBinding{
			Kind:  "slack",
			Slack: &provider.SlackProviderBinding{UserID: "U3HERMES"},
		}},
	)
	b.mu.Unlock()
	return tr, b, repo, api, teardown
}

func readEntityArticle(t *testing.T, repo *Repo, slug string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Root(), "team", "people", slug+".md"))
	if err != nil {
		t.Fatalf("read article %s: %v", slug, err)
	}
	return string(data)
}

func TestSlackEntityFactSyncBuildsPeopleArticles(t *testing.T) {
	tr, _, repo, _, teardown := newEntitySyncFixture(t)
	defer teardown()

	tr.syncEntityFactsOnce(context.Background())

	// Human: profile facts + presence.
	human := readEntityArticle(t, repo, "naj-mohammad")
	for _, want := range []string{
		"Human teammate on Slack",
		"U1HUMAN",
		"Najmuzzaman Mohammad",
		"Founder",
		"America/Los_Angeles",
		`office channel "slack-general"`,
	} {
		if !strings.Contains(human, want) {
			t.Errorf("human article missing %q:\n%s", want, human)
		}
	}

	// Foreign agent: roster identity (under its OFFICE slug) + presence.
	hermes := readEntityArticle(t, repo, "hermes")
	for _, want := range []string{"foreign Slack agent", "U3HERMES", "office channel"} {
		if !strings.Contains(hermes, want) {
			t.Errorf("hermes article missing %q:\n%s", want, hermes)
		}
	}

	// Office agent: role + runtime.
	ceo := readEntityArticle(t, repo, "ceo")
	for _, want := range []string{"office agent", "Coordinator", "codex runtime"} {
		if !strings.Contains(ceo, want) {
			t.Errorf("ceo article missing %q:\n%s", want, ceo)
		}
	}

	// Unregistered stray bot is still observed.
	stray := readEntityArticle(t, repo, "some-vendor-bot")
	if !strings.Contains(stray, "not registered as an office agent") {
		t.Errorf("stray bot article missing unregistered note:\n%s", stray)
	}

	// The office's own bot user is the observer, not an entity.
	if _, err := os.Stat(filepath.Join(repo.Root(), "team", "people", "ubot.md")); err == nil {
		t.Error("the office bot must not get an entity article")
	}
}

func TestSlackEntityFactSyncIsIdempotent(t *testing.T) {
	tr, b, repo, _, teardown := newEntitySyncFixture(t)
	defer teardown()

	tr.syncEntityFactsOnce(context.Background())
	factsPath := filepath.Join(repo.Root(), FactLogPath(EntityKindPeople, "naj-mohammad"))
	first, err := os.ReadFile(factsPath)
	if err != nil {
		t.Fatalf("read facts: %v", err)
	}

	tr.syncEntityFactsOnce(context.Background())
	second, err := os.ReadFile(factsPath)
	if err != nil {
		t.Fatalf("re-read facts: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("second pass appended duplicate facts:\nfirst:\n%s\nsecond:\n%s", first, second)
	}

	// Wiki backend off → pass is a clean no-op.
	b.mu.Lock()
	b.factLog = nil
	b.mu.Unlock()
	tr.syncEntityFactsOnce(context.Background())
}
