package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
)

var (
	headlessCodexLookPath       = exec.LookPath
	headlessCodexCommandContext = exec.CommandContext
	headlessCodexExecutablePath = os.Executable
	headlessCodexRunTurn        = func(l *Launcher, ctx context.Context, slug, notification string) error {
		if l != nil && !l.usesCodexRuntime() {
			return l.runHeadlessClaudeTurn(ctx, slug, notification)
		}
		return l.runHeadlessCodexTurn(ctx, slug, notification)
	}
)

var (
	headlessCodexTurnTimeout      = 2 * time.Minute
	headlessCodexStaleCancelAfter = 8 * time.Second
)

type headlessCodexTurn struct {
	Prompt     string
	EnqueuedAt time.Time
}

type headlessCodexActiveTurn struct {
	Turn      headlessCodexTurn
	StartedAt time.Time
	Cancel    context.CancelFunc
}

func (l *Launcher) launchHeadlessCodex() error {
	killStaleBroker()
	exec.Command("tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	l.broker = NewBroker()
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())

	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

func (l *Launcher) enqueueHeadlessCodexTurn(slug string, prompt string) {
	slug = strings.TrimSpace(slug)
	prompt = strings.TrimSpace(prompt)
	if slug == "" || prompt == "" {
		return
	}

	var cancel context.CancelFunc
	var staleAge time.Duration
	startWorker := false

	l.headlessMu.Lock()
	l.headlessQueues[slug] = append(l.headlessQueues[slug], headlessCodexTurn{
		Prompt:     prompt,
		EnqueuedAt: time.Now(),
	})
	if !l.headlessWorkers[slug] {
		l.headlessWorkers[slug] = true
		startWorker = true
	}
	if active := l.headlessActive[slug]; active != nil && active.Cancel != nil {
		age := time.Since(active.StartedAt)
		if age >= headlessCodexStaleCancelAfter {
			cancel = active.Cancel
			staleAge = age
		}
	}
	l.headlessMu.Unlock()

	if cancel != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("stale-turn: cancelling active turn after %s to process queued work", staleAge.Round(time.Second)))
		l.updateHeadlessProgress(slug, "active", "queued", "preempting stale work for newer request", headlessProgressMetrics{})
		cancel()
	}
	if startWorker {
		go l.runHeadlessCodexQueue(slug)
	}
}

func (l *Launcher) runHeadlessCodexQueue(slug string) {
	for {
		turn, turnCtx, ok := l.beginHeadlessCodexTurn(slug)
		if !ok {
			l.updateHeadlessProgress(slug, "idle", "idle", "waiting for work", headlessProgressMetrics{})
			l.finishHeadlessWorker(slug)
			return
		}
		appendHeadlessCodexLatency(slug, fmt.Sprintf("stage=started queue_wait_ms=%d", time.Since(turn.EnqueuedAt).Milliseconds()))
		l.updateHeadlessProgress(slug, "active", "queued", "queued work packet received", headlessProgressMetrics{})

		err := headlessCodexRunTurn(l, turnCtx, slug, turn.Prompt)
		ctxErr := turnCtx.Err()
		switch {
		case err == nil:
		case errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
			appendHeadlessCodexLog(slug, fmt.Sprintf("error: headless codex turn timed out after %s", headlessCodexTurnTimeout))
			l.updateHeadlessProgress(slug, "error", "error", fmt.Sprintf("turn timed out after %s", headlessCodexTurnTimeout), headlessProgressMetrics{})
		case errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled):
			appendHeadlessCodexLog(slug, "error: headless codex turn cancelled so newer queued work can run")
			l.updateHeadlessProgress(slug, "active", "queued", "restarting on newer queued work", headlessProgressMetrics{})
		default:
			appendHeadlessCodexLog(slug, fmt.Sprintf("error: %v", err))
			l.updateHeadlessProgress(slug, "error", "error", truncate(err.Error(), 180), headlessProgressMetrics{})
		}
		l.finishHeadlessTurn(slug)
	}
}

func (l *Launcher) finishHeadlessTurn(slug string) {
	l.headlessMu.Lock()
	if active := l.headlessActive[slug]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	delete(l.headlessActive, slug)
	l.headlessMu.Unlock()
}

func (l *Launcher) finishHeadlessWorker(slug string) {
	l.headlessMu.Lock()
	delete(l.headlessWorkers, slug)
	if len(l.headlessQueues[slug]) == 0 {
		delete(l.headlessQueues, slug)
	}
	l.headlessMu.Unlock()
}

func (l *Launcher) beginHeadlessCodexTurn(slug string) (headlessCodexTurn, context.Context, bool) {
	l.headlessMu.Lock()
	defer l.headlessMu.Unlock()

	queue := l.headlessQueues[slug]
	if len(queue) == 0 {
		delete(l.headlessQueues, slug)
		return headlessCodexTurn{}, nil, false
	}

	turn := queue[0]
	if len(queue) == 1 {
		delete(l.headlessQueues, slug)
	} else {
		l.headlessQueues[slug] = queue[1:]
	}

	baseCtx := l.headlessCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	turnCtx, cancel := context.WithTimeout(baseCtx, headlessCodexTurnTimeout)
	l.headlessActive[slug] = &headlessCodexActiveTurn{
		Turn:      turn,
		StartedAt: time.Now(),
		Cancel:    cancel,
	}
	return turn, turnCtx, true
}

func (l *Launcher) runHeadlessCodexTurn(ctx context.Context, slug string, notification string) error {
	if _, err := headlessCodexLookPath("codex"); err != nil {
		return fmt.Errorf("codex not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}

	overrides, err := l.buildCodexOfficeConfigOverrides(slug)
	if err != nil {
		return err
	}

	args := make([]string, 0, 16+len(overrides)*2)
	if l.unsafe {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "-a", "never", "-s", "workspace-write")
	}
	args = append(args,
		"exec",
		"-C", l.cwd,
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--json",
	)
	if model := strings.TrimSpace(config.ResolveCodexModel(l.cwd)); model != "" {
		args = append(args, "--model", model)
	}
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	args = append(args, "-")

	cmd := headlessCodexCommandContext(ctx, "codex", args...)
	cmd.Dir = l.cwd
	cmd.Env = l.buildHeadlessCodexEnv(slug)
	cmd.Stdin = strings.NewReader(buildHeadlessCodexPrompt(l.buildPrompt(slug), notification))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach codex stdout: %w", err)
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	startedAt := time.Now()
	metrics := headlessProgressMetrics{
		TotalMs:      -1,
		FirstEventMs: -1,
		FirstTextMs:  -1,
		FirstToolMs:  -1,
	}
	l.updateHeadlessProgress(slug, "active", "thinking", "reviewing work packet", metrics)
	var firstEventAt time.Time
	var firstTextAt time.Time
	var firstToolAt time.Time
	textStarted := false
	result, parseErr := provider.ReadCodexJSONStream(stdout, func(event provider.CodexStreamEvent) {
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			metrics.FirstEventMs = durationMillis(startedAt, firstEventAt)
		}
		switch event.Type {
		case "text":
			if firstTextAt.IsZero() && strings.TrimSpace(event.Text) != "" {
				firstTextAt = time.Now()
				metrics.FirstTextMs = durationMillis(startedAt, firstTextAt)
			}
			if !textStarted && strings.TrimSpace(event.Text) != "" {
				textStarted = true
				l.updateHeadlessProgress(slug, "active", "text", "drafting response", metrics)
			}
		case "tool_use":
			if firstToolAt.IsZero() {
				firstToolAt = time.Now()
				metrics.FirstToolMs = durationMillis(startedAt, firstToolAt)
			}
			appendHeadlessCodexLog(slug, fmt.Sprintf("tool_use: %s %s", event.ToolName, truncate(event.ToolInput, 120)))
			l.updateHeadlessProgress(slug, "active", "tool_use", fmt.Sprintf("running %s", strings.TrimSpace(event.ToolName)), metrics)
		case "tool_result":
			appendHeadlessCodexLog(slug, "tool_result: "+truncate(event.Text, 140))
			l.updateHeadlessProgress(slug, "active", "tool_result", truncate(event.Text, 140), metrics)
		case "error":
			appendHeadlessCodexLog(slug, "stream_error: "+event.Detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(event.Detail, 180), metrics)
		}
	})
	if err := cmd.Wait(); err != nil {
		detail := firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()))
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		if detail != "" {
			appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
				time.Since(startedAt).Milliseconds(),
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				durationMillis(startedAt, firstToolAt),
				detail,
			))
			appendHeadlessCodexLog(slug, "stderr: "+detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), metrics)
			return fmt.Errorf("%w: %s", err, detail)
		}
		appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
			time.Since(startedAt).Milliseconds(),
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			durationMillis(startedAt, firstToolAt),
			err.Error(),
		))
		return err
	}
	if parseErr != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		l.updateHeadlessProgress(slug, "error", "error", truncate(parseErr.Error(), 180), metrics)
		return parseErr
	}
	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d",
		time.Since(startedAt).Milliseconds(),
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		durationMillis(startedAt, firstToolAt),
		len(strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine))),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	if text := strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine)); text != "" {
		appendHeadlessCodexLog(slug, "result: "+text)
	}
	return nil
}

func (l *Launcher) buildHeadlessCodexEnv(slug string) []string {
	env := os.Environ()
	env = append(env,
		"WUPHF_AGENT_SLUG="+slug,
		"WUPHF_BROKER_TOKEN="+l.broker.Token(),
		"WUPHF_HEADLESS_PROVIDER=codex",
	)
	if config.ResolveNoNex() {
		env = append(env, "WUPHF_NO_NEX=1")
	}
	if l.isOneOnOne() {
		env = append(env,
			"WUPHF_ONE_ON_ONE=1",
			"WUPHF_ONE_ON_ONE_AGENT="+l.oneOnOneAgent(),
		)
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		env = append(env, "ONE_SECRET="+secret)
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env = append(env, "ONE_IDENTITY="+identity)
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env = append(env, "ONE_IDENTITY_TYPE="+identityType)
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
		env = append(env,
			"WUPHF_API_KEY="+apiKey,
			"NEX_API_KEY="+apiKey,
		)
	}
	return env
}

func (l *Launcher) buildCodexOfficeConfigOverrides(slug string) ([]string, error) {
	wuphfBinary, err := headlessCodexExecutablePath()
	if err != nil {
		return nil, err
	}
	wuphfEnvVars := []string{
		"WUPHF_AGENT_SLUG",
		"WUPHF_BROKER_TOKEN",
	}
	if config.ResolveNoNex() {
		wuphfEnvVars = append(wuphfEnvVars, "WUPHF_NO_NEX")
	}
	if l.isOneOnOne() {
		wuphfEnvVars = append(wuphfEnvVars,
			"WUPHF_ONE_ON_ONE",
			"WUPHF_ONE_ON_ONE_AGENT",
		)
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		wuphfEnvVars = append(wuphfEnvVars, "ONE_SECRET")
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		wuphfEnvVars = append(wuphfEnvVars, "ONE_IDENTITY")
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			wuphfEnvVars = append(wuphfEnvVars, "ONE_IDENTITY_TYPE")
		}
	}

	overrides := []string{
		fmt.Sprintf(`mcp_servers.wuphf-office.command=%s`, tomlQuote(wuphfBinary)),
		`mcp_servers.wuphf-office.args=["mcp-team"]`,
		fmt.Sprintf(`mcp_servers.wuphf-office.env_vars=%s`, tomlStringArray(wuphfEnvVars)),
	}

	if !config.ResolveNoNex() {
		if nexMCP, err := headlessCodexLookPath("nex-mcp"); err == nil {
			overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.command=%s`, tomlQuote(nexMCP)))
			if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
				overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.env_vars=%s`, tomlStringArray([]string{
					"WUPHF_API_KEY",
					"NEX_API_KEY",
				})))
			}
		}
	}

	return overrides, nil
}

func buildHeadlessCodexPrompt(systemPrompt string, prompt string) string {
	var parts []string
	if trimmed := strings.TrimSpace(systemPrompt); trimmed != "" {
		parts = append(parts, "<system>\n"+trimmed+"\n</system>")
	}
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, "\n\n")
}

func appendHeadlessCodexLog(slug string, line string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".wuphf", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(logDir, "headless-codex-"+slug+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessCodexLatency(slug string, line string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(home, ".wuphf", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return
	}
	path := filepath.Join(logDir, "headless-codex-latency.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}

func durationMillis(start, mark time.Time) int64 {
	if start.IsZero() || mark.IsZero() {
		return -1
	}
	return mark.Sub(start).Milliseconds()
}

func tomlQuote(value string) string {
	return fmt.Sprintf("%q", value)
}

func tomlStringArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, tomlQuote(value))
	}
	if len(parts) == 0 {
		return "[]"
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
