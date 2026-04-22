package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// TestUsesPaneRuntime_ConsultsRegistryCapabilities is the epicentric red test
// for P0.2: install-wide pane-eligibility must come from the Registry, not
// from the binary `provider == "codex"` predicate.
//
// Today UsesTmuxRuntime() returns !usesCodexRuntime(), so any non-codex value
// (including a hypothetical "ollama" or "openai-compatible") is reported as
// pane-eligible — wrong for runtimes that talk to an HTTP API instead of
// running an interactive TUI in a tmux pane.
//
// Before the refactor: UsesTmuxRuntime() == true for the fake non-pane Kind
//
//	(because the fake isn't "codex"). Test FAILS.
//
// After the refactor:  UsesTmuxRuntime() consults provider.CapabilitiesFor
//
//	and returns the registered PaneEligible value. Test PASSES.
func TestUsesPaneRuntime_ConsultsRegistryCapabilities(t *testing.T) {
	const fakeKind = "wuphf-test-fake-non-pane"
	provider.RegisterForTest(t, &provider.Entry{
		Kind: fakeKind,
		StreamFn: func(slug string) agent.StreamFn {
			return func([]agent.Message, []agent.AgentTool) <-chan agent.StreamChunk {
				ch := make(chan agent.StreamChunk)
				close(ch)
				return ch
			}
		},
		Capabilities: provider.Capabilities{PaneEligible: false},
	})

	l := &Launcher{provider: fakeKind}
	if l.UsesTmuxRuntime() {
		t.Fatal("UsesTmuxRuntime() returned true for a non-pane-eligible provider; " +
			"the predicate must consult provider.CapabilitiesFor, not just !codex")
	}
}

// TestUsesPaneRuntime_PaneEligibleProviderStaysTrue confirms that pane-eligible
// runtimes (Claude Code today, future Ollama variants if any) keep their
// pane-runtime status after the refactor.
func TestUsesPaneRuntime_PaneEligibleProviderStaysTrue(t *testing.T) {
	const fakeKind = "wuphf-test-fake-pane"
	provider.RegisterForTest(t, &provider.Entry{
		Kind: fakeKind,
		StreamFn: func(slug string) agent.StreamFn {
			return func([]agent.Message, []agent.AgentTool) <-chan agent.StreamChunk {
				ch := make(chan agent.StreamChunk)
				close(ch)
				return ch
			}
		},
		Capabilities: provider.Capabilities{PaneEligible: true},
	})

	l := &Launcher{provider: fakeKind}
	if !l.UsesTmuxRuntime() {
		t.Fatal("UsesTmuxRuntime() returned false for a pane-eligible provider")
	}
}

// TestRequiresClaudeSessionReset_OnlyTrueForClaude pins the second capability:
// only providers that populate provider.ResetClaudeSessions's session store
// should trigger a reset when ResetSession runs. Codex doesn't; a future
// Ollama provider doesn't; only Claude Code does.
func TestRequiresClaudeSessionReset_OnlyTrueForClaude(t *testing.T) {
	if !provider.CapabilitiesFor(provider.KindClaudeCode).RequiresClaudeSessionReset {
		t.Error("Claude Code should declare RequiresClaudeSessionReset=true")
	}
	if provider.CapabilitiesFor(provider.KindCodex).RequiresClaudeSessionReset {
		t.Error("Codex should declare RequiresClaudeSessionReset=false (no Claude session state)")
	}
}
