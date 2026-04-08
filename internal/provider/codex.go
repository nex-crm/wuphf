package provider

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
)

var (
	codexLookPath = exec.LookPath
	codexCommand  = exec.Command
	codexGetwd    = os.Getwd
)

// CreateCodexCLIStreamFn returns a StreamFn that runs Codex CLI non-interactively.
// WUPHF keeps the conversation history, so each invocation is intentionally ephemeral.
func CreateCodexCLIStreamFn(agentSlug string) agent.StreamFn {
	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk, 64)
		go func() {
			defer close(ch)

			if _, err := codexLookPath("codex"); err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: "Codex CLI not found. Run `codex login` or use /provider to choose a different provider."}
				return
			}

			cwd, err := codexGetwd()
			if err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: fmt.Sprintf("resolve working directory: %v", err)}
				return
			}

			systemPrompt, prompt := buildClaudePrompts(msgs)
			if prompt == "" {
				prompt = "Proceed with the task."
			}

			text, err := runCodexOnce(systemPrompt, prompt, cwd)
			if err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: describeCodexFailure(err)}
				return
			}
			streamTextChunks(ch, text)
		}()
		return ch
	}
}

// RunCodexOneShot runs Codex once with the given system prompt and user prompt
// and returns the final plain-text result.
func RunCodexOneShot(systemPrompt, prompt, cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = codexGetwd()
		if err != nil {
			return "", err
		}
	}
	return runCodexOnce(systemPrompt, prompt, cwd)
}

func runCodexOnce(systemPrompt, prompt, cwd string) (string, error) {
	outputFile, err := os.CreateTemp("", "wuphf-codex-*.txt")
	if err != nil {
		return "", fmt.Errorf("create codex output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	args := buildCodexArgs(cwd, outputPath)
	cmd := codexCommand("codex", args...)
	cmd.Dir = cwd
	cmd.Env = filteredEnv(nil)
	cmd.Stdin = strings.NewReader(buildCodexPrompt(systemPrompt, prompt))

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if content, readErr := os.ReadFile(outputPath); readErr == nil {
			if text := strings.TrimSpace(string(content)); text != "" {
				return text, nil
			}
		}
		if stderrText := strings.TrimSpace(stderr.String()); stderrText != "" {
			return "", fmt.Errorf("%w: %s", err, stderrText)
		}
		return "", err
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read codex output: %w", err)
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return "", fmt.Errorf("codex returned no final text")
	}
	return text, nil
}

func buildCodexArgs(cwd string, outputPath string) []string {
	return []string{
		"exec",
		"-C", cwd,
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--output-last-message", outputPath,
		"-",
	}
}

func buildCodexPrompt(systemPrompt, prompt string) string {
	var parts []string
	if strings.TrimSpace(systemPrompt) != "" {
		parts = append(parts, "<system>\n"+strings.TrimSpace(systemPrompt)+"\n</system>")
	}
	if strings.TrimSpace(prompt) != "" {
		parts = append(parts, strings.TrimSpace(prompt))
	}
	return strings.Join(parts, "\n\n")
}

func describeCodexFailure(err error) string {
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(text, "login") || strings.Contains(text, "auth") || strings.Contains(text, "unauthorized") {
		return "Codex CLI requires login. Run `codex login` or use /provider to choose a different provider."
	}
	return fmt.Sprintf("codex exited with error: %v", err)
}
