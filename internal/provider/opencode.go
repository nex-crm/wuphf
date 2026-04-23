package provider

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/runtimebin"
)

var (
	opencodeLookPath = runtimebin.LookPath
	opencodeCommand  = exec.Command
	opencodeGetwd    = os.Getwd
)

// CreateOpencodeCLIStreamFn returns a StreamFn that runs the Opencode CLI
// non-interactively. Each invocation is ephemeral: WUPHF owns the conversation
// history and hands Opencode a fresh prompt every turn.
//
// Opencode emits plain text on stdout (no JSONL surface), so we stream stdout
// line-by-line as text chunks rather than parsing structured events.
func CreateOpencodeCLIStreamFn(agentSlug string) agent.StreamFn {
	return func(msgs []agent.Message, tools []agent.AgentTool) <-chan agent.StreamChunk {
		ch := make(chan agent.StreamChunk, 64)
		go func() {
			defer close(ch)

			if _, err := opencodeLookPath("opencode"); err != nil {
				ch <- agent.StreamChunk{Type: "error", Content: "Opencode CLI not found. Install opencode or use /provider to choose a different provider."}
				return
			}

			cwd, err := opencodeGetwd()
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
			text, err := runOpencodeOnce(systemPrompt, prompt, cwd, func(line string) {
				if firstEventAt.IsZero() {
					firstEventAt = time.Now()
				}
				if strings.TrimSpace(line) == "" {
					return
				}
				if firstTextAt.IsZero() {
					firstTextAt = time.Now()
				}
				ch <- agent.StreamChunk{Type: "text", Content: line}
			})
			if err != nil {
				appendOpencodeLatencyLog(agentSlug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d detail=%q",
					time.Since(startedAt).Milliseconds(),
					durationMillis(startedAt, firstEventAt),
					durationMillis(startedAt, firstTextAt),
					err.Error(),
				))
				ch <- agent.StreamChunk{Type: "error", Content: describeOpencodeFailure(err)}
				return
			}
			appendOpencodeLatencyLog(agentSlug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d final_chars=%d",
				time.Since(startedAt).Milliseconds(),
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				len(text),
			))
			if firstTextAt.IsZero() && strings.TrimSpace(text) != "" {
				streamTextChunks(ch, text)
			}
		}()
		return ch
	}
}

// RunOpencodeOneShot runs Opencode once with the given system prompt and user
// prompt and returns the final plain-text result.
func RunOpencodeOneShot(systemPrompt, prompt, cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = opencodeGetwd()
		if err != nil {
			return "", err
		}
	}
	return runOpencodeOnce(systemPrompt, prompt, cwd, nil)
}

// runOpencodeOnce invokes `opencode run` with the caller's prompt as the final
// variadic positional argument (Opencode has no stdin-prompt convention),
// streams plain stdout lines via onLine (if provided), and returns the full
// concatenated output.
func runOpencodeOnce(systemPrompt, prompt, cwd string, onLine func(string)) (string, error) {
	promptText := buildOpencodePrompt(systemPrompt, prompt)
	args := buildOpencodeArgs(config.ResolveOpencodeModel(), promptText)
	cmd := opencodeCommand("opencode", args...)
	cmd.Dir = cwd
	// NO_COLOR suppresses ANSI decoration in Opencode's default formatted
	// output so downstream line scanners see clean text.
	cmd.Env = append(filteredEnv(nil), "NO_COLOR=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("attach opencode stdout: %w", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", err
	}

	output, readErr := readOpencodeStream(stdout, onLine)
	if readErr != nil && errors.Is(readErr, bufio.ErrTooLong) {
		// A single >4 MiB line would block cmd.Wait() on pipe backpressure
		// forever; kill the child so Wait returns and the caller sees a
		// clean error.
		_ = cmd.Process.Kill()
	}
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	if readErr != nil {
		return "", readErr
	}
	text := strings.TrimSpace(output)
	if text == "" {
		return "", fmt.Errorf("opencode returned no output")
	}
	return text, nil
}

// readOpencodeStream reads plain-text lines from r, forwarding each line to
// onLine as it arrives, and returns the combined output.
func readOpencodeStream(r io.Reader, onLine func(string)) (string, error) {
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(line)
		if onLine != nil {
			onLine(line)
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return sb.String(), fmt.Errorf("opencode output line exceeded 4 MiB buffer: %w", err)
		}
		return sb.String(), fmt.Errorf("read opencode stream: %w", err)
	}
	return sb.String(), nil
}

// buildOpencodeArgs constructs the argv for a single-shot `opencode run`.
// Opencode's CLI takes the prompt as trailing variadic positional arguments
// (`opencode run [message..]`) rather than via stdin, so we pass the full
// composed prompt as a single argv string. Working directory is set via
// cmd.Dir by the caller — Opencode exposes no `--cwd` flag.
// Model selection is optional; when unset Opencode uses its configured
// default. Expected format: `provider/model` (e.g. "anthropic/claude-sonnet-4").
func buildOpencodeArgs(model string, prompt string) []string {
	args := []string{"run"}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, prompt)
	}
	return args
}

// buildOpencodePrompt concatenates system and user text for delivery as a
// single positional argument to `opencode run`. Any literal <system>/</system>
// tokens inside user content are neutralised with a zero-width space so the
// wrapper the builder adds cannot be closed or re-opened from within — belt
// and suspenders for confused-deputy issues when untrusted text is passed in.
func buildOpencodePrompt(systemPrompt, prompt string) string {
	var parts []string
	if s := strings.TrimSpace(systemPrompt); s != "" {
		parts = append(parts, "<system>\n"+escapeOpencodeSystemWrapper(s)+"\n</system>")
	}
	if p := strings.TrimSpace(prompt); p != "" {
		parts = append(parts, escapeOpencodeSystemWrapper(p))
	}
	return strings.Join(parts, "\n\n")
}

// escapeOpencodeSystemWrapper inserts a zero-width space inside any literal
// <system>/</system> tag so the prompt wrapper buildOpencodePrompt adds cannot
// be terminated from within user content.
func escapeOpencodeSystemWrapper(s string) string {
	s = strings.ReplaceAll(s, "</system>", "</\u200bsystem>")
	s = strings.ReplaceAll(s, "<system>", "<\u200bsystem>")
	return s
}

func describeOpencodeFailure(err error) string {
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(text, "login") || strings.Contains(text, "auth") || strings.Contains(text, "unauthorized") || strings.Contains(text, "api key") {
		return "Opencode CLI is not authenticated. Configure your Opencode provider credentials or use /provider to choose a different provider."
	}
	if strings.Contains(text, "exceeded 4 mib") {
		return "Opencode produced a stream line larger than 4 MiB; aborted. Retry with a smaller response."
	}
	return fmt.Sprintf("opencode exited with error: %v", err)
}

func appendOpencodeLatencyLog(agentSlug string, line string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".wuphf", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(logDir, "opencode-latency.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(agentSlug), strings.TrimSpace(line))
}
