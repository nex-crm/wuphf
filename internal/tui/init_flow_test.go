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
	if !containsAll(view, "Setup Readiness", "Memory backend", "Nex API key", "tmux office runtime", "LLM runtime", "Agent pack") {
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
	if instructions == "" || !containsAll(instructions, "Nex", "managed integrations") {
		t.Fatalf("expected Nex setup copy, got %q", instructions)
	}
}

func TestInitFlowStartsWithGBrainProviderKeyWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)

	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitAPIKey {
		t.Fatalf("expected API key phase for gbrain, got %q", flow.Phase())
	}

	flow.phase = InitAPIKey
	view := flow.View()
	if !containsAll(view, "GBrain", "OpenAI or Anthropic", "embeddings", "reduced mode") {
		t.Fatalf("expected GBrain key guidance, got %q", view)
	}
}

func TestInitFlowSkipsGBrainKeyStepWhenProviderCredentialExists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-test-openai")

	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitProviderChoice {
		t.Fatalf("expected provider choice phase, got %q", flow.Phase())
	}
}

func TestInitFlowFinishSavesGBrainKeysByProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)

	flow := NewInitFlow()
	flow.apiKey = "sk-ant-test-anthropic"
	flow.provider = "codex"
	flow.pack = "founding-team"
	if _, cmd := flow.finish(); cmd == nil {
		t.Fatal("expected finish to emit a phase transition")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AnthropicAPIKey != "sk-ant-test-anthropic" {
		t.Fatalf("expected anthropic key to be saved, got %#v", cfg)
	}
	if cfg.OpenAIAPIKey != "" {
		t.Fatalf("did not expect OpenAI key to be set, got %#v", cfg)
	}
}

func TestInitFlowAnthropicOnlyExplainsReducedMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_ANTHROPIC_API_KEY", "sk-ant-test-anthropic")

	flow := NewInitFlow()
	flow.phase = InitProviderChoice
	view := flow.View()

	if !containsAll(view, "reduced mode", "OpenAI", "vector search") {
		t.Fatalf("expected Anthropic-only reduced mode guidance, got %q", view)
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
