package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
)

var (
	codexLookPath = exec.LookPath
	codexCommand  = exec.Command
	codexGetwd    = os.Getwd
)

func init() {
	Register(&Entry{
		Kind:     KindCodex,
		StreamFn: CreateCodexCLIStreamFn,
		OneShot:  RunCodexOneShot,
		Capabilities: Capabilities{
			PaneEligible:    false,
			SupportsOneShot: true,
		},
	})
}

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

			startedAt := time.Now()
			var firstEventAt time.Time
			var firstTextAt time.Time
			var firstToolAt time.Time
			text, err := runCodexOnce(systemPrompt, prompt, cwd, func(event CodexStreamEvent) {
				if firstEventAt.IsZero() {
					firstEventAt = time.Now()
				}
				switch event.Type {
				case "text":
					if strings.TrimSpace(event.Text) == "" {
						return
					}
					if firstTextAt.IsZero() {
						firstTextAt = time.Now()
					}
					ch <- agent.StreamChunk{Type: "text", Content: event.Text}
				case "tool_use":
					if firstToolAt.IsZero() {
						firstToolAt = time.Now()
					}
					ch <- agent.StreamChunk{
						Type:      "tool_use",
						ToolName:  event.ToolName,
						ToolUseID: event.ToolUseID,
						ToolInput: event.ToolInput,
					}
				case "tool_result":
					ch <- agent.StreamChunk{
						Type:      "tool_result",
						ToolUseID: event.ToolUseID,
						Content:   event.Text,
					}
				}
			})
			if err != nil {
				appendCodexLatencyLog(agentSlug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
					time.Since(startedAt).Milliseconds(),
					durationMillis(startedAt, firstEventAt),
					durationMillis(startedAt, firstTextAt),
					durationMillis(startedAt, firstToolAt),
					err.Error(),
				))
				ch <- agent.StreamChunk{Type: "error", Content: describeCodexFailure(err)}
				return
			}
			appendCodexLatencyLog(agentSlug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d",
				time.Since(startedAt).Milliseconds(),
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				durationMillis(startedAt, firstToolAt),
				len(text),
			))
			if firstTextAt.IsZero() && strings.TrimSpace(text) != "" {
				streamTextChunks(ch, text)
			}
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
	return runCodexOnce(systemPrompt, prompt, cwd, nil)
}

func runCodexOnce(systemPrompt, prompt, cwd string, onEvent func(CodexStreamEvent)) (string, error) {
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

	result, parseErr := ReadCodexJSONStream(stdout, onEvent)
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

func appendCodexLatencyLog(agentSlug string, line string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".wuphf", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(logDir, "codex-latency.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(agentSlug), strings.TrimSpace(line))
}

func durationMillis(start, mark time.Time) int64 {
	if start.IsZero() || mark.IsZero() {
		return -1
	}
	return mark.Sub(start).Milliseconds()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
