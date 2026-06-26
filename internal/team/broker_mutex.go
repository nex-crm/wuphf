package team

// broker_mutex.go — lightweight contention visibility for the broker's
// single big lock (F1.d, docs/specs/ten-out-of-ten.md Wave F).
//
// The broker is a single-mutex design: b.mu guards all in-memory state and
// most mutations also persist the full state file while holding it. When one
// holder goes long (historically: git worktree subprocesses, full-state JSON
// writes), every other endpoint — wiki reads, the task board, task creation —
// queues silently behind it (ICP-eval v3 [19:44]/[19:50]: wiki "Still waiting
// on the broker", task create 4.5 min under load). This wrapper makes those
// waits observable without changing locking semantics: a Lock() that waits
// longer than slowLockWaitThreshold logs once (rate-limited) with the
// caller's file:line, so the long HOLDER can be found by reading what ran
// just before the slow WAITER.

import (
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// slowLockWaitThreshold is how long a b.mu acquisition may wait before the
// wait is logged. 1s is far above any healthy hold (in-memory mutation +
// state snapshot) and far below the observed wedges (minutes).
const slowLockWaitThreshold = time.Second

// slowLockLogMinInterval rate-limits slow-wait logging so a pile-up of
// blocked goroutines behind one long holder produces one line, not hundreds.
const slowLockLogMinInterval = 10 * time.Second

// slowLockLog is the log sink, swappable by tests.
var slowLockLog atomic.Pointer[func(format string, args ...any)]

func slowLockLogf(format string, args ...any) {
	if fn := slowLockLog.Load(); fn != nil {
		(*fn)(format, args...)
		return
	}
	log.Printf(format, args...)
}

// contendedMutex is a sync.Mutex that logs (sampled) when an acquisition
// waits longer than slowLockWaitThreshold. The uncontended fast path is a
// single TryLock — no clock reads, no allocation — so it costs nothing when
// the lock is healthy. It intentionally exposes the same Lock/Unlock surface
// as sync.Mutex so every existing b.mu call site compiles unchanged.
type contendedMutex struct {
	mu sync.Mutex
	// lastLoggedUnixNano is the time of the last slow-wait log line,
	// for rate limiting. Atomic: many waiters can cross the threshold
	// at once.
	lastLoggedUnixNano atomic.Int64
}

func (m *contendedMutex) Lock() {
	if m.mu.TryLock() {
		return
	}
	start := time.Now()
	m.mu.Lock()
	wait := time.Since(start)
	if wait < slowLockWaitThreshold {
		return
	}
	now := time.Now().UnixNano()
	last := m.lastLoggedUnixNano.Load()
	if now-last < int64(slowLockLogMinInterval) {
		return
	}
	if !m.lastLoggedUnixNano.CompareAndSwap(last, now) {
		return // another waiter just logged
	}
	caller := "unknown"
	if _, file, line, ok := runtime.Caller(1); ok {
		caller = trimLockCallerPath(file, line)
	}
	slowLockLogf("broker: lock acquire waited %s at %s — a long holder is wedging the broker (see the call that ran just before this)", wait.Round(time.Millisecond), caller)
}

func (m *contendedMutex) Unlock() {
	m.mu.Unlock()
}

// TryLock mirrors sync.Mutex.TryLock for call sites that probe the lock.
func (m *contendedMutex) TryLock() bool {
	return m.mu.TryLock()
}

// trimLockCallerPath shortens an absolute caller path to its last two
// segments ("team/broker_tasks_http.go:42") so log lines stay readable
// regardless of build machine layout.
func trimLockCallerPath(file string, line int) string {
	short := file
	slashes := 0
	for i := len(file) - 1; i >= 0; i-- {
		if file[i] == '/' {
			slashes++
			if slashes == 2 {
				short = file[i+1:]
				break
			}
		}
	}
	return short + ":" + strconv.Itoa(line)
}
