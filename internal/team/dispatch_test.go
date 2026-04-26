package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

func TestNormalizeProviderKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"", provider.KindClaudeCode},       // empty → default to claude-code
		{"claude", provider.KindClaudeCode}, // legacy alias
		{"Claude", provider.KindClaudeCode}, // case-insensitive
		{" codex ", provider.KindCodex},     // trim
		{"CODEX", provider.KindCodex},       // uppercase
		{"claude-code", provider.KindClaudeCode},
		{"openclaw", provider.KindOpenclaw},
		{"gemini", "gemini"}, // unknown passes through so dispatch can error
	}
	for _, tt := range tests {
		if got := normalizeProviderKind(tt.in); got != tt.want {
			t.Errorf("normalizeProviderKind(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMemberEffectiveProviderKind_PerAgentWins(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug:     "pm-codex",
		Name:     "PM Codex",
		Provider: provider.ProviderBinding{Kind: provider.KindCodex},
	})
	b.memberIndex = nil // force rebuild
	b.mu.Unlock()

	l := &Launcher{broker: b, provider: "claude-code"}
	if got := l.memberEffectiveProviderKind("pm-codex"); got != provider.KindCodex {
		t.Fatalf("per-agent should win over global, got %q", got)
	}
}

func TestMemberEffectiveProviderKind_FallsBackToGlobal(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug: "no-binding",
		Name: "No Binding",
	})
	b.memberIndex = nil
	b.mu.Unlock()

	l := &Launcher{broker: b, provider: "codex"}
	if got := l.memberEffectiveProviderKind("no-binding"); got != provider.KindCodex {
		t.Fatalf("empty Kind should fall back to global=codex, got %q", got)
	}

	// Unknown member also falls back to global (dispatch then errors if global is bad).
	if got := l.memberEffectiveProviderKind("nobody"); got != provider.KindCodex {
		t.Fatalf("unknown slug should fall back to global=codex, got %q", got)
	}
}

func TestMemberEffectiveProviderKind_DefaultsToClaudeWhenAllEmpty(t *testing.T) {
	b := newTestBroker(t)
	l := &Launcher{broker: b, provider: ""}
	// Fully empty globals fall through to claude-code. This preserves the
	// install-default behavior that predated per-agent providers.
	if got := l.memberEffectiveProviderKind("anybody"); got != provider.KindClaudeCode {
		t.Fatalf("default fallback should be claude-code, got %q", got)
	}
}

func TestShouldUseHeadlessDispatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		provider         string
		webMode          bool
		paneBackedAgents bool
		want             bool
	}{
		// Both web and TUI modes default to headless: `claude --print` per turn
		// is the primary dispatch path; pane-backed dispatch is reserved as an
		// internal fallback that sets paneBackedAgents=true.
		{"tui mode, claude, no panes → headless", "claude-code", false, false, true},
		{"tui mode, codex → headless", "codex", false, false, true},
		{"tui mode with panes (fallback active) → pane", "claude-code", false, true, false},
		{"web mode, no panes → headless", "claude-code", true, false, true},
		{"web mode with panes (fallback active) → pane", "claude-code", true, true, false},
		{"web mode, codex always headless even when panes flag set", "codex", true, true, true},
	}
	for _, tt := range tests {
		l := &Launcher{
			provider:         tt.provider,
			webMode:          tt.webMode,
			paneBackedAgents: tt.paneBackedAgents,
		}
		if got := l.shouldUseHeadlessDispatch(); got != tt.want {
			t.Errorf("%s: shouldUseHeadlessDispatch() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBrokerMemberProviderKind_Lookup(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug:     "eng-openclaw",
		Name:     "Eng Openclaw",
		Provider: provider.ProviderBinding{Kind: provider.KindOpenclaw, Model: "openai-codex/gpt-5.4"},
	})
	b.memberIndex = nil
	b.mu.Unlock()

	if got := b.MemberProviderKind("eng-openclaw"); got != provider.KindOpenclaw {
		t.Fatalf("MemberProviderKind = %q, want %q", got, provider.KindOpenclaw)
	}
	if binding := b.MemberProviderBinding("eng-openclaw"); binding.Model != "openai-codex/gpt-5.4" {
		t.Fatalf("MemberProviderBinding lost model: %+v", binding)
	}
	// Unknown slug returns zero value, not error.
	if got := b.MemberProviderKind("missing"); got != "" {
		t.Fatalf("unknown slug should return empty, got %q", got)
	}
}
