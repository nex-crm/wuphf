package team

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	if l == nil {
		// Nil receiver is safe: runHeadlessCodexTurn checks l == nil and returns
		// "broker is not running" — preserves the prior fall-through behavior.
		return l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
	}
	// Per-task provider wins over the agent binding (then global default).
	kind := l.effectiveProviderKindForAgent(ctx, slug)
	var err error
	switch {
	case kind == provider.KindSlack:
		// Foreign Slack agents have no local runtime: their "turn" is the
		// outbound Slack relay (a real <@U…> mention), which the transport
		// dispatcher already delivered. Falling through to the Claude runner
		// would spawn a doomed subprocess for an agent that acts on its own in
		// Slack — so the queued turn is a deliberate no-op (and emits no
		// manifest, so there is nothing to detect).
		return nil
	case kind == provider.KindCodex:
		err = l.runHeadlessCodexTurn(ctx, slug, notification, channel...)
	case kind == provider.KindOpencode:
		err = l.runHeadlessOpencodeTurn(ctx, slug, notification, channel...)
	case isOpenAICompatKind(kind):
		err = l.runHeadlessOpenAICompatTurn(ctx, slug, notification, channel...)
	default:
		err = l.runHeadlessClaudeTurn(ctx, slug, notification, channel...)
	}
	// After a task-less (inline / chat) turn, run inline workflow→App detection:
	// the post-task hook only fires on a task reaching done, so work the CEO did
	// answering chat inline would otherwise never be seen. The turn's manifest is
	// already persisted (turn-scoped), so the corpus is current.
	l.maybeQueueInlineDetection(ctx, slug, channel...)
	return err
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
	headlessCodexTurnTimeout = headlessCodexTurnTimeoutEnv("WUPHF_TURN_TIMEOUT", 4*time.Minute)
	// headlessCodexOfficeTurnTimeout governs every office-mode turn that is
	// not a one-time launch. The CEO/leader orchestration turn (decompose a
	// request, spawn a specialist, create/assign tasks, post the synthesis)
	// is the most tool-heavy turn in the system: on a cold session it also
	// pays a ToolSearch tax rediscovering deferred MCP tools. Observed good
	// turns already run 2.5–3.4m, so the old 4m default (WUPHF_TURN_TIMEOUT)
	// force-killed legitimate orchestration mid-flight — the recovery path
	// then blocked the office task, leaving its work half-done. Give office
	// turns their own generous budget, operator-tunable via WUPHF_OFFICE_TIMEOUT.
	headlessCodexOfficeTurnTimeout        = headlessCodexTurnTimeoutEnv("WUPHF_OFFICE_TIMEOUT", 10*time.Minute)
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
	// FromHuman marks turns originating from a real person's chat message
	// (channel post or DM, tagged or not). Human-originated turns bypass
	// the lead queue-hold, the lead queue cap, the same-task dedup drop,
	// and the staleness/min-age preemption floors so the agent absorbs
	// the human input before resuming any prior agent-originated work.
	FromHuman bool
	// ContextUsed is the manifest of knowledge items the work packet
	// injected ("learning:<id>", "wiki:<ref>", "upstream:<task>",
	// "journal:<task>"). Recorded at packet-build time — deterministic,
	// not model-self-reported — and stamped onto the task ledger entry
	// when the turn settles (B4 context transparency).
	ContextUsed []string
}

type headlessCodexActiveTurn struct {
	Turn              headlessCodexTurn
	StartedAt         time.Time
	Timeout           time.Duration
	Cancel            context.CancelFunc
	WorkspaceDir      string
	WorkspaceSnapshot string
}

// headlessLane identifies one serialized dispatch lane. An agent used to have
// exactly one lane (its slug); parallel instances split an agent into several
// lanes so it can run more than one task at once. The key is the turn's
// resolved git worktree path (worktree tasks) or "task:"+id (office/external
// tasks), so two turns share a lane — and therefore serialize — exactly when
// they would write the same directory or are the same office task. Turns with
// no task (chat / channel triage) use the agent's default lane (key ""),
// preserving conversational coherence; this is true for the lead too, but a
// lead turn that DOES carry a task id now gets its own per-task lane (CEO
// multitasking — non-dependent tasks run concurrently). Keying on the workspace
// (not the task id) for worktree tasks makes the scheduler intrinsically
// collision-proof: distinct worktrees ⇒ distinct lanes ⇒ safe to run at once;
// a shared worktree (e.g. a dependency reusing its parent's tree) ⇒ same lane
// ⇒ serialized, regardless of how admission control routed the tasks.
type headlessLane struct {
	slug string
	key  string // resolved worktree path, or "" for the agent's default lane
}

// Lane constructors. Three shapes exist; routing them through named
// constructors keeps the empty-key default-lane intent and the "task:" key
// prefix in one place instead of scattered struct literals (laneForTurn).
func slugLane(slug string) headlessLane { return headlessLane{slug: slug} }

func worktreeLane(slug, worktreePath string) headlessLane {
	return headlessLane{slug: slug, key: strings.TrimSpace(worktreePath)}
}

func taskLane(slug, taskID string) headlessLane {
	return headlessLane{slug: slug, key: "task:" + taskID}
}

// Headless concurrency caps bound how many dispatch lanes may run a turn at
// once — the cost guard that keeps CEO multitasking (per-task lead lanes) from
// spawning an unbounded number of concurrent LLM subprocesses.
//
// Semantics: a pool cap field <= 0 means "unlimited" for that dimension. The
// zero value is therefore unlimited, so a bare &Launcher{} (and every existing
// concurrency test) is NOT retroactively throttled. Production resolves
// positive caps at boot via resolveHeadlessConcurrencyCaps; tests that exercise
// the cap set l.headless.maxConcurrent / maxConcurrentPerAgent directly.
const headlessCapGlobalClamp = 6 // upper clamp on the cores-derived global default

// resolveHeadlessConcurrencyCaps sets this launcher's caps from the environment
// with strong-opinion defaults (global ≈ clamp(cores, 2, 6); per-agent 3).
// Called once at launcher boot so production is always bounded.
func (l *Launcher) resolveHeadlessConcurrencyCaps() {
	cores := runtime.NumCPU()
	global := cores
	if global < 2 {
		global = 2 // allow some concurrency even on a single-core host
	}
	if global > headlessCapGlobalClamp {
		global = headlessCapGlobalClamp
	}
	// envIntDefault (notebook_signal_scanner.go) returns the env value as-is when
	// set, so an explicit WUPHF_MAX_CONCURRENT_TURNS=0 disables the cap (unlimited
	// — see headlessConcurrencyCaps); an unset value falls back to the default.
	l.headless.maxConcurrent = envIntDefault("WUPHF_MAX_CONCURRENT_TURNS", global)
	l.headless.maxConcurrentPerAgent = envIntDefault("WUPHF_MAX_CONCURRENT_PER_AGENT", 3)
}

// headlessConcurrencyCaps returns this launcher's effective (global, per-agent)
// caps. <= 0 means unlimited for that dimension.
func (l *Launcher) headlessConcurrencyCaps() (global, perAgent int) {
	return l.headless.maxConcurrent, l.headless.maxConcurrentPerAgent
}

// headlessLaneMayStartLocked reports whether starting a turn on lane would stay
// within the global and per-agent concurrency caps. Caller holds l.headless.mu.
// The lane being checked has no active turn at call time (the worker just
// finished its previous turn before looping back to begin), so counting active
// lanes never double-counts the candidate.
func (l *Launcher) headlessLaneMayStartLocked(lane headlessLane) bool {
	global, perAgent := l.headlessConcurrencyCaps()
	if global <= 0 && perAgent <= 0 {
		return true
	}
	total := 0
	sameAgent := 0
	for activeLane, active := range l.headless.active {
		if active == nil {
			continue
		}
		total++
		if activeLane.slug == lane.slug {
			sameAgent++
		}
	}
	if global > 0 && total >= global {
		return false
	}
	if perAgent > 0 && sameAgent >= perAgent {
		return false
	}
	return true
}

// headlessWorkerPool groups the per-launcher headless-dispatch state
// (PLAN.md §C7). All fields are lowercase package-internal — the pool
// is never used outside `internal/team` and stays an embedded value
// on Launcher rather than its own pointer so zero-value &Launcher{}
// in tests gets a usable pool with sane lazy-allocated maps. PR #320's
// goroutine-leak fix relies on stopCh being lazily allocated under mu
// before any worker can read it; that contract is preserved here.
//
// The maps are keyed by headlessLane (was: by slug). Slug-level coordination
// (the lead queue-hold/wake, activeHeadlessSlugs) iterates lanes and groups by
// lane.slug. deferredLead stays a single pointer because only no-task lead
// turns (channel triage) are ever deferred — those always run on the lead's one
// default lane; task-carrying lead turns run on per-task lanes and are not held.
//
// maxConcurrent / maxConcurrentPerAgent bound how many lanes may run a turn at
// once (the cost guard for CEO multitasking). 0 ⇒ use the env/default resolved
// by headlessConcurrencyCaps; a negative value ⇒ unlimited (tests). Production
// always resolves to a positive cap so concurrency can't blow up LLM spend.
type headlessWorkerPool struct {
	mu                    sync.Mutex
	ctx                   context.Context
	cancel                context.CancelFunc
	workers               map[headlessLane]bool
	active                map[headlessLane]*headlessCodexActiveTurn
	queues                map[headlessLane][]headlessCodexTurn
	deferredLead          *headlessCodexTurn
	stopCh                chan struct{}
	workerWg              sync.WaitGroup
	maxConcurrent         int
	maxConcurrentPerAgent int
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
