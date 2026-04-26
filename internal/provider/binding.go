package provider

import "fmt"

// Kind values for ProviderBinding.Kind. The empty string means "fall back to
// the install-wide default" (config.ResolveLLMProvider at dispatch time), which
// keeps manifests written before per-agent providers existed loading unchanged.
const (
	KindClaudeCode = "claude-code"
	KindCodex      = "codex"
	KindOpencode   = "opencode"
	KindOpenclaw   = "openclaw"
	// Local OpenAI-compatible HTTP runtimes. All three expose the same
	// /v1/chat/completions API; only their default base URL and model differ.
	// Configure per-kind overrides via Config.ProviderEndpoints or
	// WUPHF_<KIND>_BASE_URL / WUPHF_<KIND>_MODEL.
	KindMLXLM  = "mlx-lm"
	KindOllama = "ollama"
	KindExo    = "exo"
)

// ProviderBinding is the per-agent runtime selection persisted on an office
// member and on company.MemberSpec. It captures which LLM runtime executes the
// agent's turns (Kind) and which model the runtime should use (Model, free
// form — validated by the runtime itself, not here).
//
// Openclaw is a pointer so agents on other runtimes don't emit an empty object
// into their JSON; it's populated only when Kind == KindOpenclaw.
type ProviderBinding struct {
	Kind     string                   `json:"kind,omitempty"`
	Model    string                   `json:"model,omitempty"`
	Openclaw *OpenclawProviderBinding `json:"openclaw,omitempty"`
}

// OpenclawProviderBinding holds OpenClaw-specific parameters. SessionKey is
// the gateway's identifier for the agent's persistent conversation. AgentID
// is the OpenClaw agent config name (defaults to "main" at creation time).
type OpenclawProviderBinding struct {
	SessionKey string `json:"session_key,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
}

// ValidateKind reports whether s is an acceptable ProviderBinding.Kind value.
// The empty string is valid and means "use install-wide default."
func ValidateKind(s string) error {
	switch s {
	case "",
		KindClaudeCode, KindCodex, KindOpencode, KindOpenclaw,
		KindMLXLM, KindOllama, KindExo:
		return nil
	default:
		return fmt.Errorf("unknown provider kind %q (valid: %s, %s, %s, %s, %s, %s, %s, or empty)",
			s, KindClaudeCode, KindCodex, KindOpencode, KindOpenclaw, KindMLXLM, KindOllama, KindExo)
	}
}

// ResolveKind returns the effective runtime kind for a binding. If the
// binding's Kind is empty, it falls back to global() — the caller provides
// this function so this package stays decoupled from config loading.
func ResolveKind(b ProviderBinding, global func() string) string {
	if b.Kind != "" {
		return b.Kind
	}
	if global == nil {
		return ""
	}
	return global()
}
