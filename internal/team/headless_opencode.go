package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/runtimebin"
)

// Opencode-specific test hooks. Kept separate from the codex hooks so test
// setups can stub one runtime without colliding with the other.
var (
	headlessOpencodeLookPath       = runtimebin.LookPath
	headlessOpencodeCommandContext = exec.CommandContext
	headlessOpencodeExecutablePath = os.Executable
)

// headlessOpencodeSecretEnvVars lists WUPHF-managed secrets that must NOT flow
// into the outer opencode process. Opencode is a third-party binary that
// routes to user-configured LLM backends (OpenAI, Ollama, any OpenAI-
// compatible endpoint) and can load plugins; leaking these broader-than-Codex
// tokens into that process is a credential-exfiltration surface we would not
// trust. These secrets are still available to the WUPHF MCP subprocess via
// opencode.json's per-server `environment` block, where they are scoped to the
// wuphf-office MCP server and never reach the model backend.
var headlessOpencodeSecretEnvVars = []string{
	"WUPHF_BROKER_TOKEN",
	"WUPHF_API_KEY",
	"WUPHF_OPENAI_API_KEY",
	"NEX_API_KEY",
	"ONE_SECRET",
}

// runHeadlessOpencodeTurn executes a single Opencode turn for slug, posting the
// final text to channel (if any) via the same broker/progress machinery used
// by the Codex runtime. Opencode emits plain text on stdout rather than
// structured JSONL, so this path is a thinner version of runHeadlessCodexTurn
// with no tool-event parsing and no Codex-specific auth/config layering.
func (l *Launcher) runHeadlessOpencodeTurn(ctx context.Context, slug string, notification string, channel ...string) error {
	if _, err := headlessOpencodeLookPath("opencode"); err != nil {
		return fmt.Errorf("opencode not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}

	workspaceDir := strings.TrimSpace(l.cwd)
	if worktreeDir := l.headlessTaskWorkspaceDir(slug); worktreeDir != "" {
		workspaceDir = worktreeDir
	}
	workspaceDir = normalizeHeadlessWorkspaceDir(workspaceDir)
	if workspaceDir == "" {
		workspaceDir = "."
	}

	promptText := buildHeadlessOpencodePrompt(l.buildPrompt(slug), notification)
	args := buildHeadlessOpencodeArgs(config.ResolveOpencodeModel(), promptText)
	cmd := headlessOpencodeCommandContext(ctx, "opencode", args...)
	cmd.Dir = workspaceDir

	// Start from the Codex env builder (broker/workspace/identity plumbing),
	// then apply the Opencode-specific fixups: restore the user's real HOME so
	// opencode finds ~/.local/share/opencode/auth.json, strip secrets that
	// should never reach the third-party opencode process, overlay WUPHF's MCP
	// config so agents can claim tasks / post status / update wiki, and flip
	// the provider tag + NO_COLOR.
	env := l.buildHeadlessCodexEnv(slug, workspaceDir, firstNonEmpty(channel...))
	env = setEnvValue(env, "WUPHF_HEADLESS_PROVIDER", "opencode")
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		env = setEnvValue(env, "HOME", home)
	}
	env = stripEnvKeys(env, []string{"CODEX_HOME"})
	env = stripEnvKeys(env, headlessOpencodeSecretEnvVars)
	env = setEnvValue(env, "NO_COLOR", "1")
	if workspaceDir != strings.TrimSpace(l.cwd) {
		env = append(env, "WUPHF_WORKTREE_PATH="+workspaceDir)
	}
	opencodeConfigPath, err := l.writeHeadlessOpencodeMCPConfig(slug)
	if err != nil {
		// MCP failure is loud but non-fatal — opencode will still run, just
		// without the wuphf-office tools. Log so the user can debug.
		appendHeadlessCodexLog(slug, "opencode_mcp-config-failed: "+err.Error())
	} else {
		env = setEnvValue(env, "OPENCODE_CONFIG", opencodeConfigPath)
	}
	cmd.Env = env

	configureHeadlessProcess(cmd)
	dumpHeadlessCodexInvocation(slug, workspaceDir, args, cmd.Env, promptText)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach opencode stdout: %w", err)
	}

	var agentStream *agentStreamBuffer
	taskID := l.agentActiveTaskID(slug)
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			terminateHeadlessProcess(cmd)
			_ = stdout.Close()
		case <-done:
		}
	}()

	startedAt := time.Now()
	metrics := headlessProgressMetrics{
		TotalMs:      -1,
		FirstEventMs: -1,
		FirstTextMs:  -1,
		FirstToolMs:  -1,
	}
	l.updateHeadlessProgress(slug, "active", "thinking", "reviewing work packet", metrics)

	// Live-chat relay surfaces the model's user-facing text to the
	// channel at sentence/paragraph boundaries during the turn. Opencode
	// emits one `text` event type for the assistant's spoken output;
	// piping it through the relay is what turns the agent's reply from
	// a single end-of-turn post into a visible live conversation.
	target := firstNonEmpty(channel...)
	relay := newHeadlessLiveChatRelay(l, slug, target, notification, func(line string) {
		appendHeadlessCodexLog(slug, line)
	})
	// Defer the flush so error/scanErr exit paths still surface the
	// trailing buffered sentence. The explicit Flush before the final
	// post stays — once the buffer is empty, the deferred call is a
	// no-op.
	defer relay.Flush()

	var firstEventAt, firstTextAt, firstToolAt time.Time
	textStarted := false
	var lastError string
	turnID := newHeadlessTurnID()
	pushStream := func(line string) {
		if agentStream != nil && strings.TrimSpace(line) != "" {
			agentStream.PushTask(taskID, line)
		}
	}

	streamRes, scanErr := provider.ReadOpencodeJSONStream(stdout, func(ev provider.OpencodeStreamEvent) {
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			metrics.FirstEventMs = durationMillis(startedAt, firstEventAt)
		}
		switch ev.Type {
		case "text":
			if strings.TrimSpace(ev.Text) == "" {
				return
			}
			if firstTextAt.IsZero() {
				firstTextAt = time.Now()
				metrics.FirstTextMs = durationMillis(startedAt, firstTextAt)
			}
			if !textStarted {
				textStarted = true
				l.updateHeadlessProgress(slug, "active", "text", "drafting response", metrics)
			}
			pushStream(ev.Text)
			relay.OnText(ev.Text)
			emitHeadlessText(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, ev.Text, "opencode.text")
		case "tool_use":
			relay.Flush()
			if firstToolAt.IsZero() {
				firstToolAt = time.Now()
				metrics.FirstToolMs = durationMillis(startedAt, firstToolAt)
			}
			detail := strings.TrimSpace(ev.ToolName)
			if detail == "" {
				detail = "tool"
			}
			l.updateHeadlessProgress(slug, "active", "tool", "running "+detail, metrics)
			pushStream("[tool] " + detail)
			emitHeadlessToolUse(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, ev.ToolName, "", "opencode.tool_use")
		case "tool_result":
			if d := strings.TrimSpace(ev.Detail); d != "" {
				pushStream("[tool_result] " + truncate(d, 240))
				emitHeadlessToolResult(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, ev.ToolName, d, "opencode.tool_result")
			}
		case "error":
			if msg := strings.TrimSpace(ev.Detail); msg != "" {
				lastError = msg
				pushStream("[error] " + msg)
			}
		}
	})
	// provider.ReadOpencodeJSONStream now uses a reader-based drain, so a
	// single oversized output line cannot wedge cmd.Wait on backpressure.
	// The previous SIGKILL fallback for bufio.ErrTooLong is therefore gone.

	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		if detail != "" {
			appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error provider=opencode total_ms=%d first_event_ms=%d first_text_ms=%d detail=%q",
				metrics.TotalMs,
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				detail,
			))
			appendHeadlessCodexLog(slug, "opencode_stderr: "+detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), metrics)
			emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, "", detail, metrics, nil)
			if isOpencodeAuthError(detail) && l.broker != nil {
				sysTarget := target
				if strings.TrimSpace(sysTarget) == "" {
					sysTarget = "general"
				}
				l.broker.PostSystemMessage(sysTarget,
					fmt.Sprintf("@%s hit an auth error talking to the model (%s). Configure your Opencode provider credentials and retry.", slug, truncate(detail, 180)),
					"error",
				)
			}
			return fmt.Errorf("%w: %s", err, detail)
		}
		appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error provider=opencode total_ms=%d first_event_ms=%d first_text_ms=%d detail=%q",
			metrics.TotalMs,
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			err.Error(),
		))
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, "", err.Error(), metrics, nil)
		return err
	}
	if scanErr != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		l.updateHeadlessProgress(slug, "error", "error", truncate(scanErr.Error(), 180), metrics)
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, "", scanErr.Error(), metrics, nil)
		return scanErr
	}

	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	text := strings.TrimSpace(streamRes.FinalMessage)
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok provider=opencode total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d last_error=%q",
		metrics.TotalMs,
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		durationMillis(startedAt, firstToolAt),
		len(text),
		strings.TrimSpace(lastError),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderOpencode, slug, taskID, summary, "", metrics, nil)
	relay.Flush()
	if text != "" {
		appendHeadlessCodexLog(slug, "opencode_result: "+text)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt)
		if err != nil {
			appendHeadlessCodexLog(slug, "opencode_fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessCodexLog(slug, fmt.Sprintf("opencode_fallback-post: posted final output to #%s as %s", msg.Channel, msg.ID))
		}
	}
	return nil
}

// buildHeadlessOpencodeArgs mirrors provider.buildOpencodeArgs but is kept
// local so the team package doesn't need to import the provider package just
// for argv construction (and stays consistent with how headless_codex.go
// builds its own argv). Opencode's CLI shape: `opencode run [--format X]
// [--model X] [message..]` — no --cwd, no --quiet, no stdin sentinel.
// Working directory is set via cmd.Dir by the caller.
//
// `--format json` opts the headless path into opencode's JSON event stream so
// wuphf can see tool-use / tool-result / error events that opencode otherwise
// renders as TUI-styled stdout (or routes to stderr) and the previous
// bufio.Scanner-based consumer was blind to. See #313 (Finding 1).
func buildHeadlessOpencodeArgs(model string, prompt string) []string {
	args := []string{"run", "--format", "json"}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	if strings.TrimSpace(prompt) != "" {
		args = append(args, prompt)
	}
	return args
}

// buildHeadlessOpencodePrompt concatenates system and user text into a single
// positional argument, escaping literal <system>/</system> tokens inside user
// content so the wrapper cannot be closed from within.
func buildHeadlessOpencodePrompt(systemPrompt string, prompt string) string {
	var parts []string
	if s := strings.TrimSpace(systemPrompt); s != "" {
		parts = append(parts, "<system>\n"+escapeHeadlessOpencodeSystemWrapper(s)+"\n</system>")
	}
	if p := strings.TrimSpace(prompt); p != "" {
		parts = append(parts, escapeHeadlessOpencodeSystemWrapper(p))
	}
	return strings.Join(parts, "\n\n")
}

// escapeHeadlessOpencodeSystemWrapper inserts a zero-width space into literal
// <system>/</system> tokens inside user-provided content so the wrapper the
// prompt builder adds cannot be terminated or reopened from within.
func escapeHeadlessOpencodeSystemWrapper(s string) string {
	s = strings.ReplaceAll(s, "</system>", "</\u200bsystem>")
	s = strings.ReplaceAll(s, "<system>", "<\u200bsystem>")
	return s
}

// isOpencodeAuthError checks stderr detail for the shapes Opencode tends to
// emit when credentials are missing or invalid. Conservative — prefer false
// positives (don't nag) over nagging on every failure.
func isOpencodeAuthError(detail string) bool {
	d := strings.ToLower(strings.TrimSpace(detail))
	if d == "" {
		return false
	}
	return strings.Contains(d, "unauthorized") ||
		strings.Contains(d, "api key") ||
		strings.Contains(d, "authentication") ||
		strings.Contains(d, "invalid token") ||
		strings.Contains(d, "no api key")
}
