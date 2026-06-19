package team

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gitexec"
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
		"--model", l.headlessClaudeModel(ctx, slug),
		"--print", "-",
		"--output-format", "stream-json",
		"--verbose",
		"--max-turns", l.headlessClaudeMaxTurns(slug),
		"--disable-slash-commands",
		"--setting-sources", "user",
		"--append-system-prompt", l.buildPrompt(slug),
		"--mcp-config", agentMCP,
		"--strict-mcp-config",
		// NOTE: tried --disallowedTools ToolSearch to block the
		// deferred-tools reminder loop. claude-code requires ToolSearch
		// when MCP tools are deferred; the agent exited with SIGTERM
		// after ~23s and zero events. Reverted. The prompt's TOOL
		// HYGIENE block continues to instruct the model to ignore the
		// reminder and call MCP tools directly; we accept the soft
		// failure mode (~30s tax on first turn) over the hard one
		// (agent dies before producing any output).
	}
	args = append(args, strings.Fields(l.resolvePermissionFlags())...)

	// Per-task reasoning effort: when the active task carries a composer-set
	// effort that claude accepts, pass it as `--effort <level>`. Empty/unknown
	// normalises away so the CLI keeps its default (high).
	if effort := normalizeClaudeEffort(l.activeTaskEffort(ctx, slug)); effort != "" {
		args = append(args, "--effort", effort)
	}

	// Workspace isolation: coding agents get their own git worktree. Resolve the
	// worktree for THIS turn's task (via ctx) so a parallel instance writes its
	// own per-task worktree instead of whichever in_progress task is first.
	worktreeDir := ""
	if codingAgentSlugs[slug] && l.broker != nil {
		if task := l.turnTaskForCtx(ctx, slug); task != nil && strings.TrimSpace(task.ID) != "" {
			if wPath, _, err := prepareTaskWorktree(task.ID); err == nil {
				worktreeDir = wPath
			}
		}
	}
	if worktreeDir == "" {
		// Non-coding agents (CEO included) still honor an assigned
		// local_worktree path on this turn's task.
		worktreeDir = strings.TrimSpace(l.headlessTaskWorkspaceDir(slug, headlessTurnTaskID(ctx)))
	}

	cmd := headlessClaudeCommandContext(ctx, "claude", args...)
	if worktreeDir != "" {
		cmd.Dir = worktreeDir
	} else {
		// V3-N5: a turn without a task worktree (chat turns, office-mode
		// task turns) runs in the agent's scratch dir inside the office
		// runtime home — NEVER the broker process launch cwd. The v3 live
		// run had the CEO writing landing/index.html into (and later
		// `git checkout`-destroying it inside) the founder's host repo.
		cmd.Dir = agentScratchDir(slug)
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
	taskID := l.turnTaskIDForCtx(ctx, slug)
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}
	pr, pw := io.Pipe()
	teedStdout := io.TeeReader(stdout, pw)
	// Reader-based drain: an oversized provider line that exceeds any fixed
	// scanner buffer must not stop the loop, since stopping leaves the tee
	// pipe undrained and wedges cmd.Wait() on backpressure. Mirrors the
	// pattern used by the codex runner tee. See provider.DrainStreamLines
	// for the underlying contract.
	go func() {
		_ = provider.DrainStreamLines(pr, func(chunk string) {
			if agentStream != nil && chunk != "" {
				agentStream.PushTask(taskID, chunk)
			}
		})
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

	// Live-chat relay streams the agent's user-facing `text` output to the
	// channel as it's generated, so a long turn doesn't sit silent until the
	// final summary. Claude's `thinking` blocks are intentionally not piped:
	// those are private chain-of-thought, not "items that concern the user
	// and other agents". The model's `text` output is what the agent has
	// chosen to surface, and the relay's sentence/paragraph flush boundaries
	// keep the channel from being flooded with mid-token chunks.
	target := firstNonEmpty(channel...)
	relay := newHeadlessLiveChatRelay(l, slug, target, notification, func(line string) {
		appendHeadlessClaudeLog(slug, line)
	})
	// Defer the flush so error/parseErr exit paths still surface the
	// trailing buffered sentence. The explicit Flush before the final
	// post stays — once the buffer is empty, the deferred call is a
	// no-op.
	defer relay.Flush()

	var firstEventAt time.Time
	var firstTextAt time.Time
	var firstToolAt time.Time
	textStarted := false
	turnID := newHeadlessTurnID()
	var turnToolNames []string
	var turnTextLen int
	// Workflow-detection trace capture: correlate each integration tool_use with
	// its following tool_result so the completion-time extractor has the real
	// (masked) args + response shape, not just the action name. Pending holds the
	// in-flight proxy call until its result arrives; flushed on the next tool_use
	// or at turn end. See trace_sink.go.
	var pendingTrace *ActionTrace
	traceSeq := 0
	flushTrace := func() {
		if pendingTrace != nil {
			persistActionTrace(*pendingTrace)
			pendingTrace = nil
		}
	}

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
			relay.OnText(event.Text)
			turnTextLen += len(event.Text)
			emitHeadlessText(agentStream, turnID, HeadlessProviderClaude, slug, taskID, event.Text, "claude.text")
		case "tool_use":
			relay.Flush()
			if firstToolAt.IsZero() {
				firstToolAt = time.Now()
				metrics.FirstToolMs = durationMillis(startedAt, firstToolAt)
			}
			appendHeadlessClaudeLog(slug, fmt.Sprintf("tool_use: %s %s", event.ToolName, truncate(event.ToolInput, 120)))
			l.updateHeadlessProgress(slug, "active", "tool_use", fmt.Sprintf("running %s", strings.TrimSpace(event.ToolName)), metrics)
			// Record the workflow-detection shape token, not the raw tool name:
			// the generic external-action proxy is unwrapped to its real
			// action_id so integration steps become visible to the miner
			// (manifestToolToken). The live tool_use event below keeps the true
			// tool name + full input untouched.
			turnToolNames = append(turnToolNames, manifestToolToken(event.ToolName, event.ToolInput))
			// A new tool_use means the prior proxy call's result (if any) already
			// arrived (sequential agent), so flush it; then start tracing this one.
			flushTrace()
			if tr, ok := traceFromToolUse(taskID, turnID, slug, event.ToolName, event.ToolInput, traceSeq); ok {
				traceSeq++
				pendingTrace = &tr
			}
			emitHeadlessToolUse(agentStream, turnID, HeadlessProviderClaude, slug, taskID, event.ToolName, event.ToolInput, "claude.tool_use")
		case "tool_result":
			appendHeadlessClaudeLog(slug, "tool_result: "+truncate(event.Text, 140))
			l.updateHeadlessProgress(slug, "active", "tool_result", truncate(event.Text, 140), metrics)
			if pendingTrace != nil {
				// Prefer the untruncated ResultRaw so result_path/expose can be
				// inferred from the full response shape, not the 500-char display
				// clip; summarizeResult bounds it to a shape-preserving summary.
				raw := event.ResultRaw
				if strings.TrimSpace(raw) == "" {
					raw = event.Text
				}
				pendingTrace.Result = summarizeResult(raw)
				flushTrace()
			}
			emitHeadlessToolResult(agentStream, turnID, HeadlessProviderClaude, slug, taskID, event.ToolName, event.Text, "claude.tool_result")
		case "error":
			appendHeadlessClaudeLog(slug, "stream_error: "+event.Detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(event.Detail, 180), metrics)
		}
	})
	_ = pw.Close() // signal scanner goroutine that stream is done (io.PipeWriter.Close always returns nil)
	flushTrace()   // persist a trailing integration call whose result closed the turn
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
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderClaude, slug, taskID, "", detail, metrics, claudeUsageToTokenUsage(result.Usage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderClaude, slug, taskID, detail, turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(result.Usage))
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
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderClaude, slug, taskID, "", parseErr.Error(), metrics, claudeUsageToTokenUsage(result.Usage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderClaude, slug, taskID, parseErr.Error(), turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(result.Usage))
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
	emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderClaude, slug, taskID, summary, "", metrics, claudeUsageToTokenUsage(result.Usage))
	emitHeadlessManifest(agentStream, turnID, HeadlessProviderClaude, slug, taskID, "", turnToolNames, turnTextLen, metrics, claudeUsageToTokenUsage(result.Usage))
	if l.broker != nil {
		l.broker.RecordAgentUsage(slug, l.headlessClaudeModel(ctx, slug), result.Usage)
	}
	relay.Flush()
	finalText := strings.TrimSpace(result.FinalMessage)
	if finalText != "" {
		appendHeadlessClaudeLog(slug, "result: "+finalText)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, finalText, startedAt)
		if err != nil {
			appendHeadlessClaudeLog(slug, "fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessClaudeLog(slug, fmt.Sprintf("fallback-post: posted final output to #%s as %s", msg.Channel, msg.ID))
		}
	}
	return nil
}

func (l *Launcher) headlessClaudeModel(ctx context.Context, slug string) string {
	// Per-agent override wins: when the user picks a specific model in the
	// AgentProfilePanel runtime section (or AgentWizard), that's the
	// model the next dispatch must use. Without this check the picker
	// silently rewrote ProviderBinding.Model but every turn still ran
	// against the hardcoded default — the user-visible symptom was
	// "I picked a different model and nothing changed."
	//
	// The per-agent binding is only consulted when its kind is also
	// claude-code: if a user moved the agent to codex with model=gpt-4o,
	// we must not feed gpt-4o to claude on a later switch back. The
	// runtime-switch flow clears the binding entirely on kind change
	// (see AgentProfilePanel save path), so the most common edge cases
	// are already prevented at the source, but the kind check here is
	// belt-and-suspenders.
	// Per-task model wins over the agent binding (the model lives on the task,
	// not the agent). Only when the task's provider is claude-code.
	if model := l.taskModelForKind(ctx, slug, provider.KindClaudeCode); model != "" {
		return model
	}
	if l != nil && l.broker != nil {
		binding := l.broker.MemberProviderBinding(slug)
		if binding.Kind == "claude-code" {
			if model := strings.TrimSpace(binding.Model); model != "" {
				return model
			}
		}
	}
	// Anthropic family default (verified 2026-05-29). The lead falls back
	// to the top opus tier when --opus-ceo is on; everyone else gets the
	// best-balanced sonnet. claude-opus-4-6 (the previous default here)
	// is still available but no longer the recommended pick — see
	// https://platform.claude.com/docs/en/docs/about-claude/models.
	if l.opusCEO && slug == l.targeter().LeadSlug() {
		return "claude-opus-4-8"
	}
	return "claude-sonnet-4-6"
}

// headlessClaudeMaxTurns returns the turn budget for an agent. The CEO routes
// untagged and DM messages, which typically requires looking up tasks, channel
// members, and posting an assignment — easily more than 5 turns. Specialists
// get a smaller budget since they focus on a single task.
func (l *Launcher) headlessClaudeMaxTurns(slug string) string {
	if slug == l.targeter().LeadSlug() {
		return "30"
	}
	return "15"
}

// claudeUsageToTokenUsage adapts the provider-level ClaudeUsage record
// into the runner-agnostic envelope HeadlessEvent expects. Cost and
// cache-token fields are dropped: the wire shape only carries
// input/output for now, and adding more fields here would force a wire
// change for every runner.
func claudeUsageToTokenUsage(u provider.ClaudeUsage) *headlessTokenUsage {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return &headlessTokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
}

func (l *Launcher) buildHeadlessClaudeEnv(slug string) []string {
	// gitexec.CleanEnv: a spawned claude agent will run
	// `git status/diff/commit` inside its sandbox. If wuphf inherited
	// GIT_DIR (e.g. launched from a git hook) every child `git` would
	// silently retarget the outer repo.
	env := gitexec.CleanEnv()
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
