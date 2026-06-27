package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nex-crm/wuphf/internal/gbrain"
)

// TestHandleKnowledgeEmbeddingOptionsShape drives the handler over httptest and
// asserts the JSON contract the onboarding UI depends on. It is deliberately
// env-tolerant: the gbrain-on-PATH / OpenAI-key / Ollama state varies by
// machine, so it asserts the field set, types, and the invariants that must hold
// regardless of environment (e.g. gbrain_installed reflects gbrain.IsInstalled()
// in this test process).
func TestHandleKnowledgeEmbeddingOptionsShape(t *testing.T) {
	b := newTestBroker(t)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	// Decode into a generic map first to assert every contract field is present
	// with the right JSON type.
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, w.Body.String())
	}
	boolFields := []string{"gbrain_installed", "openai_key_set", "ollama_available", "embedding_available"}
	for _, f := range boolFields {
		v, ok := raw[f]
		if !ok {
			t.Fatalf("missing field %q in response: %s", f, w.Body.String())
		}
		if _, ok := v.(bool); !ok {
			t.Errorf("field %q: got %T, want bool", f, v)
		}
	}
	stringFields := []string{"ollama_model", "active_embedder", "recommended"}
	for _, f := range stringFields {
		v, ok := raw[f]
		if !ok {
			t.Fatalf("missing field %q in response: %s", f, w.Body.String())
		}
		if _, ok := v.(string); !ok {
			t.Errorf("field %q: got %T, want string", f, v)
		}
	}

	// Decode into the typed shape to assert cross-field invariants.
	var opts knowledgeEmbeddingOptions
	if err := json.Unmarshal(w.Body.Bytes(), &opts); err != nil {
		t.Fatalf("decode typed response: %v", err)
	}
	if opts.GBrainInstalled != gbrain.IsInstalled() {
		t.Errorf("gbrain_installed: got %v, want %v", opts.GBrainInstalled, gbrain.IsInstalled())
	}
	// embedding_available must agree with active_embedder being non-empty.
	if opts.EmbeddingAvailable != (opts.ActiveEmbedder != "") {
		t.Errorf("embedding_available (%v) disagrees with active_embedder (%q)", opts.EmbeddingAvailable, opts.ActiveEmbedder)
	}
	// ollama_available must agree with ollama_model being non-empty.
	if opts.OllamaAvailable != (opts.OllamaModel != "") {
		t.Errorf("ollama_available (%v) disagrees with ollama_model (%q)", opts.OllamaAvailable, opts.OllamaModel)
	}
	// recommended is one of the documented values and matches the gate.
	switch opts.Recommended {
	case "openai", "keyword":
	default:
		t.Errorf("recommended: got %q, want one of openai|keyword", opts.Recommended)
	}
	if opts.GBrainInstalled && opts.Recommended != "openai" {
		t.Errorf("recommended must be openai when gbrain installed, got %q", opts.Recommended)
	}
	if !opts.GBrainInstalled && opts.Recommended != "keyword" {
		t.Errorf("recommended must be keyword when gbrain absent, got %q", opts.Recommended)
	}
}

// TestHandleKnowledgeEmbeddingOptionsRejectsNonGET asserts the handler follows
// the repo's method-guard pattern.
func TestHandleKnowledgeEmbeddingOptionsRejectsNonGET(t *testing.T) {
	b := newTestBroker(t)
	req := httptest.NewRequest(http.MethodPost, "/knowledge/embedding-options", nil)
	w := httptest.NewRecorder()
	b.handleKnowledgeEmbeddingOptions(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", w.Code)
	}
}

// TestRecommendedEmbedder pins the documented recommendation logic.
func TestRecommendedEmbedder(t *testing.T) {
	if got := recommendedEmbedder(true); got != "openai" {
		t.Errorf("recommendedEmbedder(installed): got %q, want openai", got)
	}
	if got := recommendedEmbedder(false); got != "keyword" {
		t.Errorf("recommendedEmbedder(absent): got %q, want keyword", got)
	}
}
