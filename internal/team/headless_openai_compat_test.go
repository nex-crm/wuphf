package team

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/provider"
)

// TestIsOpenAICompatKind documents the closed set of provider Kinds that the
// dispatcher routes to runHeadlessOpenAICompatTurn. New OpenAI-compatible
// runtimes added under internal/provider/ MUST also be added here so the
// broker-driven turn queue actually calls them — otherwise the dispatcher
// silently falls through to runHeadlessClaudeTurn and the user wonders why
// `wuphf --provider mlx-lm` keeps invoking claude.
func TestIsOpenAICompatKind(t *testing.T) {
	for _, kind := range []string{
		provider.KindMLXLM,
		provider.KindOllama,
		provider.KindExo,
	} {
		if !isOpenAICompatKind(kind) {
			t.Errorf("isOpenAICompatKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{
		provider.KindClaudeCode,
		provider.KindCodex,
		provider.KindOpencode,
		provider.KindOpenclaw,
		"",
		"unknown",
	} {
		if isOpenAICompatKind(kind) {
			t.Errorf("isOpenAICompatKind(%q) = true, want false", kind)
		}
	}
}
