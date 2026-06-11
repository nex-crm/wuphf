package team

// headless_codex_runner.go owns the codex-CLI invocation half of
// headless dispatch (PLAN.md §C10). Hosts runHeadlessCodexTurn — the
// 220-line method that builds the codex command line, pipes prompt
// over stdin, parses the JSON event stream, and surfaces the result —
// plus its env/auth/workspace builders and the toml/env utility
// helpers it depends on. Split out of headless_codex.go so that file
// can stay focused on entry points + types.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/gitexec"
	"github.com/nex-crm/wuphf/internal/provider"
)

func (l *Launcher) runHeadlessCodexTurn(ctx context.Context, slug string, notification string, channel ...string) error {
	if _, err := headlessCodexLookPath("codex"); err != nil {
		return fmt.Errorf("codex not found: %w", err)
	}
	if l == nil || l.broker == nil {
		return fmt.Errorf("broker is not running")
	}
	if err := l.preflightHeadlessCodexAuth(slug, firstNonEmpty(channel...)); err != nil {
		return err
	}

	// Workspace isolation (V3-N5): task worktree when this turn's task has
	// one, else the agent's scratch dir inside the office runtime home.
	// NEVER the broker process launch cwd — the v3 live run had agents
	// writing into (and `git checkout`-reverting) the founder's host repo.
	workspaceDir, isTaskWorktree := l.headlessTurnWorkspace(slug, headlessTurnTaskID(ctx))

	overrides, err := l.buildCodexOfficeConfigOverrides(slug)
	if err != nil {
		return err
	}

	args := make([]string, 0, 16+len(overrides)*2)
	// Sandbox posture for this turn:
	//   - local-worktree / unsafe turn: full bypass. The child Codex
	//     sandbox rejects both apply_patch and shell writes even with
	//     workspace-write, which leaves coding tasks permanently unable to land
	//     edits.
	//   - office / non-editing turn: workspace-write.
	if l.unsafe || l.headlessCodexNeedsDangerousBypass(ctx, slug) {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "-a", "never", "-s", "workspace-write")
	}
	args = append(args, "--disable", "plugins")
	args = append(args,
		"exec",
		"-C", workspaceDir,
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--json",
	)
	if model := strings.TrimSpace(l.codexModelForAgent(ctx, slug)); model != "" {
		args = append(args, "--model", model)
	}
	// Per-task reasoning effort: when the active task carries a composer-set
	// effort that codex accepts, pass it as `-c model_reasoning_effort=<level>`.
	// Empty/unknown normalises away so codex keeps the model default.
	if effort := normalizeCodexEffort(l.activeTaskEffort(ctx, slug)); effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}
	for _, override := range overrides {
		args = append(args, "-c", override)
	}
	args = append(args, "-")

	cmd := headlessCodexCommandContext(ctx, "codex", args...)
	cmd.Dir = workspaceDir
	cmd.Env = l.buildHeadlessCodexEnv(slug, workspaceDir, firstNonEmpty(channel...))
	if isTaskWorktree {
		cmd.Env = append(cmd.Env, "WUPHF_WORKTREE_PATH="+workspaceDir)
	}
	stdinText := buildHeadlessCodexPrompt(l.buildPrompt(slug), notification)
	cmd.Stdin = strings.NewReader(stdinText)
	configureHeadlessProcess(cmd)
	dumpHeadlessCodexInvocation(slug, workspaceDir, args, cmd.Env, stdinText)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("attach codex stdout: %w", err)
	}

	// Tee raw stdout to the agent stream so the web UI can display live output.
	// The ReadCodexJSONStream parser doesn't emit streaming events for exec mode's
	// item.started/item.completed format, so we pipe raw lines directly.
	var agentStream *agentStreamBuffer
	taskID := l.turnTaskIDForCtx(ctx, slug)
	if l.broker != nil {
		agentStream = l.broker.AgentStream(slug)
	}
	pr, pw := io.Pipe()
	// Ensure the pipe writer is always closed so the drain goroutine below
	// cannot be orphaned. Normal-path callers still call pw.Close() explicitly
	// at line 210; the deferred close is a no-op in that case (io.PipeWriter
	// tolerates double-close). Guards against panics in ReadCodexJSONStream
	// that would otherwise strand the reader goroutine forever.
	defer func() { _ = pw.Close() }()
	teedStdout := io.TeeReader(stdout, pw)
	// Pipe every raw line from the provider to the web UI's live stream.
	// No filtering — the user sees everything the agent sees. The reader-
	// based drain in provider.DrainStreamLines guarantees an oversized
	// line cannot stop the loop, so io.TeeReader cannot wedge cmd.Wait
	// regardless of provider output size.
	go func() {
		err := provider.DrainStreamLines(pr, func(chunk string) {
			if agentStream != nil && chunk != "" {
				agentStream.PushTask(taskID, chunk)
			}
		})
		if err != nil {
			appendHeadlessCodexLog(slug, "stream-drain-error: "+err.Error())
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
	// metricsMu guards every read and write of metrics. The heartbeat tick
	// closure below runs on its own goroutine and reads the whole struct (via
	// updateHeadlessProgress) while the stream callback mutates FirstEventMs /
	// FirstTextMs / FirstToolMs and the post-Wait paths write TotalMs — a real
	// -race data race without this lock. Helpers snapshot a copy under the lock
	// so callers never pass the live struct to a concurrent reader.
	var metricsMu sync.Mutex
	snapshotMetrics := func() headlessProgressMetrics {
		metricsMu.Lock()
		defer metricsMu.Unlock()
		return metrics
	}
	setMetric := func(mutate func(m *headlessProgressMetrics)) {
		metricsMu.Lock()
		mutate(&metrics)
		metricsMu.Unlock()
	}
	l.updateHeadlessProgress(slug, "active", "thinking", "reviewing work packet", snapshotMetrics())

	// Coarse "still working (NNs)" heartbeat. A codex exec turn runs
	// ~80-120s; if the model is mid-reasoning and the parser sees no
	// item.* events for a stretch, the UI would otherwise freeze on a
	// stale badge. The heartbeat ticks a low-noise progress update every
	// codexHeartbeatInterval so the user always sees the turn is alive.
	// Real item events call heartbeat.Note() to reset the timer, so the
	// heartbeat only fires during genuine silence — it never competes with
	// fine-grained "running <tool>" / "drafting response" detail.
	heartbeat := newCodexProgressHeartbeat(func(elapsed time.Duration) {
		l.updateHeadlessProgress(slug, "active", "thinking",
			fmt.Sprintf("still working (%ds)", int(elapsed.Seconds())), snapshotMetrics())
	})
	heartbeat.Start(startedAt)
	defer heartbeat.Stop()

	// Live-chat relay surfaces the model's user-facing text to the channel
	// at sentence/paragraph boundaries while the turn is still running.
	// Codex doesn't expose a separate `thinking` event type — its `text`
	// stream is the assistant's spoken output, which is exactly the
	// surface "items that concern the user and other agents" should land
	// on. Tool calls flush the buffer so a partial sentence doesn't get
	// stranded across tool invocations.
	target := firstNonEmpty(channel...)
	relay := newHeadlessLiveChatRelay(l, slug, target, notification, func(line string) {
		appendHeadlessCodexLog(slug, line)
	})
	// Defer the flush so error/parseErr exit paths still surface the
	// trailing buffered sentence. The explicit Flush before the final
	// post stays — once the buffer is empty, the deferred call is a
	// no-op. Without this, a turn that streams "checking the database…"
	// and then dies in cmd.Wait() loses that user-facing breadcrumb.
	defer relay.Flush()

	var firstEventAt time.Time
	var firstTextAt time.Time
	var firstToolAt time.Time
	textStarted := false
	turnID := newHeadlessTurnID()
	var turnToolNames []string
	var turnTextLen int
	result, parseErr := provider.ReadCodexJSONStream(teedStdout, func(event provider.CodexStreamEvent) {
		// Any parseable codex event resets the silence timer: while items
		// are flowing the fine-grained per-event detail below is the user's
		// progress signal, not the coarse heartbeat.
		heartbeat.Note()
		if firstEventAt.IsZero() {
			firstEventAt = time.Now()
			setMetric(func(m *headlessProgressMetrics) { m.FirstEventMs = durationMillis(startedAt, firstEventAt) })
		}
		switch event.Type {
		case "reasoning":
			// Codex emits reasoning/thinking items between tool calls; map
			// them to the same "planning next step" surface the Claude
			// runner uses so a long reasoning stretch is not a dark interval.
			l.updateHeadlessProgress(slug, "active", "thinking", "planning next step", snapshotMetrics())
		case "text":
			if firstTextAt.IsZero() && strings.TrimSpace(event.Text) != "" {
				firstTextAt = time.Now()
				setMetric(func(m *headlessProgressMetrics) { m.FirstTextMs = durationMillis(startedAt, firstTextAt) })
			}
			if !textStarted && strings.TrimSpace(event.Text) != "" {
				textStarted = true
				l.updateHeadlessProgress(slug, "active", "text", "drafting response", snapshotMetrics())
			}
			relay.OnText(event.Text)
			turnTextLen += len(event.Text)
			emitHeadlessText(agentStream, turnID, HeadlessProviderCodex, slug, taskID, event.Text, event.RawType)
		case "tool_use":
			relay.Flush()
			if firstToolAt.IsZero() {
				firstToolAt = time.Now()
				setMetric(func(m *headlessProgressMetrics) { m.FirstToolMs = durationMillis(startedAt, firstToolAt) })
			}
			line := fmt.Sprintf("tool_use: %s %s", event.ToolName, truncate(event.ToolInput, 120))
			appendHeadlessCodexLog(slug, line)
			heartbeat.Note() // suppress the coarse heartbeat: a real tool event just landed.
			l.updateHeadlessProgress(slug, "active", "tool_use", codexToolProgressDetail(event.ToolName), snapshotMetrics())
			turnToolNames = append(turnToolNames, event.ToolName)
			emitHeadlessToolUse(agentStream, turnID, HeadlessProviderCodex, slug, taskID, event.ToolName, event.ToolInput, event.RawType)
		case "tool_result":
			line := "tool_result: " + truncate(event.Text, 140)
			appendHeadlessCodexLog(slug, line)
			l.updateHeadlessProgress(slug, "active", "tool_result", truncate(event.Text, 140), snapshotMetrics())
			emitHeadlessToolResult(agentStream, turnID, HeadlessProviderCodex, slug, taskID, event.ToolName, event.Text, event.RawType)
		case "error":
			appendHeadlessCodexLog(slug, "stream_error: "+event.Detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(event.Detail, 180), snapshotMetrics())
		}
	})
	_ = pw.Close() // signal scanner goroutine that stream is done (io.PipeWriter.Close always returns nil)
	if err := cmd.Wait(); err != nil {
		detail := firstNonEmpty(result.LastError, strings.TrimSpace(stderr.String()))
		setMetric(func(m *headlessProgressMetrics) { m.TotalMs = time.Since(startedAt).Milliseconds() })
		metricsSnap := snapshotMetrics()
		if detail != "" {
			appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
				metricsSnap.TotalMs,
				durationMillis(startedAt, firstEventAt),
				durationMillis(startedAt, firstTextAt),
				durationMillis(startedAt, firstToolAt),
				detail,
			))
			appendHeadlessCodexLog(slug, "stderr: "+detail)
			l.updateHeadlessProgress(slug, "error", "error", truncate(detail, 180), metricsSnap)
			emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderCodex, slug, taskID, "", detail, metricsSnap, codexUsageToTokenUsage(result.Usage))
			emitHeadlessManifest(agentStream, turnID, HeadlessProviderCodex, slug, taskID, detail, turnToolNames, turnTextLen, metricsSnap, codexUsageToTokenUsage(result.Usage))
			if isCodexAuthError(detail) && l.broker != nil {
				sysTarget := target
				if strings.TrimSpace(sysTarget) == "" {
					sysTarget = "general"
				}
				l.broker.PostSystemMessage(sysTarget,
					fmt.Sprintf("@%s hit an auth error talking to the model (%s). Run `codex login` on this machine, or set OPENAI_API_KEY, then retry.", slug, truncate(detail, 180)),
					"error",
				)
			}
			return fmt.Errorf("%w: %s", err, detail)
		}
		appendHeadlessCodexLatency(slug, fmt.Sprintf("status=error total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d detail=%q",
			metricsSnap.TotalMs,
			durationMillis(startedAt, firstEventAt),
			durationMillis(startedAt, firstTextAt),
			durationMillis(startedAt, firstToolAt),
			err.Error(),
		))
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderCodex, slug, taskID, "", err.Error(), metricsSnap, codexUsageToTokenUsage(result.Usage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderCodex, slug, taskID, err.Error(), turnToolNames, turnTextLen, metricsSnap, codexUsageToTokenUsage(result.Usage))
		return err
	}
	if parseErr != nil {
		setMetric(func(m *headlessProgressMetrics) { m.TotalMs = time.Since(startedAt).Milliseconds() })
		metricsSnap := snapshotMetrics()
		l.updateHeadlessProgress(slug, "error", "error", truncate(parseErr.Error(), 180), metricsSnap)
		emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderCodex, slug, taskID, "", parseErr.Error(), metricsSnap, codexUsageToTokenUsage(result.Usage))
		emitHeadlessManifest(agentStream, turnID, HeadlessProviderCodex, slug, taskID, parseErr.Error(), turnToolNames, turnTextLen, metricsSnap, codexUsageToTokenUsage(result.Usage))
		return parseErr
	}
	setMetric(func(m *headlessProgressMetrics) { m.TotalMs = time.Since(startedAt).Milliseconds() })
	metricsSnap := snapshotMetrics()
	appendHeadlessCodexLatency(slug, fmt.Sprintf("status=ok total_ms=%d first_event_ms=%d first_text_ms=%d first_tool_ms=%d final_chars=%d",
		metricsSnap.TotalMs,
		durationMillis(startedAt, firstEventAt),
		durationMillis(startedAt, firstTextAt),
		durationMillis(startedAt, firstToolAt),
		len(strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine))),
	))
	summary := strings.TrimSpace(formatHeadlessLatencySummary(metricsSnap))
	if summary == "" {
		summary = "reply ready"
	} else {
		summary = "reply ready · " + summary
	}
	// Terminal turn event + manifest can fire now — they carry the turn's
	// own latency/usage, not the agent's live status. The status flip to
	// "idle" is deliberately deferred until AFTER the final gist+artifact
	// post below (see the trailing updateHeadlessProgress). The chat's
	// "agent is working" / skeleton indicator reads the OfficeMember
	// status; flipping to idle before the gist message lands would tear
	// down the skeleton a beat too early and the final message would pop in
	// against an already-idle agent. Keep status="active" through the post.
	emitHeadlessTerminalWithTurn(agentStream, turnID, HeadlessProviderCodex, slug, taskID, summary, "", metricsSnap, codexUsageToTokenUsage(result.Usage))
	emitHeadlessManifest(agentStream, turnID, HeadlessProviderCodex, slug, taskID, "", turnToolNames, turnTextLen, metricsSnap, codexUsageToTokenUsage(result.Usage))
	if l.broker != nil && (result.Usage.InputTokens != 0 || result.Usage.OutputTokens != 0 || result.Usage.CacheReadTokens != 0 || result.Usage.CacheCreationTokens != 0 || result.Usage.CostUSD != 0) {
		l.broker.RecordAgentUsage(slug, l.codexModelForAgent(ctx, slug), result.Usage)
	}
	relay.Flush()
	if text := strings.TrimSpace(firstNonEmpty(result.FinalMessage, result.LastPlainLine)); text != "" {
		appendHeadlessCodexLog(slug, "result: "+text)
		msg, posted, err := l.postHeadlessFinalMessageIfSilent(slug, target, notification, text, startedAt)
		if err != nil {
			appendHeadlessCodexLog(slug, "fallback-post-error: "+err.Error())
		} else if posted {
			appendHeadlessCodexLog(slug, fmt.Sprintf("fallback-post: posted final output to #%s as %s", msg.Channel, msg.ID))
		}
	}
	// Flip to idle last: the gist+artifact post above has now landed, so the
	// frontend's "agent is working" skeleton stays up until the final message
	// is in the channel, then resolves cleanly to idle.
	l.updateHeadlessProgress(slug, "idle", "idle", summary, metricsSnap)
	return nil
}

// codexUsageToTokenUsage adapts the Codex provider's ClaudeUsage record
// (yes — Codex shares the ClaudeUsage envelope) into the runner-agnostic
// shape HeadlessEvent expects.
func codexUsageToTokenUsage(u provider.ClaudeUsage) *headlessTokenUsage {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return &headlessTokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
}

func (l *Launcher) headlessCodexNeedsDangerousBypass(ctx context.Context, slug string) bool {
	if l == nil || l.broker == nil {
		return false
	}
	// Resolve THIS turn's task: with parallel instances an agent can run a
	// worktree turn and a chat/office turn at once, and only the worktree turn
	// should get the sandbox bypass.
	task := l.turnTaskForCtx(ctx, slug)
	if task == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree")
}

func (l *Launcher) buildHeadlessCodexEnv(slug string, workspaceDir string, channel string) []string {
	// gitexec.CleanEnv: codex agents run `git` subcommands inside their
	// sandbox. If wuphf inherited GIT_DIR / GIT_WORK_TREE /
	// GIT_CONFIG_PARAMETERS from a parent (git hook, nested wuphf call)
	// every child git would retarget the outer repo. Clean those first,
	// then drop codex-specific noise. stripEnvKeys is exact-match,
	// gitexec.CleanEnv is prefix-match — the GIT_CONFIG_KEY_<n> family
	// needs prefix-match, so we run gitexec.CleanEnv first and stripEnvKeys
	// second.
	env := stripEnvKeys(gitexec.CleanEnv(), headlessCodexEnvVarsToStrip)
	if workspaceDir = normalizeHeadlessWorkspaceDir(workspaceDir); workspaceDir != "" {
		env = setEnvValue(env, "PWD", workspaceDir)
	}
	if codexHome := prepareHeadlessCodexHome(); codexHome != "" {
		// Use the isolated runtime home for the headless Codex process so it
		// doesn't inherit user-global ~/.agents skills from the interactive shell.
		env = setEnvValue(env, "HOME", codexHome)
		_ = os.MkdirAll(filepath.Join(codexHome, "plugins", "cache"), 0o755)
		env = setEnvValue(env, "CODEX_HOME", codexHome)
	} else if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — HOME passthrough
		// to codex subprocess so tool resolution uses the real user home.
		env = setEnvValue(env, "HOME", home)
	}
	if base := l.headlessCodexWorkspaceCacheDir(workspaceDir); base != "" {
		goCache := filepath.Join(base, "go-build", strings.TrimSpace(slug))
		goTmp := filepath.Join(base, "go-tmp", strings.TrimSpace(slug))
		if config.EnsureCacheDir(base) == nil &&
			os.MkdirAll(goCache, 0o700) == nil &&
			os.MkdirAll(goTmp, 0o700) == nil {
			env = setEnvValue(env, "GOCACHE", goCache)
			env = setEnvValue(env, "GOTMPDIR", goTmp)
		}
	}
	env = setEnvValue(env, "WUPHF_AGENT_SLUG", slug)
	if channel = strings.TrimSpace(channel); channel != "" {
		env = setEnvValue(env, "WUPHF_CHANNEL", channel)
	}
	env = setEnvValue(env, "WUPHF_BROKER_TOKEN", l.broker.Token())
	env = setEnvValue(env, "WUPHF_BROKER_BASE_URL", l.BrokerBaseURL())
	env = setEnvValue(env, "WUPHF_HEADLESS_PROVIDER", "codex")
	if config.ResolveNoNex() {
		env = setEnvValue(env, "WUPHF_NO_NEX", "1")
	}
	if l.isOneOnOne() {
		env = setEnvValue(env, "WUPHF_ONE_ON_ONE", "1")
		env = setEnvValue(env, "WUPHF_ONE_ON_ONE_AGENT", l.oneOnOneAgent())
	}
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		env = setEnvValue(env, "ONE_SECRET", secret)
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env = setEnvValue(env, "ONE_IDENTITY", identity)
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env = setEnvValue(env, "ONE_IDENTITY_TYPE", identityType)
		}
	}
	if apiKey := strings.TrimSpace(config.ResolveAPIKey("")); apiKey != "" {
		env = setEnvValue(env, "WUPHF_API_KEY", apiKey)
		env = setEnvValue(env, "NEX_API_KEY", apiKey)
	}
	if openAIKey := strings.TrimSpace(config.ResolveOpenAIAPIKey()); openAIKey != "" {
		env = setEnvValue(env, "WUPHF_OPENAI_API_KEY", openAIKey)
		env = setEnvValue(env, "OPENAI_API_KEY", openAIKey)
	}
	return env
}

func headlessCodexHomeDir() string {
	if raw := strings.TrimSpace(os.Getenv("CODEX_HOME")); raw != "" {
		if abs, err := filepath.Abs(raw); err == nil && strings.TrimSpace(abs) != "" {
			return abs
		}
	}
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — ~/.codex is the
	// Codex auth credential directory, shared across all workspaces.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func headlessCodexGlobalHomeDir() string {
	if raw := strings.TrimSpace(os.Getenv("WUPHF_GLOBAL_HOME")); raw != "" {
		if abs, err := filepath.Abs(raw); err == nil && strings.TrimSpace(abs) != "" {
			return abs
		}
		return raw
	}
	// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — resolves the
	// user's real home for cross-workspace codex tool lookup.
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(home)
}

func headlessCodexRuntimeHomeDir() string {
	if home := config.RuntimeHomeDir(); home != "" {
		return filepath.Join(home, ".wuphf", "codex-headless")
	}
	return ""
}

func prepareHeadlessCodexHome() string {
	runtimeHome := normalizeHeadlessWorkspaceDir(headlessCodexRuntimeHomeDir())
	if runtimeHome == "" {
		return headlessCodexHomeDir()
	}
	if err := os.MkdirAll(runtimeHome, 0o755); err != nil {
		return headlessCodexHomeDir()
	}
	// Prefer an explicit CODEX_HOME (returned by headlessCodexHomeDir
	// when set) when its auth.json actually exists; otherwise fall
	// back to the default $HOME/.codex layout. Pre-fix the order was
	// reversed: $HOME/.codex always won, so a custom CODEX_HOME with
	// valid auth was never copied into the isolated runtime home,
	// and headless codex died with 401 even though the user had
	// logged in via the explicit override.
	sourceHome := ""
	if explicit := normalizeHeadlessWorkspaceDir(headlessCodexHomeDir()); explicit != "" {
		if _, err := os.Stat(filepath.Join(explicit, "auth.json")); err == nil {
			sourceHome = explicit
		}
	}
	if sourceHome == "" {
		sourceHome = normalizeHeadlessWorkspaceDir(filepath.Join(headlessCodexGlobalHomeDir(), ".codex"))
	}
	if sourceHome == "" {
		sourceHome = normalizeHeadlessWorkspaceDir(headlessCodexHomeDir())
	}
	if sourceHome != "" && sourceHome != runtimeHome {
		if err := copyHeadlessCodexHomeFile(sourceHome, runtimeHome, "auth.json", 0o600); err != nil {
			// Auth is load-bearing — without it codex dies with 401 after a 5s
			// reconnect dance and nothing surfaces to the user. Log loudly.
			// runHeadlessCodexTurn does the user-visible preflight; this log is
			// the trail we want when debugging why that preflight fired.
			appendHeadlessCodexLog("_setup", fmt.Sprintf(
				"auth-copy-failed: source=%s dest=%s err=%v (run `codex login` or set OPENAI_API_KEY)",
				filepath.Join(sourceHome, "auth.json"),
				filepath.Join(runtimeHome, "auth.json"),
				err,
			))
		}
	}
	if userHome := strings.TrimSpace(headlessCodexGlobalHomeDir()); userHome != "" {
		// Best-effort — these are optional overlays, silent on miss is fine.
		_ = copyHeadlessCodexHomeFile(userHome, runtimeHome, filepath.Join(".one", "config.json"), 0o600)
		_ = copyHeadlessCodexHomeFile(userHome, runtimeHome, filepath.Join(".one", "update-check.json"), 0o600)
	}
	return runtimeHome
}

// copyHeadlessCodexHomeFile copies rel from sourceHome into runtimeHome. Returns
// an error when the source exists but the copy failed, or when the source is
// missing. A wholly-empty path or rel is a no-op (nil). Callers that care about
// visibility (auth.json) check the error; best-effort overlays ignore it.
func copyHeadlessCodexHomeFile(sourceHome string, runtimeHome string, rel string, mode os.FileMode) error {
	if strings.TrimSpace(sourceHome) == "" || strings.TrimSpace(runtimeHome) == "" || strings.TrimSpace(rel) == "" {
		return nil
	}
	sourcePath := filepath.Join(sourceHome, filepath.FromSlash(rel))
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}
	destPath := filepath.Join(runtimeHome, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
	}
	if err := os.WriteFile(destPath, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", destPath, err)
	}
	return nil
}

func normalizeHeadlessWorkspaceDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil && strings.TrimSpace(abs) != "" {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(real) != "" {
		path = real
	}
	return path
}

func (l *Launcher) headlessCodexWorkspaceCacheDir(workspaceDir string) string {
	// No fallback to l.cwd / process cwd (V3-N5): the resolved turn
	// workspace is never empty on the headless path, and a cache dir must
	// never be minted inside the host launch directory. Empty in → empty
	// out; the caller simply skips the GOCACHE/GOTMPDIR overrides.
	base := strings.TrimSpace(workspaceDir)
	if base == "" {
		return ""
	}
	return filepath.Join(base, ".wuphf", "cache")
}

// headlessTaskWorkspaceDir resolves the git worktree a turn should run in.
// taskID is the running turn's task (from the turn record / ctx); it disambiguates
// when an agent has several in_progress tasks. An empty taskID falls back to the
// agent's first in_progress task for non-turn / single-task callers.
func (l *Launcher) headlessTaskWorkspaceDir(slug, taskID string) string {
	if l == nil || l.broker == nil {
		return ""
	}
	var task *teamTask
	if taskID = strings.TrimSpace(taskID); taskID != "" {
		task = l.broker.TaskByID(taskID)
	}
	if task == nil {
		task = l.agentActiveTask(slug)
	}
	if task == nil {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return ""
	}
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		return path
	}
	if strings.TrimSpace(task.ID) == "" {
		return ""
	}
	path, _, err := prepareTaskWorktree(task.ID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(path)
}

func (l *Launcher) buildCodexOfficeConfigOverrides(slug string) ([]string, error) {
	wuphfBinary, err := headlessCodexExecutablePath()
	if err != nil {
		return nil, err
	}
	wuphfEnvVars := []string{
		"WUPHF_AGENT_SLUG",
		"WUPHF_BROKER_TOKEN",
		"WUPHF_BROKER_BASE_URL",
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

// preflightHeadlessCodexAuth verifies codex has a way to authenticate before we
// spawn the process. Without this check, codex dies with a 401 after retrying
// for ~10s and WUPHF's error log reads "exit status 1: exit status 1" — totally
// undebuggable from the UI. We check the two valid auth paths: a copied
// auth.json in the isolated CODEX_HOME (ChatGPT plan or API-key file), or an
// OPENAI_API_KEY in the env we're about to hand codex. If neither, fail fast
// with a message the user will actually see in the channel.
func (l *Launcher) preflightHeadlessCodexAuth(slug string, channel string) error {
	// Check the source codex creds that WUPHF will later copy into the isolated
	// CODEX_HOME. If the source is missing AND there's no OPENAI_API_KEY to fall
	// back on, fail fast so the user sees a clear message rather than a silent
	// 5-second 401 loop.
	sourceHome := strings.TrimSpace(headlessCodexGlobalHomeDir())
	authPath := filepath.Join(sourceHome, ".codex", "auth.json")
	if sourceHome != "" {
		if _, err := os.Stat(authPath); err == nil {
			return nil
		}
	}
	// Also accept a previously-copied auth.json in the isolated runtime
	// home — codex would still work even if the source was since
	// removed. Probe the runtime home (where prepareHeadlessCodexHome
	// actually copies into); the original code probed
	// headlessCodexHomeDir() which is the source path that
	// prepareHeadlessCodexHome reads FROM, not where it writes — so
	// the check was effectively a no-op for the isolated copy.
	isolatedAuth := filepath.Join(headlessCodexRuntimeHomeDir(), "auth.json")
	if _, err := os.Stat(isolatedAuth); err == nil {
		return nil
	}
	// Explicit CODEX_HOME override: when the user sets CODEX_HOME to
	// point at a non-default codex home (e.g. ~/.codex-work), neither
	// the global probe (~/, source) nor the runtime probe
	// (~/.wuphf/codex-headless) would find auth there — but codex
	// itself will read it because it honors $CODEX_HOME. Probe that
	// path too so a user who runs `CODEX_HOME=~/.codex-work codex
	// login` doesn't see a spurious "cannot authenticate" failure.
	if explicitHome := strings.TrimSpace(headlessCodexHomeDir()); explicitHome != "" {
		explicitAuth := filepath.Join(explicitHome, "auth.json")
		if _, err := os.Stat(explicitAuth); err == nil {
			return nil
		}
	}
	if strings.TrimSpace(config.ResolveOpenAIAPIKey()) != "" {
		return nil
	}
	reason := fmt.Sprintf(
		"Codex cannot authenticate — no `auth.json` at %s and no OPENAI_API_KEY in env. Run `codex login` on this machine, or set OPENAI_API_KEY, then retry.",
		authPath,
	)
	appendHeadlessCodexLog(slug, "preflight: "+reason)
	if l != nil && l.broker != nil {
		target := channel
		if strings.TrimSpace(target) == "" {
			target = "general"
		}
		l.broker.PostSystemMessage(target,
			fmt.Sprintf("@%s can't run: %s", slug, reason),
			"error",
		)
	}
	return fmt.Errorf("codex auth missing: %s", reason)
}

// dumpHeadlessCodexInvocation writes the exact codex argv + env + stdin to a
// shell script in $WUPHF_DEBUG_CODEX_DUMP when that env var is set. Off by
// default. Used to reproduce a failing turn outside WUPHF in a few seconds.
func dumpHeadlessCodexInvocation(slug, workspaceDir string, args []string, env []string, stdinText string) {
	dumpDir := strings.TrimSpace(os.Getenv("WUPHF_DEBUG_CODEX_DUMP"))
	if dumpDir == "" {
		return
	}
	if err := os.MkdirAll(dumpDir, 0o700); err != nil {
		return
	}
	ts := time.Now().Format("20060102-150405.000")
	stub := filepath.Join(dumpDir, fmt.Sprintf("codex-%s-%s", slug, ts))
	if err := os.WriteFile(stub+".stdin", []byte(stdinText), 0o600); err != nil {
		return
	}
	var sh strings.Builder
	sh.WriteString("#!/bin/bash\n")
	fmt.Fprintf(&sh, "# Reproduces the exact codex invocation WUPHF builds for agent=%s\n", slug)
	sh.WriteString("set -e\n")
	fmt.Fprintf(&sh, "cd %q\n", workspaceDir)
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			fmt.Fprintf(&sh, "export %s=%q\n", kv[:i], kv[i+1:])
		}
	}
	sh.WriteString("codex")
	for _, a := range args {
		fmt.Fprintf(&sh, " %q", a)
	}
	fmt.Fprintf(&sh, " < %q\n", stub+".stdin")
	if err := os.WriteFile(stub+".sh", []byte(sh.String()), 0o700); err != nil {
		return
	}
	appendHeadlessCodexLog(slug, "debug-dump: wrote "+stub+".sh")
}

// isCodexAuthError reports whether the failure detail looks like an auth issue
// (expired OAuth, bad key, missing bearer). Used to surface a clear message to
// the channel instead of the raw "exit status 1: exit status 1" noise.
func isCodexAuthError(detail string) bool {
	d := strings.ToLower(detail)
	if d == "" {
		return false
	}
	if strings.Contains(d, "401") {
		return true
	}
	if strings.Contains(d, "unauthorized") {
		return true
	}
	if strings.Contains(d, "missing bearer") {
		return true
	}
	return false
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

func setEnvValue(env []string, key string, value string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return env
	}
	prefix := key + "="
	filtered := env[:0]
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, prefix+value)
}

func stripEnvKeys(env []string, strip []string) []string {
	if len(strip) == 0 {
		return env
	}
	stripSet := make(map[string]struct{}, len(strip))
	for _, key := range strip {
		key = strings.TrimSpace(key)
		if key != "" {
			stripSet[key] = struct{}{}
		}
	}
	if len(stripSet) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, ok := stripSet[key]; ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// codexHeartbeatInterval is how often the coarse "still working (NNs)"
// progress update fires while the codex turn is silent (no parseable
// item.* events). A codex exec turn runs ~80-120s; 15s keeps the UI alive
// without flooding it.
const codexHeartbeatInterval = 15 * time.Second

// codexHeartbeatIntervalForTest lets tests shrink the heartbeat interval so
// they don't wait the full production 15s. Production code reads it via
// newCodexProgressHeartbeat; it defaults to codexHeartbeatInterval and is
// only overridden in _test.go.
var codexHeartbeatIntervalForTest = codexHeartbeatInterval

// codexProgressHeartbeat ticks a coarse progress update during stretches
// where the codex stream produces no parseable events. Every real event
// calls Note() to reset the timer, so the heartbeat fires only during
// genuine silence. This is the point-4 fallback: even if a particular
// codex build emits nothing incremental for a long reasoning stretch, the
// user still sees the turn is alive rather than a frozen badge.
type codexProgressHeartbeat struct {
	tick     func(elapsed time.Duration)
	interval time.Duration
	stop     chan struct{}
	note     chan struct{}
	stopOnce sync.Once
}

func newCodexProgressHeartbeat(tick func(elapsed time.Duration)) *codexProgressHeartbeat {
	return &codexProgressHeartbeat{
		tick:     tick,
		interval: codexHeartbeatIntervalForTest,
		stop:     make(chan struct{}),
		note:     make(chan struct{}, 1),
	}
}

// Start launches the heartbeat goroutine. startedAt anchors the elapsed
// duration reported to tick so the "(NNs)" reflects total turn time.
func (h *codexProgressHeartbeat) Start(startedAt time.Time) {
	if h == nil || h.tick == nil {
		return
	}
	interval := h.interval
	if interval <= 0 {
		interval = codexHeartbeatInterval
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-h.stop:
				return
			case <-h.note:
				// A real event landed — reset the silence window so the
				// coarse heartbeat never overwrites fine-grained detail.
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(interval)
			case <-timer.C:
				h.tick(time.Since(startedAt))
				timer.Reset(interval)
			}
		}
	}()
}

// Note signals that a real event was observed, resetting the silence
// window. Non-blocking: the buffered channel coalesces bursts of events
// into a single reset.
func (h *codexProgressHeartbeat) Note() {
	if h == nil {
		return
	}
	select {
	case h.note <- struct{}{}:
	default:
	}
}

// Stop terminates the heartbeat goroutine. Safe to call multiple times
// (the deferred Stop plus any explicit call).
func (h *codexProgressHeartbeat) Stop() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() { close(h.stop) })
}

// codexToolProgressDetail builds the human-facing progress detail for a
// codex tool call. Visual-artifact tools get a "building visual artifact"
// phrasing so the user knows a figure is being drafted (D4's skeleton
// hooks off this); everything else reads "running <tool>".
func codexToolProgressDetail(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return "running tool"
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "visual_artifact"):
		return "drafting figure · building visual artifact"
	case strings.Contains(lower, "artifact"):
		return "building artifact"
	case strings.Contains(lower, "broadcast"):
		return "sharing update with the team"
	default:
		return fmt.Sprintf("running %s", name)
	}
}

// codexModelForAgent returns the codex model the next dispatch should use
// for slug. Per-agent ProviderBinding.Model wins when set and the binding
// kind is codex; otherwise we fall back to the install-wide codex config
// resolver (--model flag, $CODEX_MODEL env var, ~/.codex/config.toml).
//
// The kind check prevents a stale gpt-4o written when the user briefly
// switched the agent to codex from being fed to codex on a later
// switch-back if the per-agent binding wasn't fully cleared. In practice
// the AgentProfilePanel save flow keeps Model and Kind aligned, but
// belt-and-suspenders matches how headlessClaudeModel reads its binding.
func (l *Launcher) codexModelForAgent(ctx context.Context, slug string) string {
	// Per-task model wins over the agent binding (the model lives on the task,
	// not the agent). Only when the task's provider is codex.
	if model := l.taskModelForKind(ctx, slug, provider.KindCodex); model != "" {
		return model
	}
	if l != nil && l.broker != nil {
		binding := l.broker.MemberProviderBinding(slug)
		if binding.Kind == "codex" {
			if model := strings.TrimSpace(binding.Model); model != "" {
				return model
			}
		}
	}
	return strings.TrimSpace(config.ResolveCodexModel(l.cwd))
}
