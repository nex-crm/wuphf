package provider

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
)

// TestRegistry_FakeKindRoutesEverywhere is the epicentric red test for the
// provider Registry: register a fake Kind, set it as the active provider, and
// confirm that BOTH the streaming resolver and the one-shot dispatcher route
// to the fake — not to claude-code's default fallback.
//
// Before the Registry exists, this test fails because:
//   - resolver.go has a hardcoded switch that knows only "claude-code"/"codex"
//     and falls through to CreateClaudeCodeStreamFn for unknown values.
//   - oneshot.go has the same closed switch, falling through to RunClaudeOneShot.
//
// After the Registry exists and resolver+oneshot dispatch through it, the fake's
// StreamFn and OneShot are invoked and the assertions pass.
func TestRegistry_FakeKindRoutesEverywhere(t *testing.T) {
	const fakeKind = "wuphf-test-fake-provider"

	var streamFnHits, oneShotHits int
	RegisterForTest(t, &Entry{
		Kind: fakeKind,
		StreamFn: func(slug string) agent.StreamFn {
			return func([]agent.Message, []agent.AgentTool) <-chan agent.StreamChunk {
				streamFnHits++
				ch := make(chan agent.StreamChunk)
				close(ch)
				return ch
			}
		},
		OneShot: func(systemPrompt, prompt, cwd string) (string, error) {
			oneShotHits++
			return "fake-oneshot-result", nil
		},
		Capabilities: Capabilities{SupportsOneShot: true},
	})

	t.Setenv("WUPHF_LLM_PROVIDER", fakeKind)

	// Path 1: streaming resolver.
	fn := DefaultStreamFnResolver(nil, nil)("agent-slug")
	if fn == nil {
		t.Fatal("resolver returned nil StreamFn for registered fake kind")
	}
	for range fn(nil, nil) {
		// drain
	}
	if streamFnHits == 0 {
		t.Fatal("streaming resolver did not route to fake provider via Registry — still hardcoded switch?")
	}

	// Path 2: one-shot dispatch.
	out, err := RunConfiguredOneShot("sys", "prompt", "/tmp")
	if err != nil {
		t.Fatalf("RunConfiguredOneShot returned error: %v", err)
	}
	if out != "fake-oneshot-result" {
		t.Fatalf("RunConfiguredOneShot returned %q, want %q — did not route to fake via Registry",
			out, "fake-oneshot-result")
	}
	if oneShotHits == 0 {
		t.Fatal("one-shot dispatcher did not route to fake provider via Registry")
	}
}

// TestRegistry_LookupReturnsNilForUnknown documents that a non-registered Kind
// returns nil so dispatchers know to fall back to a default (claude-code).
func TestRegistry_LookupReturnsNilForUnknown(t *testing.T) {
	if e := Lookup("wuphf-never-registered-kind"); e != nil {
		t.Fatalf("Lookup returned non-nil entry %+v for unregistered kind", e)
	}
}

// TestStreamFnResolver_PerAgentKindOverridesInstallWide is the epicentric red
// test for P0.3: a per-agent ProviderBinding must take priority over the
// install-wide default when the streaming resolver routes a turn.
//
// Today the resolver ignores its agentSlug argument and reads only
// config.ResolveLLMProvider, so an Ollama-bound agent on a claude-default
// install gets routed to claude — the broker's per-agent ProviderBinding
// data layer (broker.MemberProviderKind / memberEffectiveProviderKind in
// the launcher) doesn't reach the StreamFn dispatch.
//
// The fix threads an optional ProviderKindResolver through
// DefaultStreamFnResolver. When set, the per-agent kind wins; when nil,
// resolution falls back to the install-wide ResolveLLMProvider.
func TestStreamFnResolver_PerAgentKindOverridesInstallWide(t *testing.T) {
	const installKind = "wuphf-test-install-wide-kind"
	const agentKind = "wuphf-test-per-agent-kind"

	var installHits, agentHits int
	RegisterForTest(t, &Entry{
		Kind: installKind,
		StreamFn: func(slug string) agent.StreamFn {
			return func([]agent.Message, []agent.AgentTool) <-chan agent.StreamChunk {
				installHits++
				ch := make(chan agent.StreamChunk)
				close(ch)
				return ch
			}
		},
	})
	RegisterForTest(t, &Entry{
		Kind: agentKind,
		StreamFn: func(slug string) agent.StreamFn {
			return func([]agent.Message, []agent.AgentTool) <-chan agent.StreamChunk {
				agentHits++
				ch := make(chan agent.StreamChunk)
				close(ch)
				return ch
			}
		},
	})

	t.Setenv("WUPHF_LLM_PROVIDER", installKind)

	kindResolver := func(slug string) string {
		if slug == "agent-on-per-agent-binding" {
			return agentKind
		}
		return "" // empty → fall back to install-wide
	}

	resolver := DefaultStreamFnResolver(nil, kindResolver)

	// Agent with a per-agent binding routes to agentKind, not installKind.
	for range resolver("agent-on-per-agent-binding")(nil, nil) {
	}
	if agentHits != 1 || installHits != 0 {
		t.Fatalf("per-agent override did not take priority: agentHits=%d installHits=%d", agentHits, installHits)
	}

	// Agent without a per-agent binding falls back to install-wide kind.
	for range resolver("agent-using-install-default")(nil, nil) {
	}
	if installHits != 1 {
		t.Fatalf("fallback to install-wide kind did not work: installHits=%d", installHits)
	}
}

// TestRegistry_BuiltinsRegistered ensures the package's init() registers the
// shipped providers so external callers (resolver, oneshot, future capability
// checks) can rely on Lookup("claude-code") and Lookup("codex").
func TestRegistry_BuiltinsRegistered(t *testing.T) {
	for _, kind := range []string{KindClaudeCode, KindCodex} {
		if e := Lookup(kind); e == nil {
			t.Errorf("builtin Kind %q not registered — init() missing or out of order", kind)
		}
	}
}
