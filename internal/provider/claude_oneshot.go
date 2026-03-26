package provider

import (
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
)

// RunClaudeOneShot runs Claude once with the given system prompt and user prompt
// and returns the final plain-text result.
func RunClaudeOneShot(systemPrompt, prompt, cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	ch := make(chan agent.StreamChunk, 128)
	result := runClaudeAttempt(ch, prompt, systemPrompt, cwd, "")
	close(ch)
	if result.exitErr != nil {
		return "", result.exitErr
	}
	text := strings.TrimSpace(result.resultText)
	if text == "" {
		var parts []string
		for chunk := range ch {
			if chunk.Type == "text" && strings.TrimSpace(chunk.Content) != "" {
				parts = append(parts, chunk.Content)
			}
		}
		text = strings.TrimSpace(strings.Join(parts, ""))
	}
	return text, nil
}
