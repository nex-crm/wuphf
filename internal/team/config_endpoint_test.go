package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigEndpointAndHealth is a smoke test for ISSUE-004: the wizard's
// POST /config must persist llm_provider and /health must reflect it.
func TestConfigEndpointAndHealth(t *testing.T) {
	// Redirect config file to a temp HOME so we don't clobber user state.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".wuphf"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Seed config with claude-code, then POST codex.
	initial := `{"llm_provider":"claude-code"}`
	if err := os.WriteFile(filepath.Join(tmp, ".wuphf", "config.json"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	b := NewBroker()
	b.runtimeProvider = "claude-code"
	b.token = "test-token"
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer func() {
		if b.server != nil {
			_ = b.server.Shutdown(context.Background())
		}
	}()

	// /health before — should be claude-code (the launcher-seeded default)
	healthURL := "http://" + b.addr + "/health"
	resp, err := http.Get(healthURL)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	var h1 map[string]any
	raw1, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = json.Unmarshal(raw1, &h1)
	t.Logf("GET /health (initial) -> %s", string(raw1))
	if p, _ := h1["provider"].(string); p != "claude-code" {
		t.Fatalf("expected provider=claude-code before POST, got %q", p)
	}

	// POST /config with codex — simulates the wizard tile click
	body := bytes.NewBufferString(`{"llm_provider":"codex"}`)
	req, _ := http.NewRequest(http.MethodPost, "http://"+b.addr+"/config", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /config: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("POST /config {llm_provider:codex} -> %d %s", resp.StatusCode, string(raw))
	if resp.StatusCode != 200 {
		t.Fatalf("POST /config status=%d body=%s", resp.StatusCode, string(raw))
	}

	// /health after — should be codex
	resp, err = http.Get(healthURL)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	var h2 map[string]any
	raw2, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	_ = json.Unmarshal(raw2, &h2)
	t.Logf("GET /health (after POST) -> %s", string(raw2))
	if p, _ := h2["provider"].(string); p != "codex" {
		t.Fatalf("expected provider=codex after POST, got %q", p)
	}

	// Verify persisted to disk
	disk, _ := os.ReadFile(filepath.Join(tmp, ".wuphf", "config.json"))
	if !strings.Contains(string(disk), `"llm_provider": "codex"`) {
		t.Fatalf("config.json missing codex: %s", string(disk))
	}

	// Reject unsupported provider
	body = bytes.NewBufferString(`{"llm_provider":"anthropic"}`)
	req, _ = http.NewRequest(http.MethodPost, "http://"+b.addr+"/config", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /config reject: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported provider, got %d", resp.StatusCode)
	}

}
