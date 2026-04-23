package team

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
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
	if err := l.writeHeadlessOpencodeMCPConfig(slug); err != nil {
		// MCP failure is loud but non-fatal — opencode will still run, just
		// without the wuphf-office tools. Log so the user can debug.
		appendHeadlessCodexLog(slug, "opencode_mcp-config-failed: "+err.Error())
	}
	cmd.Env = env

	configureHeadlessProcess(cmd)
	dumpHeadlessCodexInvocation(slug, workspaceDir, args, cmd.Env, promptText)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach opencode stdout: %w", err)
	}

	var agentStream *agentStreamBuffer
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}
	pr, pw := io.Pipe()
	teedStdout := io.TeeReader(stdout, pw)
	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if agentStream != nil && line != "" {
				agentStream.Push(line)
			}
		}
	}()

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			terminateHeadlessProcess(cmd)
			_ = stdout.Close()
			_ = pw.CloseWithError(ctx.Err())
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

	var firstEventAt, firstTextAt time.Time
	textStarted := false
	var outputBuf strings.Builder

	scanner := bufio.NewScanner(teedStdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			metrics.FirstEventMs = durationMillis(startedAt, firstEventAt)
		}
		if strings.TrimSpace(line) != "" {
			if firstTextAt.IsZero() {
				firstTextAt = time.Now()
				metrics.FirstTextMs = durationMillis(startedAt, firstTextAt)
			}
			if !textStarted {
				textStarted = true
				l.updateHeadlessProgress(slug, "active", "text", "drafting response", metrics)
			}
		}
		if outputBuf.Len() > 0 {
			outputBuf.WriteByte('\n')
		}
		outputBuf.WriteString(line)
	}
	scanErr := scanner.Err()
	if scanErr != nil && errors.Is(scanErr, bufio.ErrTooLong) {
		// A single >4 MiB line would block cmd.Wait() on pipe backpressure
		// forever; kill the child so Wait returns promptly.
		terminateHeadlessProcess(cmd)
	}
	_ = pw.Close()

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
			if isOpencodeAuthError(detail) && l.broker != nil {
				target := firstNonEmpty(channel...)
				if strings.TrimSpace(target) == "" {
					target = "general"
				}
				l.broker.PostSystemMessage(target,
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
		return err
	}
	if scanErr != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		l.updateHeadlessProgress(slug, "error", "error", truncate(scanErr.Error(), 180), metrics)
		if errors.Is(scanErr, bufio.ErrTooLong) {
			return fmt.Errorf("opencode output line exceeded 4 MiB buffer; aborted")
		}
		return scanErr
	}

	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	text := strings.TrimSpace(outputBuf.String())
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok provider=opencode total_ms=%d first_event_ms=%d first_text_ms=%d final_chars=%d",
		metrics.TotalMs,
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		len(text),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	if text != "" {
		appendHeadlessCodexLog(slug, "opencode_result: "+text)
		target := firstNonEmpty(channel...)
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
// builds its own argv). Opencode's CLI shape: `opencode run [--model X]
// [message..]` — no --cwd, no --quiet, no stdin sentinel. Working directory
// is set via cmd.Dir by the caller.
func buildHeadlessOpencodeArgs(model string, prompt string) []string {
	args := []string{"run"}
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

// writeHeadlessOpencodeMCPConfig merges WUPHF's MCP server definition into the
// user's $HOME/.config/opencode/opencode.json. Preserves any other top-level
// keys (theme, provider preferences, user-configured MCP servers) and only
// touches the wuphf-office entry under `mcp`. Secrets live in the MCP
// subprocess's `environment` block so they never reach the model backend
// opencode routes to.
func (l *Launcher) writeHeadlessOpencodeMCPConfig(slug string) error {
	wuphfBinary, err := headlessOpencodeExecutablePath()
	if err != nil {
		return fmt.Errorf("resolve wuphf binary: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return fmt.Errorf("resolve user home: %w", err)
	}
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("mkdir opencode config dir: %w", err)
	}

	merged := map[string]any{}
	if raw, err := os.ReadFile(configPath); err == nil && len(raw) > 0 {
		// Best-effort: if the existing file isn't valid JSON, overwrite it
		// rather than silently losing the WUPHF overlay.
		_ = json.Unmarshal(raw, &merged)
	}

	mcp, _ := merged["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	mcp["wuphf-office"] = l.buildHeadlessOpencodeMCPEntry(wuphfBinary, slug)
	merged["mcp"] = mcp
	if _, ok := merged["$schema"]; !ok {
		merged["$schema"] = "https://opencode.ai/config.json"
	}

	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal opencode.json: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("write opencode.json: %w", err)
	}
	return nil
}

// buildHeadlessOpencodeMCPEntry constructs the `mcp.wuphf-office` block for
// opencode.json. The WUPHF-managed secrets (broker token, identity, Nex API
// key) live inside the MCP `environment` map — opencode forwards these only
// to the MCP subprocess, not to the model backend. This scoping is the
// security boundary that makes it safe to add a third-party provider like
// opencode, which can route to arbitrary user-configured endpoints.
func (l *Launcher) buildHeadlessOpencodeMCPEntry(wuphfBinary string, slug string) map[string]any {
	entry := map[string]any{
		"type":    "local",
		"command": []string{wuphfBinary, "mcp-team"},
		"enabled": true,
	}
	envMap := map[string]string{
		"WUPHF_AGENT_SLUG":      slug,
		"WUPHF_BROKER_BASE_URL": l.BrokerBaseURL(),
	}
	if l != nil && l.broker != nil {
		envMap["WUPHF_BROKER_TOKEN"] = l.broker.Token()
	}
	if config.ResolveNoNex() {
		envMap["WUPHF_NO_NEX"] = "1"
	}
	if l != nil && l.isOneOnOne() {
		envMap["WUPHF_ONE_ON_ONE"] = "1"
		if v := strings.TrimSpace(l.oneOnOneAgent()); v != "" {
			envMap["WUPHF_ONE_ON_ONE_AGENT"] = v
		}
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		envMap["ONE_SECRET"] = secret
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		envMap["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			envMap["ONE_IDENTITY_TYPE"] = identityType
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
		envMap["WUPHF_API_KEY"] = apiKey
		envMap["NEX_API_KEY"] = apiKey
	}
	entry["environment"] = envMap
	return entry
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
