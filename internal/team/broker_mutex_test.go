package team

// Tests for the broker-lock contention visibility wrapper (F1.d,
// broker_mutex.go): a slow acquisition logs once with the caller, the
// uncontended path logs nothing, and rate limiting collapses pile-ups.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func captureSlowLockLogs(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	lines := &[]string{}
	fn := func(format string, args ...any) {
		mu.Lock()
		*lines = append(*lines, fmt.Sprintf(format, args...))
		mu.Unlock()
	}
	prior := slowLockLog.Load()
	slowLockLog.Store(&fn)
	t.Cleanup(func() { slowLockLog.Store(prior) })
	return lines
}

func TestContendedMutexUncontendedAcquireDoesNotLog(t *testing.T) {
	lines := captureSlowLockLogs(t)
	var m contendedMutex
	guarded := 0
	for i := 0; i < 100; i++ {
		m.Lock()
		guarded++
		m.Unlock()
	}
	if guarded != 100 {
		t.Fatalf("expected 100 guarded increments, got %d", guarded)
	}
	if len(*lines) != 0 {
		t.Fatalf("uncontended acquires must not log, got %v", *lines)
	}
}

func TestContendedMutexLogsSlowAcquireWithCaller(t *testing.T) {
	if testing.Short() {
		t.Skip("holds a lock past the 1s slow-wait threshold")
	}
	lines := captureSlowLockLogs(t)
	var m contendedMutex
	m.Lock()
	released := make(chan struct{})
	timer := time.AfterFunc(slowLockWaitThreshold+200*time.Millisecond, func() {
		m.Unlock()
		close(released)
	})
	t.Cleanup(func() { timer.Stop() })
	m.Lock() // waits past the threshold
	guarded := true
	m.Unlock()
	<-released
	if !guarded {
		t.Fatal("unreachable: keeps the critical section non-empty for staticcheck")
	}

	if len(*lines) != 1 {
		t.Fatalf("expected exactly one slow-wait log line, got %d: %v", len(*lines), *lines)
	}
	line := (*lines)[0]
	if !strings.Contains(line, "lock acquire waited") {
		t.Fatalf("unexpected log line: %q", line)
	}
	if !strings.Contains(line, "broker_mutex_test.go") {
		t.Fatalf("log line must carry the caller, got %q", line)
	}

	// Rate limit: a second slow wait inside slowLockLogMinInterval stays
	// silent so a pile-up behind one long holder logs once, not per waiter.
	m.Lock()
	timer = time.AfterFunc(slowLockWaitThreshold+200*time.Millisecond, func() {
		m.Unlock()
	})
	m.Lock()
	guarded = false
	m.Unlock()
	if guarded {
		t.Fatal("unreachable: keeps the critical section non-empty for staticcheck")
	}
	if len(*lines) != 1 {
		t.Fatalf("expected rate-limited single line, got %d: %v", len(*lines), *lines)
	}
}
