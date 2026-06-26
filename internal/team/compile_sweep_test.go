package team

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestCompileSweep_TickInvokesCompile proves a tick calls the injected compile
// function. The compile func is a fake — zero LLM, zero git.
func TestCompileSweep_TickInvokesCompile(t *testing.T) {
	var calls atomic.Int64
	fired := make(chan struct{}, 1)
	sweep := newCompileSweep(func(context.Context) (CompileResult, error) {
		calls.Add(1)
		select {
		case fired <- struct{}{}:
		default:
		}
		return CompileResult{PagesWritten: 1}, nil
	}, minCompileInterval)
	// Override the clamped interval so the test doesn't wait a full minute.
	sweep.interval = 10 * time.Millisecond

	sweep.Start(context.Background())
	defer sweep.Stop(time.Second)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatalf("compile was never invoked by a tick")
	}
	if calls.Load() == 0 {
		t.Fatalf("expected at least one compile call")
	}
}

// TestCompileSweep_ErroringCompileDoesNotCrashLoop proves a tick error is
// swallowed and the loop keeps ticking (the next tick still fires).
func TestCompileSweep_ErroringCompileDoesNotCrashLoop(t *testing.T) {
	var calls atomic.Int64
	twice := make(chan struct{}, 1)
	sweep := newCompileSweep(func(context.Context) (CompileResult, error) {
		n := calls.Add(1)
		if n >= 2 {
			select {
			case twice <- struct{}{}:
			default:
			}
		}
		return CompileResult{}, errors.New("boom")
	}, minCompileInterval)
	sweep.interval = 10 * time.Millisecond

	sweep.Start(context.Background())
	defer sweep.Stop(time.Second)

	select {
	case <-twice:
	case <-time.After(2 * time.Second):
		t.Fatalf("loop did not survive an erroring compile (only %d call(s))", calls.Load())
	}
}

// TestCompileSweep_StartStopIdempotent proves Start/Stop are safe to call
// multiple times and that Stop before Start is a no-op.
func TestCompileSweep_StartStopIdempotent(t *testing.T) {
	sweep := newCompileSweep(func(context.Context) (CompileResult, error) {
		return CompileResult{}, nil
	}, time.Hour)

	// Stop before Start: no-op, must not panic or block.
	sweep.Stop(time.Second)

	sweep.Start(context.Background())
	sweep.Start(context.Background()) // second Start is a no-op
	sweep.Stop(time.Second)
	sweep.Stop(time.Second) // second Stop is a no-op
}

// TestCompileSweep_NilCompileTickIsNoop proves a sweep built without a worker
// (nil compile func) ticks harmlessly.
func TestCompileSweep_NilCompileTickIsNoop(t *testing.T) {
	sweep := newCompileSweep(nil, minCompileInterval)
	sweep.interval = 10 * time.Millisecond
	sweep.Start(context.Background())
	// Let a couple ticks elapse; a nil compile must not panic.
	time.Sleep(40 * time.Millisecond)
	sweep.Stop(time.Second)
}

// TestCompileSweep_StopOnContextCancel proves the loop exits when the context
// is cancelled even without an explicit Stop call.
func TestCompileSweep_StopOnContextCancel(t *testing.T) {
	sweep := newCompileSweep(func(context.Context) (CompileResult, error) {
		return CompileResult{}, nil
	}, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	sweep.Start(ctx)
	cancel()
	select {
	case <-sweep.done:
	case <-time.After(time.Second):
		t.Fatalf("loop did not exit on context cancel")
	}
}

func TestCompileSweepIntervalFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want time.Duration
	}{
		{name: "unset defaults", set: false, want: defaultCompileInterval},
		{name: "explicit", set: true, val: "30m", want: 30 * time.Minute},
		{name: "disabled zero", set: true, val: "0", want: 0},
		{name: "disabled word", set: true, val: "disabled", want: 0},
		{name: "clamped to min", set: true, val: "1s", want: minCompileInterval},
		{name: "invalid falls back", set: true, val: "not-a-duration", want: defaultCompileInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("WUPHF_COMPILE_INTERVAL", tc.val)
			} else {
				t.Setenv("WUPHF_COMPILE_INTERVAL", "")
			}
			if got := compileSweepIntervalFromEnv(); got != tc.want {
				t.Fatalf("interval = %s, want %s", got, tc.want)
			}
		})
	}
}
