package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// claudeEnvVarsToStrip are the env vars injected by Claude Code that must be
// removed so the child `claude` process does not detect a recursive invocation.
var claudeEnvVarsToStrip = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_SESSION",
	"CLAUDE_CODE_PARENT_SESSION",
}

var (
	claudeLookPath         = exec.LookPath
	claudeCommand          = exec.Command
	claudeGetwd            = os.Getwd
	claudeConfigureProcess = configureClaudeProcess
)

// claudeStreamMsg is the NDJSON envelope emitted by `claude --output-format stream-json`.
type claudeStreamMsg struct {
	Type          string            `json:"type"`
	Subtype       string            `json:"subtype,omitempty"`
	SessionID     string            `json:"session_id,omitempty"`
	Model         string            `json:"model,omitempty"`
	Message       *claudeMessage    `json:"message"`
	Result        string            `json:"result,omitempty"`
	Errors        []json.RawMessage `json:"errors,omitempty"`
	ToolUseResult *struct {
		Stdout string `json:"stdout,omitempty"`
		Stderr string `json:"stderr,omitempty"`
	} `json:"tool_use_result,omitempty"`
}

type claudeMessage struct {
	Content []claudeContentBlock `json:"content"`
}

type claudeContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Input    any    `json:"input,omitempty"`
	Content  any    `json:"content,omitempty"` // for tool_result
}

type claudeAttemptResult struct {
	sessionID      string
	model          string
	exitErr        error
	stderr         string
	resultText     string
	errorMessages  []string
	unknownSession bool
	loginRequired  bool
}

func init() {
	Register(&Entry{
		Kind:     KindClaudeCode,
		StreamFn: CreateClaudeCodeStreamFn,
		OneShot:  RunClaudeOneShot,
		Capabilities: Capabilities{
			PaneEligible:               true,
			SupportsOneShot:            true,
			RequiresClaudeSessionReset: true,
		},
	})
}

// CreateClaudeCodeStreamFn returns a StreamFn that runs the `claude` CLI and
// parses its NDJSON stream output.
func CreateClaudeCodeStreamFn(agentSlug string) agent.StreamFn {
	sessionStore := getClaudeSessionStore()

	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk, 64)
		go func() {
			defer close(ch)

			if _, err := claudeLookPath("claude"); err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: "Claude CLI not found. Run /init to choose a different provider."}
				return
			}

			cwd, err := claudeGetwd()
			if err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: fmt.Sprintf("resolve working directory: %v", err)}
				return
			}

			systemPrompt, prompt := buildClaudePrompts(msgs)
			if prompt == "" {
				prompt = "Proceed with the task."
			}

			resumeID := sessionStore.resumeSessionID(agentSlug, cwd)
			attempt := runClaudeAttempt(ch, prompt, systemPrompt, cwd, resumeID)
			if attempt.sessionID != "" {
				sessionStore.save(agentSlug, attempt.sessionID, cwd)
			}
			if attempt.exitErr == nil {
				return
			}

			if resumeID != "" && attempt.unknownSession {
				sessionStore.clear(agentSlug)
				ch <- agent.StreamChunk{
					Type:    "thinking",
					Content: fmt.Sprintf("%s session expired; retrying with a fresh Claude session.", agentSlug),
				}
				retry := runClaudeAttempt(ch, prompt, systemPrompt, cwd, "")
				if retry.sessionID != "" {
					sessionStore.save(agentSlug, retry.sessionID, cwd)
				}
				if retry.exitErr == nil {
					return
				}
				ch <- agent.StreamChunk{Type: "error", Content: describeClaudeAttemptFailure(retry)}
				return
			}

			ch <- agent.StreamChunk{Type: "error", Content: describeClaudeAttemptFailure(attempt)}
		}()
		return ch
	}
}

func runClaudeAttempt(ch chan<- agent.StreamChunk, prompt string, systemPrompt string, cwd string, resumeID string) claudeAttemptResult {
	args := buildClaudeArgs(systemPrompt, resumeID)
	cmd := claudeCommand("claude", args...)
	cmd.Dir = cwd
	cmd.Env = filteredEnv(claudeEnvVarsToStrip)
	cmd.Stdin = strings.NewReader(prompt)
	claudeConfigureProcess(cmd)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return claudeAttemptResult{exitErr: fmt.Errorf("pipe: %w", err)}
	}
	if err := cmd.Start(); err != nil {
		return claudeAttemptResult{exitErr: fmt.Errorf("start claude: %w", err)}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	result := claudeAttemptResult{}
	gotAssistantText := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg claudeStreamMsg
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.SessionID != "" {
			result.sessionID = msg.SessionID
		}
		if msg.Model != "" {
			result.model = msg.Model
		}

		switch msg.Type {
		case "assistant":
			if msg.Message == nil {
				continue
			}
			for _, block := range msg.Message.Content {
				switch block.Type {
				case "thinking":
					if block.Thinking != "" {
						ch <- agent.StreamChunk{Type: "thinking", Content: block.Thinking}
					}
				case "text":
					if block.Text != "" {
						streamTextChunks(ch, block.Text)
						gotAssistantText = true
					}
				case "tool_use":
					inputJSON, _ := json.Marshal(block.Input)
					ch <- agent.StreamChunk{
						Type:      "tool_use",
						ToolName:  block.Name,
						ToolUseID: block.ID,
						ToolInput: string(inputJSON),
					}
				}
			}
		case "user":
			if msg.Message != nil {
				for _, block := range msg.Message.Content {
					if block.Type != "tool_result" {
						continue
					}
					resultStr := formatClaudeToolResult(block.Content)
					ch <- agent.StreamChunk{
						Type:      "tool_result",
						ToolUseID: block.ID,
						Content:   resultStr,
					}
				}
			}
			if msg.ToolUseResult != nil && msg.ToolUseResult.Stdout != "" {
				ch <- agent.StreamChunk{Type: "tool_result", Content: truncateClaudeOutput(msg.ToolUseResult.Stdout)}
			}
		case "result":
			if msg.Result != "" {
				result.resultText = msg.Result
				if !gotAssistantText && msg.Subtype != "error" {
					streamTextChunks(ch, msg.Result)
				}
			}
			result.errorMessages = append(result.errorMessages, parseClaudeErrors(msg.Errors)...)
		}
	}

	if err := scanner.Err(); err != nil {
		result.exitErr = fmt.Errorf("scan: %w", err)
		result.stderr = strings.TrimSpace(stderrBuf.String())
		return result
	}

	result.exitErr = cmd.Wait()
	result.stderr = strings.TrimSpace(stderrBuf.String())
	result.loginRequired = isClaudeLoginRequired(result)
	result.unknownSession = isClaudeUnknownSessionFailure(result)
	return result
}

func buildClaudeArgs(systemPrompt string, resumeID string) []string {
	args := []string{
		"--print", "-",
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", "20",
		"--disable-slash-commands",
		"--strict-mcp-config",
		"--setting-sources", "user",
	}
	if shouldUseClaudeBareMode() {
		args = append(args, "--bare")
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}
	return args
}

func shouldUseClaudeBareMode() bool {
	return strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != ""
}

func formatClaudeToolResult(value any) string {
	resultStr := ""
	switch v := value.(type) {
	case string:
		resultStr = v
	default:
		b, _ := json.Marshal(v)
		resultStr = string(b)
	}
	return truncateClaudeOutput(resultStr)
}

func truncateClaudeOutput(value string) string {
	if len(value) > 500 {
		return value[:500] + "..."
	}
	return value
}

func parseClaudeErrors(rawErrors []json.RawMessage) []string {
	messages := make([]string, 0, len(rawErrors))
	for _, raw := range rawErrors {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			text = strings.TrimSpace(text)
			if text != "" {
				messages = append(messages, text)
			}
			continue
		}

		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		for _, key := range []string{"message", "error", "code"} {
			value, ok := obj[key].(string)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			messages = append(messages, value)
			break
		}
	}
	return messages
}

func isClaudeUnknownSessionFailure(result claudeAttemptResult) bool {
	messages := append([]string{result.resultText}, result.errorMessages...)
	for _, message := range messages {
		text := strings.ToLower(strings.TrimSpace(message))
		if strings.Contains(text, "no conversation found with session id") ||
			strings.Contains(text, "unknown session") ||
			(strings.Contains(text, "session") && strings.Contains(text, "not found")) {
			return true
		}
	}
	return false
}

func isClaudeLoginRequired(result claudeAttemptResult) bool {
	messages := append([]string{result.resultText}, result.errorMessages...)
	if result.stderr != "" {
		messages = append(messages, result.stderr)
	}
	text := strings.ToLower(strings.Join(messages, "\n"))
	return strings.Contains(text, "not logged in") ||
		strings.Contains(text, "please log in") ||
		strings.Contains(text, "please run `claude login`") ||
		strings.Contains(text, "please run claude login") ||
		strings.Contains(text, "login required") ||
		strings.Contains(text, "requires login") ||
		strings.Contains(text, "unauthorized") ||
		strings.Contains(text, "authentication required")
}

func describeClaudeAttemptFailure(result claudeAttemptResult) string {
	if result.loginRequired {
		return "Claude CLI requires login. Run `claude login` or use /init to choose a different provider."
	}
	if result.stderr != "" {
		return fmt.Sprintf("claude exited with error: %v — %s", result.exitErr, result.stderr)
	}
	if len(result.errorMessages) > 0 {
		return fmt.Sprintf("claude exited with error: %v — %s", result.exitErr, result.errorMessages[0])
	}
	if result.resultText != "" {
		return fmt.Sprintf("claude exited with error: %v — %s", result.exitErr, result.resultText)
	}
	return fmt.Sprintf("claude exited with error: %v", result.exitErr)
}

// filteredEnv returns os.Environ() with the given keys removed.
func filteredEnv(strip []string) []string {
	stripSet := make(map[string]struct{}, len(strip))
	for _, k := range strip {
		stripSet[k] = struct{}{}
	}
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if _, skip := stripSet[key]; !skip {
			out = append(out, kv)
		}
	}
	return out
}

// buildClaudePrompts splits conversation history into a Claude system prompt and
// a printable conversation transcript for stdin-driven `claude --print -`.
func buildClaudePrompts(msgs []agent.Message) (systemPrompt string, prompt string) {
	var systemParts []string
	var sb strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			systemParts = append(systemParts, m.Content)
			continue
		}
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return strings.Join(systemParts, "\n\n"), strings.TrimRight(sb.String(), "\n")
}

func streamTextChunks(ch chan<- agent.StreamChunk, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	if len(text) <= 40 {
		ch <- agent.StreamChunk{Type: "text", Content: text}
		return
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		ch <- agent.StreamChunk{Type: "text", Content: text}
		return
	}

	const wordsPerChunk = 5
	for i := 0; i < len(words); i += wordsPerChunk {
		end := i + wordsPerChunk
		if end > len(words) {
			end = len(words)
		}
		ch <- agent.StreamChunk{Type: "text", Content: strings.Join(words[i:end], " ")}
		if end < len(words) {
			time.Sleep(40 * time.Millisecond)
		}
	}
}
