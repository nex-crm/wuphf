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
