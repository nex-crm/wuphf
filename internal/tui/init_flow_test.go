package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func TestInitFlowStartsWithAPIKeyStepWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitAPIKey {
		t.Fatalf("expected API key phase, got %q", flow.Phase())
	}
}

func TestInitFlowUsesResolvedAPIKeyFromEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_API_KEY", "env-key")

	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitProviderChoice {
		t.Fatalf("expected provider choice phase, got %q", flow.Phase())
	}
	if flow.apiKey != "env-key" {
		t.Fatalf("expected resolved env API key, got %q", flow.apiKey)
	}
}

func TestInitFlowSkipsToPackWhenAPIKeyExists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(config.Config{APIKey: "wuphf-key"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitProviderChoice {
		t.Fatalf("expected provider choice phase, got %q", flow.Phase())
	}
	if flow.provider != "claude-code" {
		t.Fatalf("expected provider to default to claude-code, got %q", flow.provider)
	}
}

func TestInitFlowViewShowsReadinessSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	prevLookPath := initFlowLookPathFn
	initFlowLookPathFn = func(name string) (string, error) {
		switch name {
		case "tmux", "claude":
			return "/usr/bin/" + name, nil
		default:
			return "", fmt.Errorf("%s not found", name)
		}
	}
	t.Cleanup(func() {
		initFlowLookPathFn = prevLookPath
	})

	flow := NewInitFlow()
	flow.phase = InitAPIKey
	flow.provider = "claude-code"

	view := flow.View()
	if !containsAll(view, "Setup Readiness", "Nex identity", "tmux office runtime", "LLM runtime", "Agent pack") {
		t.Fatalf("expected readiness summary in init view, got %q", view)
	}
	if !strings.Contains(view, "Paste your WUPHF/Nex API key") {
		t.Fatalf("expected API key guidance in readiness summary, got %q", view)
	}
}

func TestInitFlowMentionsManagedIntegrations(t *testing.T) {
	heading, instructions := NewInitFlow().phaseText()
	if heading != "Setup" || instructions == "" {
		t.Fatalf("unexpected idle phase text: %q / %q", heading, instructions)
	}

	flow := NewInitFlow()
	flow.phase = InitAPIKey
	_, instructions = flow.phaseText()
	if instructions == "" || !containsAll(instructions, "One", "automatically") {
		t.Fatalf("expected managed integration copy, got %q", instructions)
	}
}

func TestProviderOptionsIncludeCodex(t *testing.T) {
	options := ProviderOptions()
	for _, opt := range options {
		if opt.Value == "codex" {
			return
		}
	}
	t.Fatal("expected codex provider option")
}

func TestProviderOptionsOnlyExposeClaudeAndCodex(t *testing.T) {
	options := ProviderOptions()
	values := make([]string, 0, len(options))
	for _, opt := range options {
		values = append(values, opt.Value)
	}
	joined := strings.Join(values, ",")
	if strings.Contains(joined, "gemini") || strings.Contains(joined, "nex-ask") {
		t.Fatalf("expected provider options to hide gemini and nex-ask, got %q", joined)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
