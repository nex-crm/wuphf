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
)

var (
	headlessCodexLookPath       = exec.LookPath
	headlessCodexCommandContext = exec.CommandContext
	headlessCodexExecutablePath = os.Executable
	headlessCodexRunTurn        = func(l *Launcher, ctx context.Context, slug, notification string) error {
		return l.runHeadlessCodexTurn(ctx, slug, notification)
	}
)

var (
	headlessCodexTurnTimeout      = 2 * time.Minute
	headlessCodexStaleCancelAfter = 45 * time.Second
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
	if err := l.broker.SetFocusMode(l.focusMode); err != nil {
		return fmt.Errorf("set focus mode: %w", err)
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
			l.finishHeadlessWorker(slug)
			return
		}

		err := headlessCodexRunTurn(l, turnCtx, slug, turn.Prompt)
		ctxErr := turnCtx.Err()
		switch {
		case err == nil:
		case errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
			appendHeadlessCodexLog(slug, fmt.Sprintf("error: headless codex turn timed out after %s", headlessCodexTurnTimeout))
		case errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled):
			appendHeadlessCodexLog(slug, "error: headless codex turn cancelled so newer queued work can run")
		default:
			appendHeadlessCodexLog(slug, fmt.Sprintf("error: %v", err))
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

	result, parseErr := readHeadlessCodexJSONStream(stdout)
	if err := cmd.Wait(); err != nil {
		detail := firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()))
		if detail != "" {
			appendHeadlessCodexLog(slug, "stderr: "+detail)
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	if parseErr != nil {
		return parseErr
	}
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
	if l.isFocusModeEnabled() {
		env = append(env, "WUPHF_FOCUS_MODE=1")
	}
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
	if l.isFocusModeEnabled() {
		wuphfEnvVars = append(wuphfEnvVars, "WUPHF_FOCUS_MODE")
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

type headlessCodexJSONResult struct {
	FinalMessage  string
	LastPlainLine string
	LastError     string
}

type headlessCodexJSONEvent struct {
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

func readHeadlessCodexJSONStream(r io.Reader) (headlessCodexJSONResult, error) {
	var result headlessCodexJSONResult
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event headlessCodexJSONEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			result.LastPlainLine = line
			continue
		}
		if text := strings.TrimSpace(extractHeadlessCodexAgentMessage(event)); text != "" {
			result.FinalMessage = text
		}
		if detail := strings.TrimSpace(extractHeadlessCodexError(event)); detail != "" {
			result.LastError = detail
		}
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("read codex json stream: %w", err)
	}
	return result, nil
}

func extractHeadlessCodexAgentMessage(event headlessCodexJSONEvent) string {
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

func extractHeadlessCodexError(event headlessCodexJSONEvent) string {
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
