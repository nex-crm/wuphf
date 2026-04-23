package provider

import (
	"github.com/nex-crm/wuphf/internal/config"
)

// RunConfiguredOneShot runs a single-shot generation using the active LLM
// provider's OneShot implementation. Providers without a one-shot path
// (Capabilities.SupportsOneShot == false) and unregistered kinds fall back
// to Claude.
func RunConfiguredOneShot(systemPrompt, prompt, cwd string) (string, error) {
	kind := config.ResolveLLMProvider("")
	if e := Lookup(kind); e != nil && e.Capabilities.SupportsOneShot && e.OneShot != nil {
		return e.OneShot(systemPrompt, prompt, cwd)
	}
	return RunClaudeOneShot(systemPrompt, prompt, cwd)
}
