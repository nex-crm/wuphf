package team

// Characterization tests for broker state-path resolution. Intended as
// the floor that the "Track A" refactor (replacing the brokerStatePath
// package-var with a Broker constructor argument) must not regress.
//
// The existing TestBrokerPersistsAndReloadsState + Loads… tests cover
// the happy save/reload path. These cover the three invariants that
// the refactor most easily breaks:
//
//  1. WUPHF_BROKER_STATE_PATH env override still wins over defaults.
//  2. .last-good snapshot path is always a sibling of the main path.
//  3. skipBrokerStateLoadOnConstruct still gates the auto-load hook.
//
// Also covers: Stop() must not continue writing the state file after
// returning. Catches a regression where a background goroutine would
// read a refactored statePath via a stale closure and race the next
// construction.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultBrokerStatePath_EnvOverrideWins(t *testing.T) {
	// WUPHF_BROKER_STATE_PATH takes precedence over WUPHF_RUNTIME_HOME
	// so probes and harnesses can pin just the broker file without
	// remapping $HOME (which breaks Keychain lookups on macOS for
	// bundled CLIs like Claude Code).
	override := filepath.Join(t.TempDir(), "explicit-broker-state.json")
	t.Setenv("WUPHF_BROKER_STATE_PATH", override)
	t.Setenv("WUPHF_RUNTIME_HOME", "/tmp/should-be-ignored")

	got := defaultBrokerStatePath()
	if got != override {
		t.Fatalf("env override ignored: got %q, want %q", got, override)
	}
}

func TestDefaultBrokerStatePath_RuntimeHomeFallback(t *testing.T) {
	// No env override, WUPHF_RUNTIME_HOME set → <home>/.wuphf/team/
	// broker-state.json. This is the happy path prod and tests both
	// take (tests via worktree_guard_test.go pinning WUPHF_RUNTIME_HOME
	// to a leaked tempdir).
	t.Setenv("WUPHF_BROKER_STATE_PATH", "")
	home := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", home)

	got := defaultBrokerStatePath()
	want := filepath.Join(home, ".wuphf", "team", "broker-state.json")
	if got != want {
		t.Fatalf("home fallback wrong: got %q, want %q", got, want)
	}
}

func TestDefaultBrokerStatePath_RelativeFallbackWhenHomeMissing(t *testing.T) {
	// Neither env var set: fall back to a repo-relative path. Hit when
	// config.RuntimeHomeDir() returns "" — e.g. a CI container without
	// HOME. Must still produce a deterministic writable path rather
	// than panicking.
	t.Setenv("WUPHF_BROKER_STATE_PATH", "")
	t.Setenv("WUPHF_RUNTIME_HOME", "")
	t.Setenv("HOME", "")

	got := defaultBrokerStatePath()
	want := filepath.Join(".wuphf", "team", "broker-state.json")
	if got != want {
		t.Fatalf("relative fallback wrong: got %q, want %q", got, want)
	}
	if filepath.IsAbs(got) {
		t.Fatalf("relative fallback must not be absolute: got %q", got)
	}
}

func TestBrokerStateSnapshotPathIsLastGoodSibling(t *testing.T) {
	// Snapshot path is always `<brokerStatePath>.last-good` — same
	// directory, same base name plus suffix. The load-side path (hit
	// by TestBrokerLoadsLastGoodSnapshotWhenPrimaryStateIsClobbered)
	// relies on this exact shape. Post-refactor, if snapshot
	// derivation drifts to a different directory or format, recovery
	// silently breaks.
	oldPathFn := brokerStatePath
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	brokerStatePath = func() string { return statePath }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	got := brokerStateSnapshotPath()
	want := statePath + ".last-good"
	if got != want {
		t.Fatalf("snapshot path drifted: got %q, want %q", got, want)
	}
	if filepath.Dir(got) != filepath.Dir(statePath) {
		t.Fatalf("snapshot must live next to state file; got dir %q want %q",
			filepath.Dir(got), filepath.Dir(statePath))
	}
	if !strings.HasSuffix(got, ".last-good") {
		t.Fatalf("snapshot suffix drifted: got %q", got)
	}
}

func TestNewBroker_SkipStateLoadGateRespected(t *testing.T) {
	// The TestMain in this package flips skipBrokerStateLoadOnConstruct
	// to true so NewBroker() starts fresh and tests don't cross-
	// contaminate via a shared broker-state.json. Persistence-checking
	// tests opt back into disk load via reloadedBroker(t). Track A
	// must preserve this contract: constructor argument or not, a
	// test-mode NewBroker() must NOT auto-load.
	statePath := leakedBrokerStatePath(t)
	oldPathFn := brokerStatePath
	brokerStatePath = func() string { return statePath }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	// Seed disk with a distinctive message. If the gate is broken,
	// NewBroker() will pick it up.
	seed := NewBroker()
	seed.mu.Lock()
	seed.messages = []channelMessage{{
		ID:        "seed-msg",
		From:      "ceo",
		Content:   "canary from the seed broker",
		Timestamp: "2026-04-25T00:00:00Z",
	}}
	seed.counter = 1
	if err := seed.saveLocked(); err != nil {
		seed.mu.Unlock()
		t.Fatalf("seed saveLocked: %v", err)
	}
	seed.mu.Unlock()

	// Gate ON (test default): fresh broker, no seed.
	if !skipBrokerStateLoadOnConstruct {
		t.Fatal("precondition: skipBrokerStateLoadOnConstruct should be true in tests")
	}
	gated := NewBroker()
	if got := len(gated.Messages()); got != 0 {
		t.Fatalf("gate=true must yield 0 messages on construct; got %d", got)
	}

	// Gate OFF (production default): NewBroker() reads from disk.
	oldGate := skipBrokerStateLoadOnConstruct
	skipBrokerStateLoadOnConstruct = false
	t.Cleanup(func() { skipBrokerStateLoadOnConstruct = oldGate })
	loaded := NewBroker()
	msgs := loaded.Messages()
	if len(msgs) != 1 || msgs[0].ID != "seed-msg" {
		t.Fatalf("gate=false must auto-load seed; got %+v", msgs)
	}
}

func TestBrokerStop_NoWriteAfterReturn(t *testing.T) {
	// Regression cover for the goroutine-drain gap: if Stop() returns
	// while a background goroutine is still holding a reference to the
	// state path and mid-save, the next construction (or the test
	// tempdir cleanup) can race the late write. Stop's contract must
	// be "no further state writes after I return."
	//
	// Implementation note: the activity watchdog is disabled in tests,
	// so the set of goroutines that could write post-Stop is small —
	// but this test is cheap and catches the class of regression a
	// Track A refactor most easily introduces (e.g. a goroutine that
	// reads a refactored `b.statePath` via a pointer captured at Start
	// time and continues writing after b.stopCh closes).
	statePath := leakedBrokerStatePath(t)
	oldPathFn := brokerStatePath
	brokerStatePath = func() string { return statePath }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{{
		ID:        "pre-stop",
		From:      "ceo",
		Content:   "written before Stop",
		Timestamp: "2026-04-25T00:00:00Z",
	}}
	b.counter = 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("pre-stop save: %v", err)
	}
	b.mu.Unlock()

	beforeStat, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat before Stop: %v", err)
	}

	b.Stop()

	// Give any straggler goroutine a realistic window to misbehave —
	// the watchdog ticker, if enabled, fires once per minute; shorter
	// loops like wiki-index reconcile use 100ms+ intervals. 250ms is
	// long enough to catch short-interval leaks without slowing the
	// test suite meaningfully.
	time.Sleep(250 * time.Millisecond)

	afterStat, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("stat after Stop: %v", err)
	}
	if !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Fatalf("state file modified after Stop returned: before=%s after=%s",
			beforeStat.ModTime(), afterStat.ModTime())
	}
	if afterStat.Size() != beforeStat.Size() {
		t.Fatalf("state file size changed after Stop: before=%d after=%d",
			beforeStat.Size(), afterStat.Size())
	}
}
