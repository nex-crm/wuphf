package provider

import (
	"github.com/nex-crm/wuphf/internal/config"
)

// RunConfiguredOneShot runs a single-shot generation using the configured LLM provider.
// Providers without a dedicated one-shot path fall back to Claude for now.
func RunConfiguredOneShot(systemPrompt, prompt, cwd string) (string, error) {
	switch config.ResolveLLMProvider("") {
	case "codex":
		return RunCodexOneShot(systemPrompt, prompt, cwd)
	default:
		return RunClaudeOneShot(systemPrompt, prompt, cwd)
	}
}
