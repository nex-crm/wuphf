package provider_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/provider"
)

// TestClaudeStreamFnBuilds verifies the function compiles and returns a channel.
// We don't actually exec `claude` in CI — just confirm the factory doesn't panic.
func TestClaudeStreamFnBuilds(t *testing.T) {
	fn := provider.CreateClaudeCodeStreamFn("test-agent")
	if fn == nil {
		t.Fatal("expected non-nil StreamFn")
	}
}

func TestNexAskStreamFn_NoAPIKey(t *testing.T) {
	c := api.NewClient("") // no key
	fn := provider.CreateNexAskStreamFn(c)

	msgs := []agent.Message{{Role: "user", Content: "ping"}}
	ch := fn(msgs, nil)

	var chunks []agent.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content == "" {
		t.Error("expected non-empty fallback content")
	}
}

func TestNexAskStreamFn_WithMockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"answer": "pong"})
	}))
	defer srv.Close()

	c := api.NewClient("test-key")
	c.BaseURL = srv.URL

	fn := provider.CreateNexAskStreamFn(c)
	msgs := []agent.Message{{Role: "user", Content: "ping"}}
	ch := fn(msgs, nil)

	var chunks []agent.StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != "pong" {
		t.Errorf("expected 'pong', got %q", chunks[0].Content)
	}
}
