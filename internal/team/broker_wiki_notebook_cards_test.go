package team

// Unit tests for broker_wiki_notebook_cards.go — proves each helper
// appends exactly one #general message with the expected kind and
// payload shape. Each test takes b.mu the same way production callers
// do so the lock-discipline invariant the helpers depend on is
// exercised here too.

import (
	"encoding/json"
	"testing"
)

func messagesByKind(b *Broker, kind string) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []channelMessage
	for _, m := range b.messages {
		if m.Kind == kind {
			out = append(out, m)
		}
	}
	return out
}

func mustUnmarshalPayload(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("payload is empty")
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal payload: %v (raw=%s)", err, string(raw))
	}
	return out
}

func TestPostWikiArticleCreatedCardLocked_EmitsCard(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postWikiArticleCreatedCardLocked("team/people/nazz.md", "Nazz", "ceo")
	b.mu.Unlock()

	cards := messagesByKind(b, "wiki_article_created")
	if len(cards) != 1 {
		t.Fatalf("expected 1 wiki_article_created card, got %d", len(cards))
	}
	card := cards[0]
	if card.Channel != "general" {
		t.Errorf("channel: want general, got %q", card.Channel)
	}
	if card.From != "system" {
		t.Errorf("from: want system, got %q", card.From)
	}
	p := mustUnmarshalPayload(t, card.Payload)
	if p["path"] != "team/people/nazz.md" {
		t.Errorf("payload.path: %q", p["path"])
	}
	if p["title"] != "Nazz" {
		t.Errorf("payload.title: %q", p["title"])
	}
	if p["author"] != "ceo" {
		t.Errorf("payload.author: %q", p["author"])
	}
}

func TestPostWikiArticleCreatedCardLocked_EmptyPathSuppresses(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postWikiArticleCreatedCardLocked("   ", "Title", "ceo")
	b.mu.Unlock()
	if cards := messagesByKind(b, "wiki_article_created"); len(cards) != 0 {
		t.Fatalf("empty path must suppress card, got %d", len(cards))
	}
}

func TestPostNotebookEntryCreatedCardLocked_EmitsCard(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postNotebookEntryCreatedCardLocked(
		"pm",
		"agents/pm/notebook/2026-05-28-handoff.md",
		"Handoff",
		"pm",
	)
	b.mu.Unlock()

	cards := messagesByKind(b, "notebook_entry_created")
	if len(cards) != 1 {
		t.Fatalf("expected 1 notebook_entry_created card, got %d", len(cards))
	}
	card := cards[0]
	if card.Channel != "general" {
		t.Errorf("channel: want general, got %q", card.Channel)
	}
	p := mustUnmarshalPayload(t, card.Payload)
	if p["slug"] != "pm" {
		t.Errorf("payload.slug: %q", p["slug"])
	}
	if p["title"] != "Handoff" {
		t.Errorf("payload.title: %q", p["title"])
	}
	if p["author"] != "pm" {
		t.Errorf("payload.author: %q", p["author"])
	}
	// Author should also be in tagged so the author gets a chat ping.
	foundAuthor := false
	for _, tag := range card.Tagged {
		if tag == "pm" {
			foundAuthor = true
		}
	}
	if !foundAuthor {
		t.Errorf("tagged: expected %q in %v", "pm", card.Tagged)
	}
}

func TestPostNotebookPromotionRequestedCardLocked_EmitsCard(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postNotebookPromotionRequestedCardLocked(
		"prom-1",
		"agents/eng/notebook/2026-05-28-fix.md",
		"team/playbooks/fix.md",
		"eng",
	)
	b.mu.Unlock()

	cards := messagesByKind(b, "notebook_promotion_requested")
	if len(cards) != 1 {
		t.Fatalf("expected 1 notebook_promotion_requested card, got %d", len(cards))
	}
	card := cards[0]
	if card.Channel != "general" {
		t.Errorf("channel: want general, got %q", card.Channel)
	}
	p := mustUnmarshalPayload(t, card.Payload)
	if p["promotion_id"] != "prom-1" {
		t.Errorf("payload.promotion_id: %q", p["promotion_id"])
	}
	if p["target_path"] != "team/playbooks/fix.md" {
		t.Errorf("payload.target_path: %q", p["target_path"])
	}
	if p["submitter"] != "eng" {
		t.Errorf("payload.submitter: %q", p["submitter"])
	}
}

func TestPostNotebookPromotionResolvedCardLocked_Approved(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postNotebookPromotionResolvedCardLocked(
		"prom-2",
		"agents/eng/notebook/x.md",
		"team/playbooks/x.md",
		"ceo",
		PromotionDecisionApproved,
		"LGTM",
		"eng",
	)
	b.mu.Unlock()

	cards := messagesByKind(b, "notebook_promotion_resolved")
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	p := mustUnmarshalPayload(t, cards[0].Payload)
	if p["decision"] != "approved" {
		t.Errorf("payload.decision: want approved, got %q", p["decision"])
	}
	if p["reviewer"] != "ceo" {
		t.Errorf("payload.reviewer: %q", p["reviewer"])
	}
	if p["submitter"] != "eng" {
		t.Errorf("payload.submitter: %q", p["submitter"])
	}
}

func TestPostNotebookPromotionResolvedCardLocked_ChangesRequested(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postNotebookPromotionResolvedCardLocked(
		"prom-3",
		"agents/eng/notebook/x.md",
		"team/playbooks/x.md",
		"ceo",
		PromotionDecisionChangesRequested,
		"please clarify",
		"eng",
	)
	b.mu.Unlock()

	cards := messagesByKind(b, "notebook_promotion_resolved")
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	p := mustUnmarshalPayload(t, cards[0].Payload)
	if p["decision"] != "changes_requested" {
		t.Errorf("payload.decision: want changes_requested, got %q", p["decision"])
	}
	if p["rationale"] != "please clarify" {
		t.Errorf("payload.rationale: %q", p["rationale"])
	}
}

func TestPostNotebookPromotionResolvedCardLocked_UnknownDecisionSuppressed(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.postNotebookPromotionResolvedCardLocked(
		"prom-4",
		"src",
		"tgt",
		"ceo",
		PromotionDecision("rejected"), // not one of the two valid decisions
		"",
		"eng",
	)
	b.mu.Unlock()

	if cards := messagesByKind(b, "notebook_promotion_resolved"); len(cards) != 0 {
		t.Fatalf("unknown decision must suppress card, got %d", len(cards))
	}
}
