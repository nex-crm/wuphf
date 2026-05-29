package team

// broker_rich_artifact_card_test.go covers defect D2: creating a visual
// artifact auto-creates a canonical notebook home, and the create handler must
// announce that new entry with a notebook_entry_created chat card — the same
// signal the manual notebook-write path emits. Without the card the entry is
// only discoverable by accident.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestBrokerNotebookVisualArtifactCreateEmitsEntryCard(t *testing.T) {
	srv, b, teardown := newNotebookTestServer(t)
	defer teardown()
	token := b.Token()

	// Create a visual artifact with no source_markdown_path so the worker
	// auto-creates a canonical notebook home — the exact path defect D2 left
	// silent.
	createBody, _ := json.Marshal(map[string]any{
		"slug":           "pm",
		"title":          "Pipeline Overview",
		"summary":        "How the pipeline fits together.",
		"html":           "<html><body><h1>Pipeline Overview</h1><svg></svg></body></html>",
		"commit_message": "add pipeline overview",
	})
	req, _ := authReq(http.MethodPost, srv.URL+"/notebook/visual-artifacts", bytes.NewReader(createBody), token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("visual artifact create: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", res.StatusCode, string(body))
	}

	var resp struct {
		Artifact RichArtifact `json:"artifact"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode create response: %v (body=%s)", err, string(body))
	}
	if resp.Artifact.AttachedToNotebookEntry == nil {
		t.Fatalf("expected auto-created notebook home, got nil AttachedToNotebookEntry (body=%s)", string(body))
	}

	cards := messagesByKind(b, "notebook_entry_created")
	if len(cards) != 1 {
		t.Fatalf("expected 1 notebook_entry_created card, got %d", len(cards))
	}
	card := cards[0]
	if card.Channel != "general" {
		t.Errorf("card channel: want general, got %q", card.Channel)
	}
	if card.From != "system" {
		t.Errorf("card from: want system, got %q", card.From)
	}

	p := mustUnmarshalPayload(t, card.Payload)
	if got, want := p["slug"], resp.Artifact.AttachedToNotebookEntry.OwnerSlug; got != want {
		t.Errorf("payload.slug: got %q want %q", got, want)
	}
	if got, want := p["path"], resp.Artifact.SourceMarkdownPath; got != want {
		t.Errorf("payload.path: got %q want %q", got, want)
	}
	if got := p["title"]; got != "Pipeline Overview" {
		t.Errorf("payload.title: got %q want %q", got, "Pipeline Overview")
	}
}
