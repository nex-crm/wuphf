package team

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
)

var (
	headlessClaudeLookPath       = exec.LookPath
	headlessClaudeCommandContext = exec.CommandContext
)

func (l *Launcher) runHeadlessClaudeTurn(ctx context.Context, slug string, notification string, channel ...string) error {
	if _, err := headlessClaudeLookPath("claude"); err != nil {
		return fmt.Errorf("claude not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}

	// Per-agent MCP scoping: give each agent only the MCP servers it needs.
	agentMCP := l.mcpConfig
	if path, err := l.ensureAgentMCPConfig(slug); err == nil {
		agentMCP = path
	}

	args := []string{
		"--model", l.headlessClaudeModel(slug),
		"--print", "-",
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", l.headlessClaudeMaxTurns(slug),
		"--disable-slash-commands",
		"--setting-sources", "user",
		"--append-system-prompt", l.buildPrompt(slug),
		"--mcp-config", agentMCP,
		"--strict-mcp-config",
	}
	args = append(args, strings.Fields(l.resolvePermissionFlags(slug))...)

	// Workspace isolation: coding agents get their own git worktree.
	worktreeDir := ""
	if codingAgentSlugs[slug] && l.broker != nil {
		task := l.agentActiveTask(slug)
		if task != nil && strings.TrimSpace(task.ID) != "" {
			if wPath, _, err := prepareTaskWorktree(task.ID); err == nil {
				worktreeDir = wPath
			}
		}
	}

	cmd := headlessClaudeCommandContext(ctx, "claude", args...)
	if worktreeDir != "" {
		cmd.Dir = worktreeDir
	} else {
		cmd.Dir = l.cwd
	}
	configureHeadlessProcess(cmd)
	env := l.buildHeadlessClaudeEnv(slug)
	if worktreeDir != "" {
		env = append(env, "WUPHF_WORKTREE_PATH="+worktreeDir)
	}
	cmd.Env = env

	// Enrich the notification with Nex entity context. Use a 2s deadline so a
	// slow or unreachable memory backend never holds up the agent turn.
	//
	// The memory brief can contain attacker-controlled data (email bodies, CRM
	// notes, calendar entries, etc.), so it is appended AFTER the operator's
	// notification and wrapped in an explicitly untrusted fence. Putting
	// attacker data before the operator's instructions is a known prompt-
	// injection vector; last-message anchoring is where the agent's attention
	// lands, so the operator's notification stays first.
	memoryCtx, memoryCancel := context.WithTimeout(ctx, 2*time.Second)
	brief := fetchScopedMemoryBrief(memoryCtx, slug, notification, l.broker)
	memoryCancel()
	stdinPayload := composeHeadlessStdinPayload(notification, brief)
	cmd.Stdin = strings.NewReader(stdinPayload)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach claude stdout: %w", err)
	}

	// Pipe raw stdout to the agent stream for the web UI's live output pane.
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

	var firstEventAt time.Time
	var firstTextAt time.Time
	var firstToolAt time.Time
	textStarted := false

	result, parseErr := provider.ReadClaudeJSONStream(teedStdout, func(event provider.ClaudeStreamEvent) {
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			metrics.FirstEventMs = durationMillis(startedAt, firstEventAt)
		}
		switch event.Type {
		case "thinking":
			l.updateHeadlessProgress(slug, "active", "thinking", "planning next step", metrics)
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
			appendHeadlessClaudeLog(slug, fmt.Sprintf("tool_use: %s %s", event.ToolName, truncate(event.ToolInput, 120)))
			l.updateHeadlessProgress(slug, "active", "tool_use", fmt.Sprintf("running %s", strings.TrimSpace(event.ToolName)), metrics)
		case "tool_result":
			appendHeadlessClaudeLog(slug, "tool_result: "+truncate(event.Text, 140))
			l.updateHeadlessProgress(slug, "active", "tool_result", truncate(event.Text, 140), metrics)
		case "error":
			appendHeadlessClaudeLog(slug, "stream_error: "+event.Detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(event.Detail, 180), metrics)
		}
	})
	_ = pw.Close() // signal scanner goroutine that stream is done (io.PipeWriter.Close always returns nil)
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()), err.Error()))
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		appendHeadlessClaudeLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
			metrics.TotalMs,
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			durationMillis(startedAt, firstToolAt),
			detail,
		))
		l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), metrics)
		return fmt.Errorf("%w: %s", err, detail)
	}
	if parseErr != nil {
		metrics.TotalMs = time.Since(startedAt).Milliseconds()
		appendHeadlessClaudeLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
			metrics.TotalMs,
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			durationMillis(startedAt, firstToolAt),
			parseErr.Error(),
		))
		l.updateHeadlessProgress(slug, "error", "error", truncate(parseErr.Error(), 180), metrics)
		return parseErr
	}

	metrics.TotalMs = time.Since(startedAt).Milliseconds()
	appendHeadlessClaudeLatency(slug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d",
		metrics.TotalMs,
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		durationMillis(startedAt, firstToolAt),
		len(strings.TrimSpace(result.FinalMessage)),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metrics))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metrics)
	if l.broker != nil {
		l.broker.RecordAgentUsage(slug, l.headlessClaudeModel(slug), result.Usage)
	}
	if text := strings.TrimSpace(result.FinalMessage); text != "" {
		appendHeadlessClaudeLog(slug, "result: "+text)
		target := firstNonEmpty(channel...)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt)
		if err != nil {
			appendHeadlessClaudeLog(slug, "fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessClaudeLog(slug, fmt.Sprintf("fallback-post: posted final output to #%s as %s", msg.Channel, msg.ID))
		}
	}
	return nil
}

func (l *Launcher) headlessClaudeModel(slug string) string {
	if l.opusCEO && slug == l.officeLeadSlug() {
		return "claude-opus-4-6"
	}
	return "claude-sonnet-4-6"
}

// headlessClaudeMaxTurns returns the turn budget for an agent. The CEO routes
// untagged and DM messages, which typically requires looking up tasks, channel
// members, and posting an assignment — easily more than 5 turns. Specialists
// get a smaller budget since they focus on a single task.
func (l *Launcher) headlessClaudeMaxTurns(slug string) string {
	if slug == l.officeLeadSlug() {
		return "30"
	}
	return "15"
}

func (l *Launcher) buildHeadlessClaudeEnv(slug string) []string {
	// gitCleanEnv: a spawned claude agent will run `git status/diff/commit`
	// inside its sandbox. If wuphf inherited GIT_DIR (e.g. launched from a
	// git hook) every child `git` would silently retarget the outer repo.
	env := gitCleanEnv()
	env = append(env,
		"WUPHF_AGENT_SLUG="+slug,
		"WUPHF_BROKER_TOKEN="+l.broker.Token(),
		"WUPHF_BROKER_BASE_URL="+l.BrokerBaseURL(),
		"WUPHF_HEADLESS_PROVIDER=claude",
		"WUPHF_MEMORY_BACKEND="+config.ResolveMemoryBackend(""),
		fmt.Sprintf("WUPHF_NO_NEX=%t", config.ResolveNoNex()),
		"ANTHROPIC_PROMPT_CACHING=1",
	)
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

func appendHeadlessClaudeLog(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-claude-"+slug+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessClaudeLatency(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-claude-latency.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}
