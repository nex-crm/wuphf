package team

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

var (
	headlessCodexLookPath       = exec.LookPath
	headlessCodexCommandContext = exec.CommandContext
	headlessCodexExecutablePath = os.Executable

	// headlessCodexRunTurnOverride lets tests intercept turn execution
	// without racing with goroutines that the queue worker spawned before
	// the test's deferred restore ran. Tests must use
	// setHeadlessCodexRunTurnForTest(t, fn) — never assign directly.
	//
	// Production callers go through headlessCodexRunTurn(...) which reads
	// the atomic and falls back to defaultHeadlessCodexRunTurn.
	headlessCodexRunTurnOverride atomic.Pointer[func(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error]
	// headlessWakeLeadFn is nil in production; override in tests to intercept
	// lead wake-ups. Always access via headlessWakeLeadFnMu to avoid races
	// with leaked goroutines from concurrent tests.
	headlessWakeLeadFn   func(l *Launcher, specialistSlug string)
	headlessWakeLeadFnMu sync.RWMutex
)

// defaultHeadlessCodexRunTurn is the production implementation of
// headlessCodexRunTurn. Routes by provider kind to the codex/opencode/claude
// turn runner. Tests substitute via setHeadlessCodexRunTurnForTest.
func defaultHeadlessCodexRunTurn(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error {
	if l != nil {
		kind := l.targeter().MemberEffectiveProviderKind(slug)
		switch {
		case kind == provider.KindCodex:
			return l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
		case kind == provider.KindOpencode:
			return l.runHeadlessOpencodeTurn(ctx, slug, notification, channel...)
		case isOpenAICompatKind(kind):
			return l.runHeadlessOpenAICompatTurn(ctx, slug, notification, channel...)
		default:
			return l.runHeadlessClaudeTurn(ctx, slug, notification, channel...)
		}
	}
	return l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
}

// headlessCodexRunTurn dispatches a queued turn to whichever runner the
// member's effective provider kind picks. Reads the test override via
// atomic.Pointer.Load so a worker goroutine that spawned before a test's
// override-restore cleanup ran cannot race against the assignment.
func headlessCodexRunTurn(l *Launcher, ctx context.Context, slug, notification string, channel ...string) error {
	if p := headlessCodexRunTurnOverride.Load(); p != nil {
		return (*p)(l, ctx, slug, notification, channel...)
	}
	return defaultHeadlessCodexRunTurn(l, ctx, slug, notification, channel...)
}

// headlessCodexTurnTimeoutEnv reads a duration from env, falling back to the
// supplied default when the var is unset, empty, non-positive, or unparseable.
// Accepts any input time.ParseDuration accepts (e.g. "6m", "90s", "1h").
//
// The defaults below are intentionally tight (4m / 10m / 12m); operators
// running slower providers (OpenRouter pooled queues, Kimi via Venice) or
// tool-heavy turns may need to extend them. See
// https://github.com/nex-crm/wuphf/issues/313.
func headlessCodexTurnTimeoutEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

var (
	headlessCodexTurnTimeout              = headlessCodexTurnTimeoutEnv("WUPHF_TURN_TIMEOUT", 4*time.Minute)
	headlessCodexOfficeLaunchTurnTimeout  = headlessCodexTurnTimeoutEnv("WUPHF_OFFICE_LAUNCH_TIMEOUT", 10*time.Minute)
	headlessCodexLocalWorktreeTurnTimeout = headlessCodexTurnTimeoutEnv("WUPHF_WORKTREE_TIMEOUT", 12*time.Minute)
	headlessCodexStaleCancelAfter         = 90 * time.Second
	// Minimum age an active turn must have before an enqueue can preempt
	// it via stale-cancel. Floors out tight re-enqueue loops where two
	// near-simultaneous enqueues would otherwise cancel each other on
	// arrival. Seen in prod (April 2026): CEO codex queue logged dozens
	// of `stale-turn: cancelling active turn after 0s` over hours because
	// the enqueue path exhausted the 90s threshold via clock-skew or a
	// malformed turn, causing back-to-back cancels that never produced
	// real work. 2s is long enough to absorb any legitimate rapid-fire
	// wake without blocking genuine preemption.
	headlessCodexMinTurnAgeBeforeCancel = 2 * time.Second
	headlessCodexEnvVarsToStrip         = []string{
		"OLDPWD",
		"PWD",
		"CODEX_THREAD_ID",
		"CODEX_TUI_RECORD_SESSION",
		"CODEX_TUI_SESSION_LOG_PATH",
	}
)

const headlessCodexLocalWorktreeRetryLimit = 2
const headlessCodexExternalActionRetryLimit = 1

type headlessCodexTurn struct {
	Prompt     string
	Channel    string // channel slug (e.g. "dm-ceo", "general")
	TaskID     string
	Attempts   int
	EnqueuedAt time.Time
}

type headlessCodexActiveTurn struct {
	Turn              headlessCodexTurn
	StartedAt         time.Time
	Timeout           time.Duration
	Cancel            context.CancelFunc
	WorkspaceDir      string
	WorkspaceSnapshot string
}

// headlessWorkerPool groups the per-launcher headless-dispatch state
// (PLAN.md §C7). All fields are lowercase package-internal — the pool
// is never used outside `internal/team` and stays an embedded value
// on Launcher rather than its own pointer so zero-value &Launcher{}
// in tests gets a usable pool with sane lazy-allocated maps. PR #320's
// goroutine-leak fix relies on stopCh being lazily allocated under mu
// before any worker can read it; that contract is preserved here.
type headlessWorkerPool struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	workers      map[string]bool
	active       map[string]*headlessCodexActiveTurn
	queues       map[string][]headlessCodexTurn
	deferredLead *headlessCodexTurn
	stopCh       chan struct{}
	workerWg     sync.WaitGroup
}

// headlessCodexWorkspaceStatusSnapshotFn is the seam type swapped by tests
// via setHeadlessCodexWorkspaceStatusSnapshotForTest. Kept as a named type so
// the atomic.Pointer below stays readable.
type headlessCodexWorkspaceStatusSnapshotFn func(path string) string

// headlessCodexWorkspaceStatusSnapshotOverride is read by the headless queue
// worker (see runHeadlessCodexQueue → headlessTurnCompletedDurably) and by
// recoverFailedHeadlessTurn — both run on goroutines that can outlive a
// test's t.Cleanup. Tests must never assign directly; use
// setHeadlessCodexWorkspaceStatusSnapshotForTest in test_support.go.
var headlessCodexWorkspaceStatusSnapshotOverride atomic.Pointer[headlessCodexWorkspaceStatusSnapshotFn]

func headlessCodexWorkspaceStatusSnapshot(path string) string {
	if p := headlessCodexWorkspaceStatusSnapshotOverride.Load(); p != nil {
		return (*p)(path)
	}
	return defaultHeadlessCodexWorkspaceStatusSnapshot(path)
}

func defaultHeadlessCodexWorkspaceStatusSnapshot(path string) string {
	path = normalizeHeadlessWorkspaceDir(path)
	if path == "" {
		return ""
	}
	out, err := runGitOutput(path, "status", "--porcelain=v1", "-z")
	if err != nil {
		return ""
	}
	return string(out)
}

func (l *Launcher) launchHeadlessCodex() error {
	killStaleBroker()
	killStaleHeadlessTaskRunners()
	_ = exec.CommandContext(context.Background(), "tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	l.broker = NewBroker()
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}
	if err := writeOfficePIDFile(); err != nil {
		return fmt.Errorf("write office pid: %w", err)
	}

	l.headless.ctx, l.headless.cancel = context.WithCancel(context.Background())

	l.resumeInFlightWork()
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.notifyOfficeChangesLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

func taskHasDurableCompletionState(task *teamTask) bool {
	if task == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	review := strings.ToLower(strings.TrimSpace(task.ReviewState))
	switch status {
	case "done", "completed", "blocked", "cancelled", "canceled", "review":
		return true
	}
	switch review {
	case "ready_for_review", "approved":
		return true
	}
	return false
}

func (l *Launcher) headlessTurnCompletedDurably(slug string, active *headlessCodexActiveTurn) (bool, string) {
	if l == nil || l.broker == nil || active == nil {
		return true, ""
	}
	task := l.timedOutTaskForTurn(slug, active.Turn)
	requiresDurableGuard := codingAgentSlugs[slug]
	requiresExternalExecution := taskRequiresRealExternalExecution(task)
	if task != nil && strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		requiresDurableGuard = true
	}
	if requiresExternalExecution {
		requiresDurableGuard = true
	}
	if !requiresDurableGuard {
		return true, ""
	}
	if task != nil && requiresExternalExecution {
		executed, attempted := l.taskHasExternalWorkflowEvidenceSince(task, active.StartedAt)
		if taskHasDurableCompletionState(task) {
			status := strings.ToLower(strings.TrimSpace(task.Status))
			switch status {
			case "done", "completed", "review":
				if executed {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s marked %s/%s without recorded external execution evidence", task.ID, strings.TrimSpace(task.Status), strings.TrimSpace(task.ReviewState))
			case "blocked", "cancelled", "canceled":
				if attempted {
					return true, ""
				}
				return false, fmt.Sprintf("external-action turn for #%s moved to %s without recorded external workflow evidence", task.ID, strings.TrimSpace(task.Status))
			default:
				if executed {
					return true, ""
				}
			}
		}
		if executed {
			return true, ""
		}
	}
	if task != nil && taskHasDurableCompletionState(task) {
		return true, ""
	}
	if l.agentPostedSubstantiveMessageSince(slug, active.StartedAt) {
		return true, ""
	}
	if workspaceDir := strings.TrimSpace(active.WorkspaceDir); workspaceDir != "" {
		current := headlessCodexWorkspaceStatusSnapshot(workspaceDir)
		if strings.TrimSpace(active.WorkspaceSnapshot) != "" && current != active.WorkspaceSnapshot {
			if task != nil {
				return false, fmt.Sprintf("coding turn for #%s changed workspace %s but left task %s/%s without durable completion evidence", task.ID, workspaceDir, strings.TrimSpace(task.Status), strings.TrimSpace(task.ReviewState))
			}
			return false, fmt.Sprintf("coding turn changed workspace %s without durable completion evidence", workspaceDir)
		}
	}
	if task != nil {
		if requiresExternalExecution {
			return false, fmt.Sprintf("external-action turn for #%s completed without durable task state or external workflow evidence", task.ID)
		}
		return false, fmt.Sprintf("coding turn for #%s completed without durable task state or completion evidence", task.ID)
	}
	if requiresExternalExecution {
		return false, fmt.Sprintf("external-action turn by @%s completed without durable task state or external workflow evidence", slug)
	}
	return false, fmt.Sprintf("coding turn by @%s completed without durable task state or completion evidence", slug)
}

func (l *Launcher) taskHasExternalWorkflowEvidenceSince(task *teamTask, startedAt time.Time) (executed bool, attempted bool) {
	if l == nil || l.broker == nil || task == nil {
		return false, false
	}
	channel := normalizeChannelSlug(task.Channel)
	owner := strings.TrimSpace(task.Owner)
	for _, action := range l.broker.Actions() {
		kind := strings.ToLower(strings.TrimSpace(action.Kind))
		switch kind {
		case "external_workflow_executed",
			"external_workflow_failed",
			"external_workflow_rate_limited",
			"external_action_executed",
			"external_action_failed":
		default:
			continue
		}
		if channel != "" && normalizeChannelSlug(action.Channel) != channel {
			continue
		}
		if owner != "" {
			actor := strings.TrimSpace(action.Actor)
			if actor != "" && actor != owner && actor != "scheduler" {
				continue
			}
		}
		when, err := time.Parse(time.RFC3339, strings.TrimSpace(action.CreatedAt))
		if err != nil {
			when, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(action.CreatedAt))
		}
		if err == nil && !when.Add(time.Second).After(startedAt) {
			continue
		}
		attempted = true
		if kind == "external_workflow_executed" || kind == "external_action_executed" {
			executed = true
		}
	}
	return executed, attempted
}

func headlessCodexTaskID(prompt string) string {
	prefixes := []string{"#task-", "#blank-slate-"}
	for _, prefix := range prefixes {
		idx := strings.Index(prompt, prefix)
		if idx == -1 {
			continue
		}
		start := idx + 1
		end := start
		for end < len(prompt) {
			ch := prompt[end]
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
				end++
				continue
			}
			break
		}
		return strings.TrimSpace(prompt[start:end])
	}
	return ""
}

func (l *Launcher) agentPostedSubstantiveMessageSince(slug string, startedAt time.Time) bool {
	if l == nil || l.broker == nil {
		return false
	}
	for _, msg := range l.broker.AllMessages() {
		if msg.From != slug {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(content, "[STATUS]") {
			continue
		}
		when, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			continue
		}
		if when.Add(time.Second).After(startedAt) {
			return true
		}
	}
	return false
}

func (l *Launcher) agentPostedSubstantiveMessageToChannelSince(slug string, targetChannel string, startedAt time.Time) bool {
	if l == nil || l.broker == nil {
		return false
	}
	targetChannel = normalizeChannelSlug(targetChannel)
	if IsDMSlug(targetChannel) {
		if targetAgent := DMTargetAgent(targetChannel); targetAgent != "" {
			targetChannel = DMSlugFor(targetAgent)
		}
	}
	for _, msg := range l.broker.AllMessages() {
		if msg.From != slug {
			continue
		}
		if targetChannel != "" && normalizeChannelSlug(msg.Channel) != targetChannel {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || strings.HasPrefix(content, "[STATUS]") {
			continue
		}
		when, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err != nil {
			continue
		}
		if when.Add(time.Second).After(startedAt) {
			return true
		}
	}
	return false
}

func (l *Launcher) postHeadlessFinalMessageIfSilent(slug string, targetChannel string, notification string, text string, startedAt time.Time) (channelMessage, bool, error) {
	if l == nil || l.broker == nil {
		return channelMessage{}, false, nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return channelMessage{}, false, nil
	}
	targetChannel = normalizeChannelSlug(targetChannel)
	if targetChannel == "" {
		targetChannel = "general"
	}
	if IsDMSlug(targetChannel) {
		if targetAgent := DMTargetAgent(targetChannel); targetAgent != "" {
			targetChannel = DMSlugFor(targetAgent)
		}
	}
	if l.agentPostedSubstantiveMessageToChannelSince(slug, targetChannel, startedAt) {
		return channelMessage{}, false, nil
	}
	msg, err := l.broker.PostMessage(slug, targetChannel, text, nil, headlessReplyToID(notification))
	if err != nil {
		return channelMessage{}, false, err
	}
	return msg, true, nil
}

func headlessReplyToID(notification string) string {
	const marker = `reply_to_id "`
	idx := strings.LastIndex(notification, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	end := strings.Index(notification[start:], `"`)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(notification[start : start+end])
}

func (l *Launcher) timedOutTaskForTurn(slug string, turn headlessCodexTurn) *teamTask {
	if l == nil || l.broker == nil {
		return nil
	}
	if id := strings.TrimSpace(turn.TaskID); id != "" {
		for _, task := range l.broker.AllTasks() {
			if task.ID == id {
				cp := task
				return &cp
			}
		}
	}
	return l.agentActiveTask(slug)
}

func (l *Launcher) shouldRetryTimedOutHeadlessTurn(task *teamTask, turn headlessCodexTurn) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	return turn.Attempts < headlessCodexLocalWorktreeRetryLimit
}

func headlessTimedOutRetryPrompt(slug string, prompt string, timeout time.Duration, attempt int, external bool) string {
	note := fmt.Sprintf("Previous attempt by @%s timed out after %s without a durable task handoff. Retry #%d.", strings.TrimSpace(slug), timeout, attempt)
	if external {
		note += " This is a live external-action task. Do the smallest useful live external step now. If Slack target discovery is already known, use it. If the first live Slack target fails, retry once against the resolved writable target; if that still fails, pivot immediately to the smallest useful live Notion or Drive action and report the exact blocker. Do not write repo docs or planning artifacts as substitutes."
	} else {
		note += " For this retry, move immediately from claim/status into targeted file reads and edits, then leave the task in review/done/blocked before you stop. If you cannot ship the whole slice, ship the smallest runnable sub-slice and mark that state explicitly."
	}
	if strings.TrimSpace(prompt) == "" {
		return note
	}
	return strings.TrimSpace(prompt) + "\n\n" + note
}

func headlessFailedRetryPrompt(slug string, prompt string, detail string, attempt int, external bool) string {
	note := fmt.Sprintf("Previous attempt by @%s failed before a durable task handoff. Retry #%d.", strings.TrimSpace(slug), attempt)
	if trimmed := strings.TrimSpace(detail); trimmed != "" {
		note += " Last error: " + truncate(trimmed, 180) + "."
	}
	if external {
		note += " This is a live external-action task. Do the smallest useful live external step now. Do not keep discovering or drafting repo substitutes. If the first live Slack target fails, retry once against the resolved writable target; if that still fails, pivot immediately to the smallest useful live Notion or Drive action and report the exact blocker."
	} else {
		note += " For this retry, move immediately from claim/status into targeted file reads and edits, then leave the task in review/done/blocked before you stop. If you cannot ship the whole slice, ship the smallest runnable sub-slice and mark that state explicitly."
	}
	if strings.TrimSpace(prompt) == "" {
		return note
	}
	return strings.TrimSpace(prompt) + "\n\n" + note
}

func shouldRetryHeadlessTurn(task *teamTask, turn headlessCodexTurn) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return turn.Attempts < headlessCodexLocalWorktreeRetryLimit
	}
	if taskRequiresRealExternalExecution(task) {
		return turn.Attempts < headlessCodexExternalActionRetryLimit
	}
	return false
}

func (l *Launcher) recoverTimedOutHeadlessTurn(slug string, turn headlessCodexTurn, startedAt time.Time, timeout time.Duration) {
	if l == nil || l.broker == nil {
		return
	}
	task := l.timedOutTaskForTurn(slug, turn)
	if task == nil || strings.TrimSpace(task.ID) == "" {
		appendHeadlessCodexLog(slug, "timeout-recovery: no matching task found to block")
		return
	}
	if l.timedOutTurnAlreadyRecovered(task, slug, startedAt) {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: %s already produced durable progress; leaving task state unchanged", task.ID))
		return
	}
	if shouldRetryHeadlessTurn(task, turn) {
		retryTurn := turn
		retryTurn.Attempts++
		retryTurn.EnqueuedAt = time.Now()
		retryTurn.Prompt = headlessTimedOutRetryPrompt(slug, turn.Prompt, timeout, retryTurn.Attempts, taskRequiresRealExternalExecution(task))
		limit := headlessCodexLocalWorktreeRetryLimit
		if taskRequiresRealExternalExecution(task) {
			limit = headlessCodexExternalActionRetryLimit
		}
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: requeueing %s after silent timeout (attempt %d/%d)", task.ID, retryTurn.Attempts, limit))
		l.enqueueHeadlessCodexTurnRecord(slug, retryTurn)
		return
	}
	reason := fmt.Sprintf("Automatic timeout recovery: @%s timed out after %s before posting a substantive update. Requeue, retry, or reassign from here.", slug, timeout)
	if _, changed, err := l.broker.BlockTask(task.ID, slug, reason); err != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery-error: could not block %s: %v", task.ID, err))
		return
	} else if changed {
		appendHeadlessCodexLog(slug, fmt.Sprintf("timeout-recovery: blocked %s after empty timeout", task.ID))
		_, _, _ = l.requestSelfHealing(slug, task.ID, agent.EscalationStuck, reason)
	}
}

// isDurabilityFailure reports whether detail came from headlessTurnCompletedDurably
// ("completed without durable task state"). These failures mean the agent ran but did
// nothing observable — retrying produces the same result, so we block instead.
func isDurabilityFailure(detail string) bool {
	return strings.Contains(strings.TrimSpace(detail), "completed without durable task state")
}

func (l *Launcher) recoverFailedHeadlessTurn(slug string, turn headlessCodexTurn, startedAt time.Time, detail string) {
	if l == nil || l.broker == nil {
		return
	}
	task := l.timedOutTaskForTurn(slug, turn)
	if task == nil || strings.TrimSpace(task.ID) == "" {
		appendHeadlessCodexLog(slug, "error-recovery: no matching task found to recover")
		return
	}
	if l.timedOutTurnAlreadyRecovered(task, slug, startedAt) {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: %s already produced durable progress; leaving task state unchanged", task.ID))
		return
	}
	if shouldRetryHeadlessTurn(task, turn) && !isDurabilityFailure(detail) {
		retryTurn := turn
		retryTurn.Attempts++
		retryTurn.EnqueuedAt = time.Now()
		retryTurn.Prompt = headlessFailedRetryPrompt(slug, turn.Prompt, detail, retryTurn.Attempts, taskRequiresRealExternalExecution(task))
		limit := headlessCodexLocalWorktreeRetryLimit
		if taskRequiresRealExternalExecution(task) {
			limit = headlessCodexExternalActionRetryLimit
		}
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: requeueing %s after failed turn (attempt %d/%d)", task.ID, retryTurn.Attempts, limit))
		l.enqueueHeadlessCodexTurnRecord(slug, retryTurn)
		return
	}
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" {
		trimmed = "unknown headless codex failure"
	}
	reason := fmt.Sprintf("Automatic error recovery: @%s failed before a durable task handoff. Last error: %s. Requeue, retry, or reassign from here.", slug, truncate(trimmed, 220))
	if _, changed, err := l.broker.BlockTask(task.ID, slug, reason); err != nil {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery-error: could not block %s: %v", task.ID, err))
		return
	} else if changed {
		appendHeadlessCodexLog(slug, fmt.Sprintf("error-recovery: blocked %s after failed turn", task.ID))
		_, _, _ = l.requestSelfHealing(slug, task.ID, agent.EscalationMaxRetries, reason)
	}
}

func (l *Launcher) timedOutTurnAlreadyRecovered(task *teamTask, slug string, startedAt time.Time) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		review := strings.ToLower(strings.TrimSpace(task.ReviewState))
		return status == "done" || status == "review" || status == "blocked" ||
			review == "ready_for_review" || review == "approved"
	}
	return l.agentPostedSubstantiveMessageSince(slug, startedAt)
}

// wuphfLogDirOverride is a test hook for redirecting headless log writes to
// an isolated path. Stored as atomic.Pointer so reads on the headless write
// path don't take a lock; nil in production. Tests set this via TestMain so
// log files don't pollute the user's real ~/.wuphf/logs while the suite
// runs. The previous WUPHF_LOG_DIR environment variable was retired in
// favour of this in-process hook — env vars leak into spawned codex/claude
// subprocesses, which is not what tests want.
var wuphfLogDirOverride atomic.Pointer[string]

func wuphfLogDir() string {
	if p := wuphfLogDirOverride.Load(); p != nil {
		override := strings.TrimSpace(*p)
		if override == "" {
			return ""
		}
		if err := os.MkdirAll(override, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "wuphf: log dir override %q unwritable: %v — headless logging disabled\n", override, err)
			return ""
		}
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".wuphf", "logs")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func appendHeadlessCodexLog(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-"+slug+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(line))
}

func appendHeadlessCodexLatency(slug string, line string) {
	dir := wuphfLogDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "headless-codex-latency.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] agent=%s %s\n", time.Now().Format(time.RFC3339), strings.TrimSpace(slug), strings.TrimSpace(line))
}

func durationMillis(start, mark time.Time) int64 {
	if start.IsZero() || mark.IsZero() {
		return -1
	}
	return mark.Sub(start).Milliseconds()
}
