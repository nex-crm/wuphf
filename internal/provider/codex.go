package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
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
	args := buildCodexArgs(cwd, config.ResolveCodexModel(cwd))
	cmd := codexCommand("codex", args...)
	cmd.Dir = cwd
	cmd.Env = filteredEnv(nil)
	cmd.Stdin = strings.NewReader(buildCodexPrompt(systemPrompt, prompt))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("attach codex stdout: %w", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}

	result, parseErr := readCodexJSONStream(stdout)
	if err := cmd.Wait(); err != nil {
		detail := firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()))
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	if parseErr != nil {
		return "", parseErr
	}
	text := strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine))
	if text == "" {
		return "", fmt.Errorf("codex returned no final text")
	}
	return text, nil
}

func buildCodexArgs(cwd string, model string) []string {
	args := []string{"exec"}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	args = append(args,
		"-C", cwd,
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--json",
		"-",
	)
	return args
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

type codexJSONResult struct {
	FinalMessage  string
	LastPlainLine string
	LastError     string
}

type codexJSONEvent struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
	Error   *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	Item *struct {
		Type    string `json:"type,omitempty"`
		Text    string `json:"text,omitempty"`
		Content []struct {
			Type string `json:"type,omitempty"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
	} `json:"item,omitempty"`
}

func readCodexJSONStream(r io.Reader) (codexJSONResult, error) {
	var result codexJSONResult
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event codexJSONEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			result.LastPlainLine = line
			continue
		}
		if text := strings.TrimSpace(extractCodexAgentMessage(event)); text != "" {
			result.FinalMessage = text
		}
		if detail := strings.TrimSpace(extractCodexError(event)); detail != "" {
			result.LastError = detail
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read codex json stream: %w", err)
	}
	return result, nil
}

func extractCodexAgentMessage(event codexJSONEvent) string {
	if event.Type != "item.completed" || event.Item == nil || event.Item.Type != "agent_message" {
		return ""
	}
	if text := strings.TrimSpace(event.Item.Text); text != "" {
		return text
	}
	parts := make([]string, 0, len(event.Item.Content))
	for _, item := range event.Item.Content {
		if item.Type == "output_text" || item.Type == "text" {
			if text := strings.TrimSpace(item.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func extractCodexError(event codexJSONEvent) string {
	switch event.Type {
	case "error", "turn.failed":
		if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
			return event.Error.Message
		}
		if strings.TrimSpace(event.Message) != "" {
			return event.Message
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
