package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// The App Home view is a native Block Kit overview: surface buttons, the task
// board with one card-like section per task, and the wiki as real links.
func TestSlackHomeTabRendersBoardWikiAndWebSurfaces(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://127.0.0.1:7905")
	seedTask := func(id, title, status string) {
		b.mu.Lock()
		b.tasks = append(b.tasks, teamTask{ID: id, Title: title, Owner: "ceo", Channel: "slack-general", status: status})
		b.mu.Unlock()
	}
	seedTask("OFFICE-1", "Ship the pricing facts", "in_progress")
	seedTask("OFFICE-2", "Review the launch plan", "review")
	seedTask("OFFICE-3", "Old win", "done")
	// Seeded/system plumbing must stay off the board.
	seedTask("task-skill-nudge-9", "Skill review: codify what you've been doing", "done")

	// app_home_opened drives the publish through the normal event path.
	evt := socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: slackevents.EventsAPIEvent{
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Data: &slackevents.AppHomeOpenedEvent{User: "U777", Tab: "home"},
			},
		},
	}
	if ack := tr.handleEvent(context.Background(), &brokerTransportHost{broker: b}, evt); !ack {
		t.Fatal("app_home_opened must be acked")
	}

	if len(api.publishedViews) != 1 {
		t.Fatalf("expected 1 published view, got %d", len(api.publishedViews))
	}
	pv := api.publishedViews[0]
	if pv.UserID != "U777" {
		t.Fatalf("view published for %q, want U777", pv.UserID)
	}
	raw, err := json.Marshal(pv.View)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	view := string(raw)

	for _, want := range []string{
		"WUPHF Office", "Task board", "Wiki",
		// Lanes render with their emoji titles.
		"🔨 In progress", "🗂 Up next", "✅ Done recently",
		// Tasks deep-link into the web app and carry state + owner.
		"http://127.0.0.1:7905/tasks/OFFICE-1|OFFICE-1", "Ship the pricing facts",
		"OFFICE-2", "OFFICE-3",
		// Surface buttons (actions block) + secondary links.
		"wuphf_home_links", "📋 Task board",
		"http://127.0.0.1:7905/tasks", "http://127.0.0.1:7905/wiki",
		"http://127.0.0.1:7905/inbox", "http://127.0.0.1:7905/agents",
		"http://127.0.0.1:7905/notebooks",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("home view missing %q\nview: %s", want, view)
		}
	}
	if strings.Contains(view, "task-skill-nudge-9") {
		t.Fatalf("system plumbing task leaked onto the board\nview: %s", view)
	}
}

// Without a web URL (broker booted before LaunchWeb, or headless tests) the
// view still renders — no buttons, no links, plain labels.
func TestSlackHomeTabWithoutWebURL(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	tr.publishHomeTab(context.Background(), "U777")
	if len(api.publishedViews) != 1 {
		t.Fatalf("expected 1 published view, got %d", len(api.publishedViews))
	}
	raw, _ := json.Marshal(api.publishedViews[0].View)
	view := string(raw)
	if strings.Contains(view, slackHomeLinksBlockID) {
		t.Fatalf("no surface buttons expected without a web URL, got %s", view)
	}
	if strings.Contains(view, "http://") {
		t.Fatalf("no links expected without a web URL, got %s", view)
	}
}

// The wiki index parser turns the auto-generated markdown into structured
// entries and the card renderer produces preview cards — freshest first,
// linked title, group tag, and a teaser snippet from the article body.
func TestSlackWikiIndexParsingAndPreviewCards(t *testing.T) {
	md := "# Team wiki index\n\n_Auto-generated._\n## team/people\n\n" +
		"- [Nazz](../team/people/nazz.md) _(updated 2026-06-12T20:19:50Z)_\n" +
		"- [Hermes](../team/people/hermes.md) _(updated 2026-06-12T20:19:50Z)_\n" +
		"## team/playbooks\n\n" +
		"- [Billing](../team/playbooks/billing.md) _(updated 2026-06-13T00:00:00Z)_\n"

	entries := parseSlackWikiIndex(md)
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Group != "team/people" || entries[0].Label != "Nazz" || entries[0].Path != "team/people/nazz.md" {
		t.Fatalf("first entry parsed wrong: %+v", entries[0])
	}
	if entries[0].Updated != "2026-06-12T20:19:50Z" {
		t.Fatalf("updated timestamp not parsed: %+v", entries[0])
	}

	repo, worker, _, teardown := newLearningFixture(t)
	defer teardown()
	_ = worker
	write := func(rel, content string) {
		full := filepath.Join(repo.Root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	// A human-authored playbook (no Observations section, no fact count).
	write("team/playbooks/billing.md",
		"---\ntitle: Billing\n---\n\n# Billing\n\nReconcile invoices monthly and flag any mismatched ledger entries before close.[^1]\n\n[^1]: recorded by system.\n")
	// A generated entity article (frontmatter fact count + Observations).
	write("team/people/nazz.md",
		"---\nlast_synthesized_ts: 2026-06-12T20:19:50Z\nfact_count_at_synthesis: 4\n---\n\n# Nazz\n\nNazz is a person in the team knowledge graph, with 4 recorded facts.\n\n## Observations\n\n- Full name: Najmuzzaman Mohammad.[^1]\n- Timezone: Europe/Amsterdam.[^2]\n")

	// Fixed clock: Billing (06-13) is hours old, the people articles (06-12)
	// are ~a day old — both inside the 48h New window.
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	cards := renderSlackWikiCards(entries, repo, "http://x", now)
	if len(cards) != 3 {
		t.Fatalf("want 3 cards, got %d: %+v", len(cards), cards)
	}
	// Freshest first: Billing (06-13) before the people articles (06-12).
	first := cards[0]
	if !strings.Contains(first, "<http://x/wiki/team/playbooks/billing.md|Billing>") {
		t.Fatalf("first card should be the freshest with a deep link: %q", first)
	}
	if !strings.Contains(first, "🆕") || !strings.Contains(first, "New") {
		t.Fatalf("fresh article must wear a New badge: %q", first)
	}
	if !strings.Contains(first, "Reconcile invoices monthly") {
		t.Fatalf("playbook card missing prose teaser: %q", first)
	}
	if strings.Contains(first, "[^1]") || strings.Contains(first, "title: Billing") {
		t.Fatalf("teaser leaked frontmatter or footnote markers: %q", first)
	}

	// The entity card surfaces the fact count + concrete observations, NOT the
	// "in the team knowledge graph" boilerplate intro.
	var nazz string
	for _, c := range cards {
		if strings.Contains(c, "|Nazz>") {
			nazz = c
		}
	}
	if nazz == "" {
		t.Fatalf("no Nazz card rendered: %+v", cards)
	}
	if !strings.Contains(nazz, "4 facts") {
		t.Fatalf("entity card missing fact count: %q", nazz)
	}
	if !strings.Contains(nazz, "Najmuzzaman Mohammad") || !strings.Contains(nazz, "Europe/Amsterdam") {
		t.Fatalf("entity card missing observation teaser: %q", nazz)
	}
	if strings.Contains(nazz, "knowledge graph") {
		t.Fatalf("entity card leaked the boilerplate intro: %q", nazz)
	}

	// Old article: outside the New window, no badge.
	staleEntries := []slackWikiIndexEntry{{Group: "team/playbooks", Label: "Billing", Path: "team/playbooks/billing.md", Updated: "2026-06-12T20:19:50Z"}}
	stale := renderSlackWikiCards(staleEntries, repo, "http://x", time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC))
	if strings.Contains(stale[0], "🆕") {
		t.Fatalf("stale article must not wear a New badge: %q", stale[0])
	}

	// Without a base URL titles render plain.
	plain := renderSlackWikiCards(entries, repo, "", now)
	if strings.Contains(plain[0], "<http") {
		t.Fatalf("plain rendering should have no links: %q", plain[0])
	}
}
