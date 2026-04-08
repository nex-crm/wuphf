package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

var (
	headlessCodexLookPath       = exec.LookPath
	headlessCodexCommandContext = exec.CommandContext
	headlessCodexExecutablePath = os.Executable
)

func (l *Launcher) launchHeadlessCodex() error {
	killStaleBroker()

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

	l.headlessMu.Lock()
	if l.headlessRunning[slug] {
		l.headlessPending[slug] = prompt
		l.headlessMu.Unlock()
		return
	}
	l.headlessRunning[slug] = true
	l.headlessMu.Unlock()

	go l.runHeadlessCodexQueue(slug, prompt)
}

func (l *Launcher) runHeadlessCodexQueue(slug string, prompt string) {
	current := prompt
	for {
		if ctx := l.headlessCtx; ctx != nil {
			select {
			case <-ctx.Done():
				l.finishHeadlessTurn(slug)
				return
			default:
			}
		}

		if err := l.runHeadlessCodexTurn(slug, current); err != nil {
			appendHeadlessCodexLog(slug, fmt.Sprintf("error: %v", err))
		}

		l.headlessMu.Lock()
		next, ok := l.headlessPending[slug]
		if ok {
			delete(l.headlessPending, slug)
			l.headlessMu.Unlock()
			current = next
			continue
		}
		delete(l.headlessRunning, slug)
		l.headlessMu.Unlock()
		return
	}
}

func (l *Launcher) finishHeadlessTurn(slug string) {
	l.headlessMu.Lock()
	delete(l.headlessRunning, slug)
	delete(l.headlessPending, slug)
	l.headlessMu.Unlock()
}

func (l *Launcher) runHeadlessCodexTurn(slug string, notification string) error {
	if _, err := headlessCodexLookPath("codex"); err != nil {
		return fmt.Errorf("codex not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}

	outputFile, err := os.CreateTemp("", "wuphf-office-codex-*.txt")
	if err != nil {
		return fmt.Errorf("create codex output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

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
	)
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	args = append(args, "--output-last-message", outputPath, "-")

	ctx := l.headlessCtx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := headlessCodexCommandContext(ctx, "codex", args...)
	cmd.Dir = l.cwd
	cmd.Env = l.buildHeadlessCodexEnv(slug)
	cmd.Stdin = strings.NewReader(buildHeadlessCodexPrompt(l.buildPrompt(slug), notification))

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stderrText := strings.TrimSpace(stderr.String()); stderrText != "" {
			appendHeadlessCodexLog(slug, "stderr: "+stderrText)
			return fmt.Errorf("%w: %s", err, stderrText)
		}
		return err
	}

	if raw, err := os.ReadFile(outputPath); err == nil {
		if text := strings.TrimSpace(string(raw)); text != "" {
			appendHeadlessCodexLog(slug, "result: "+text)
		}
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
	return env
}

func (l *Launcher) buildCodexOfficeConfigOverrides(slug string) ([]string, error) {
	wuphfBinary, err := headlessCodexExecutablePath()
	if err != nil {
		return nil, err
	}
	wuphfEnv := map[string]string{
		"WUPHF_AGENT_SLUG":   slug,
		"WUPHF_BROKER_TOKEN": l.broker.Token(),
	}
	if config.ResolveNoNex() {
		wuphfEnv["WUPHF_NO_NEX"] = "1"
	}
	if l.isOneOnOne() {
		wuphfEnv["WUPHF_ONE_ON_ONE"] = "1"
		wuphfEnv["WUPHF_ONE_ON_ONE_AGENT"] = l.oneOnOneAgent()
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		wuphfEnv["ONE_SECRET"] = secret
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		wuphfEnv["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			wuphfEnv["ONE_IDENTITY_TYPE"] = identityType
		}
	}

	overrides := []string{
		fmt.Sprintf(`mcp_servers.wuphf-office.command=%s`, tomlQuote(wuphfBinary)),
		`mcp_servers.wuphf-office.args=["mcp-team"]`,
		fmt.Sprintf(`mcp_servers.wuphf-office.env=%s`, tomlInlineTable(wuphfEnv)),
	}

	if !config.ResolveNoNex() {
		if nexMCP, err := headlessCodexLookPath("nex-mcp"); err == nil {
			overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.command=%s`, tomlQuote(nexMCP)))
			nexEnv := map[string]string{}
			if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
				nexEnv["WUPHF_API_KEY"] = apiKey
				nexEnv["NEX_API_KEY"] = apiKey
			}
			if len(nexEnv) > 0 {
				overrides = append(overrides, fmt.Sprintf(`mcp_servers.nex.env=%s`, tomlInlineTable(nexEnv)))
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

func tomlInlineTable(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, tomlQuote(values[key])))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
