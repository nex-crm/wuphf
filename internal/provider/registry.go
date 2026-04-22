package provider

import (
	"fmt"
	"sync"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
)

// Capabilities describes how a provider integrates with the team launcher.
//
// Capabilities are consumed by team-side dispatch logic (pane spawning,
// cleanup, session reset) so that adding a new provider Kind does not require
// editing every conditional in launcher.go — instead, the provider declares
// what it supports and the launcher reads those declarations.
type Capabilities struct {
	// PaneEligible reports whether the launcher should spawn an interactive
	// tmux pane for an agent bound to this provider. True for runtimes with
	// an interactive TUI (Claude Code). False for headless-only runtimes
	// (Codex, OpenAI-compatible HTTP, OpenClaw bridge, etc.).
	PaneEligible bool

	// SupportsOneShot reports whether the provider implements OneShot. False
	// providers fall back to the default one-shot path (currently Claude).
	SupportsOneShot bool

	// RequiresClaudeSessionReset reports whether switching the install-wide
	// default away from this provider should also wipe the Claude session
	// store. Today only Claude Code populates that store.
	RequiresClaudeSessionReset bool
}

// Entry is a registered provider's runtime hooks plus its capabilities.
//
// StreamFn is required (every provider must support streaming). OneShot is
// optional — providers without a one-shot implementation set Capabilities
// .SupportsOneShot = false and leave OneShot nil; RunConfiguredOneShot then
// falls back to claude-code.
type Entry struct {
	Kind         string
	StreamFn     func(slug string) agent.StreamFn
	OneShot      func(systemPrompt, prompt, cwd string) (string, error)
	Capabilities Capabilities
}

var (
	registryMu sync.RWMutex
	registry   = map[string]*Entry{}
)

// Register installs a provider Entry. It also teaches the config layer to
// accept e.Kind as a valid value for the WUPHF_LLM_PROVIDER env var, the
// config file, and CLI --provider flags. Intended for use from package init().
//
// Panics if e is nil, e.Kind is empty, or e.Kind is already registered —
// duplicate registration indicates a programming error (two init() calls for
// the same Kind), not user input.
func Register(e *Entry) {
	if e == nil {
		panic("provider: Register requires non-nil Entry")
	}
	if e.Kind == "" {
		panic("provider: Register requires non-empty Entry.Kind")
	}
	if e.StreamFn == nil {
		panic(fmt.Sprintf("provider: Register Kind %q requires non-nil StreamFn", e.Kind))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[e.Kind]; exists {
		panic(fmt.Sprintf("provider: Kind %q already registered", e.Kind))
	}
	registry[e.Kind] = e
	config.AllowLLMProviderKind(e.Kind)
}

// Lookup returns the registered Entry for kind, or nil if no provider with
// that Kind has been registered. Callers that need a fallback (e.g., the
// streaming resolver, the one-shot dispatcher) check for nil and use the
// claude-code default.
func Lookup(kind string) *Entry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[kind]
}

// CapabilitiesFor returns the capabilities for kind, or the zero value if
// kind is not registered. The zero value (PaneEligible=false,
// SupportsOneShot=false, RequiresClaudeSessionReset=false) is the safe default
// — it skips pane spawning and falls back to the default one-shot path.
func CapabilitiesFor(kind string) Capabilities {
	if e := Lookup(kind); e != nil {
		return e.Capabilities
	}
	return Capabilities{}
}

// RegisterForTest installs e for the duration of the test, restoring the prior
// registration (if any) on cleanup. Use this in tests that need to inject a
// fake provider without conflicting with init()-time registration of the
// shipped providers.
//
// Like Register, this also teaches config.normalizeLLMProvider to accept
// e.Kind so that t.Setenv("WUPHF_LLM_PROVIDER", e.Kind) round-trips through
// config.ResolveLLMProvider. The config-side allowlist entry is permanent —
// the kind name remains valid for the rest of the test binary's lifetime —
// but Lookup returns the prior entry (or nil) once the test ends.
func RegisterForTest(t testing.TB, e *Entry) {
	t.Helper()
	if e == nil || e.Kind == "" {
		t.Fatal("RegisterForTest: Entry must be non-nil with non-empty Kind")
	}
	registryMu.Lock()
	prev, hadPrev := registry[e.Kind]
	registry[e.Kind] = e
	registryMu.Unlock()
	config.AllowLLMProviderKind(e.Kind)
	t.Cleanup(func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		if hadPrev {
			registry[e.Kind] = prev
		} else {
			delete(registry, e.Kind)
		}
	})
}
