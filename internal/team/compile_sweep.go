package team

// compile_sweep.go makes the wiki self-maintaining. A CompileSweep runs the
// full compile engine (wiki_compile.go) on a fixed cadence so captured sources
// flow into compiled articles without anyone clicking the manual Compile
// button (broker_compile.go) or asking Pam.
//
// The sweep is cheap when idle: the S4 compile engine is idempotent — a
// recompile with no source changes makes ZERO LLM calls (every extraction is
// reused from the state.json hash cache and every page input hash matches), so
// an idle tick is just a state-load + a few stat() calls.
//
// Lifecycle mirrors runActivityWatchdog / the wiki archive sweep loop: a ticker
// loop selecting on ctx.Done + a stopCh, with Start(ctx) spawning the goroutine
// and Stop(timeout) draining it. A tick error is logged, never fatal.
//
// First tick: the sweep waits one full interval before its first compile. Boot
// already runs the index reconcile and source-capture path; delaying the first
// sweep keeps cold start light and lets capture settle. Operators who want an
// immediate compile have the manual POST /wiki/compile trigger.

import (
	"context"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// defaultCompileInterval is the cadence when WUPHF_COMPILE_INTERVAL is
	// unset. Idempotency (S4) makes an idle sweep nearly free, so this can be
	// reasonably tight.
	defaultCompileInterval = 15 * time.Minute
	// minCompileInterval clamps the cadence so a typo like "1s" can't spin the
	// compile engine in a hot loop.
	minCompileInterval = 1 * time.Minute
)

// CompileSweep periodically runs a full wiki compile. The compile function is
// injected so production wires the real Compiler (real LLM at runtime) while
// tests inject a fake with zero LLM calls.
type CompileSweep struct {
	// compile runs one full recompile. nil means the worker wasn't ready at
	// construction; a tick then no-ops rather than panicking.
	compile  func(context.Context) (CompileResult, error)
	interval time.Duration

	running atomic.Bool
	stopCh  chan struct{}
	done    chan struct{}
}

// NewCompileSweep builds a sweep over the given repo + worker, wiring the real
// compile engine via NewCompiler with the headless Pam runner (real LLM at
// runtime). interval is clamped to minCompileInterval. If repo or worker is nil
// the sweep is inert (ticks no-op) so wiring before the worker is ready is
// safe.
func NewCompileSweep(repo *Repo, worker *WikiWorker, interval time.Duration) *CompileSweep {
	if interval < minCompileInterval {
		interval = minCompileInterval
	}
	var compile func(context.Context) (CompileResult, error)
	if repo != nil && worker != nil {
		compile = NewCompiler(repo, worker, HeadlessPamRunner{}).Compile
	}
	return newCompileSweep(compile, interval)
}

// newCompileSweep is the shared constructor. Tests use it directly to inject a
// fake compile function without touching git or the LLM.
func newCompileSweep(compile func(context.Context) (CompileResult, error), interval time.Duration) *CompileSweep {
	if interval < minCompileInterval {
		interval = minCompileInterval
	}
	return &CompileSweep{
		compile:  compile,
		interval: interval,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start launches the ticker goroutine. Idempotent: a second call while running
// is a no-op.
func (s *CompileSweep) Start(ctx context.Context) {
	if s == nil || s.running.Swap(true) {
		return
	}
	go s.run(ctx)
}

// Stop signals the goroutine to exit and waits up to timeout for it to drain.
// timeout <= 0 waits indefinitely. Idempotent: a call before Start, or a
// second Stop, is a no-op.
func (s *CompileSweep) Stop(timeout time.Duration) {
	if s == nil || !s.running.Swap(false) {
		return
	}
	close(s.stopCh)
	if timeout <= 0 {
		<-s.done
		return
	}
	select {
	case <-s.done:
	case <-time.After(timeout):
	}
}

func (s *CompileSweep) run(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick runs one compile. Errors are logged, never fatal — a transient compile
// failure must not kill the loop, so the next tick retries.
func (s *CompileSweep) tick(ctx context.Context) {
	if s.compile == nil {
		// Worker wasn't ready at construction; nothing to compile.
		return
	}
	result, err := s.compile(ctx)
	if err != nil {
		log.Printf("compile sweep: tick failed: %v", err)
		return
	}
	log.Printf("compile sweep: sources=%d concepts=%d pages_written=%d pages_skipped=%d pages_linked=%d citation_warnings=%d errors=%d",
		result.SourcesRead, result.Concepts, result.PagesWritten, result.PagesSkipped,
		result.PagesLinked, len(result.CitationWarnings), len(result.Errors))
	for _, e := range result.Errors {
		log.Printf("compile sweep: compile error: %s", e)
	}
}

// compileSweepIntervalFromEnv resolves the sweep cadence from
// WUPHF_COMPILE_INTERVAL. Unset → defaultCompileInterval. "0" or "disabled" →
// 0 (caller skips Start, sweep is off). Any value time.ParseDuration accepts is
// honored; an invalid or non-positive value falls back to the default. Values
// below minCompileInterval are clamped up.
func compileSweepIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WUPHF_COMPILE_INTERVAL"))
	if raw == "" {
		return defaultCompileInterval
	}
	if raw == "0" || raw == "disabled" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.Printf("compile sweep: invalid WUPHF_COMPILE_INTERVAL %q, using default %s", raw, defaultCompileInterval)
		return defaultCompileInterval
	}
	if d < minCompileInterval {
		return minCompileInterval
	}
	return d
}
