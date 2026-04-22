package team

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// Opencode-specific test hooks. Kept separate from the codex hooks so test
// setups can stub one runtime without colliding with the other.
var (
	headlessOpencodeLookPath       = exec.LookPath
	headlessOpencodeCommandContext = exec.CommandContext
)

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

	args := buildHeadlessOpencodeArgs(workspaceDir, config.ResolveOpencodeModel(l.cwd))
	cmd := headlessOpencodeCommandContext(ctx, "opencode", args...)
	cmd.Dir = workspaceDir

	// Reuse the Codex env builder — it's provider-neutral (broker token,
	// workspace, identity, API keys) — and override only the provider tag so
	// downstream tooling can distinguish runs.
	env := l.buildHeadlessCodexEnv(slug, workspaceDir, firstNonEmpty(channel...))
	env = setEnvValue(env, "WUPHF_HEADLESS_PROVIDER", "opencode")
	if workspaceDir != strings.TrimSpace(l.cwd) {
		env = append(env, "WUPHF_WORKTREE_PATH="+workspaceDir)
	}
	cmd.Env = env

	stdinText := buildHeadlessCodexPrompt(l.buildPrompt(slug), notification)
	cmd.Stdin = strings.NewReader(stdinText)
	configureHeadlessProcess(cmd)
	dumpHeadlessCodexInvocation(slug, workspaceDir, args, cmd.Env, stdinText)

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
// builds its own argv).
func buildHeadlessOpencodeArgs(workspaceDir string, model string) []string {
	args := []string{"run"}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	if strings.TrimSpace(workspaceDir) != "" {
		args = append(args, "--cwd", strings.TrimSpace(workspaceDir))
	}
	args = append(args, "--quiet", "-")
	return args
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
