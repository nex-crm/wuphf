package tui

import (
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

func TestInitFlowSkipsToPackWhenAPIKeyExists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(config.Config{APIKey: "wuphf-key"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	flow, _ := NewInitFlow().Start()
	if flow.Phase() != InitPackChoice {
		t.Fatalf("expected pack choice phase, got %q", flow.Phase())
	}
	if flow.provider != "claude-code" {
		t.Fatalf("expected provider to default to claude-code, got %q", flow.provider)
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

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
