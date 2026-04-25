package team

// Characterization tests for broker state-path resolution. Intended as
// the floor that the "Track A" refactor (replacing the state-path
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
// Also covers: Stop()'s observable contract — stopCh is closed and the
// last-saved state file is byte-identical immediately afterward.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	// Snapshot path is always `<statePath>.last-good` — same directory,
	// same base name plus suffix. The load-side path (hit by
	// TestBrokerLoadsLastGoodSnapshotWhenPrimaryStateIsClobbered) relies
	// on this exact shape. If snapshot derivation ever drifts to a
	// different directory or format, recovery silently breaks.
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)

	got := b.stateSnapshotPath()
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
	// to true so NewBrokerAt() starts fresh and tests don't cross-
	// contaminate via a shared broker-state.json. Persistence-checking
	// tests opt back into disk load via reloadedBroker(t, b). Track A
	// must preserve this contract: a test-mode constructor must NOT
	// auto-load.
	statePath := leakedBrokerStatePath(t)

	// Seed disk with a distinctive message. If the gate is broken,
	// NewBrokerAt() will pick it up.
	seed := NewBrokerAt(statePath)
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
	gated := NewBrokerAt(statePath)
	if got := len(gated.Messages()); got != 0 {
		t.Fatalf("gate=true must yield 0 messages on construct; got %d", got)
	}

	// Gate OFF (production default): NewBrokerAt() reads from disk.
	oldGate := skipBrokerStateLoadOnConstruct
	skipBrokerStateLoadOnConstruct = false
	t.Cleanup(func() { skipBrokerStateLoadOnConstruct = oldGate })
	loaded := NewBrokerAt(statePath)
	msgs := loaded.Messages()
	if len(msgs) != 1 || msgs[0].ID != "seed-msg" {
		t.Fatalf("gate=false must auto-load seed; got %+v", msgs)
	}
}

func TestNewBrokerAt_PathSnapshottedAtConstruction(t *testing.T) {
	// NewBrokerAt binds the state path to the Broker at construction so a
	// later construction of a second broker at a different path can't
	// retarget the first broker's saves. Contract: after construction,
	// every save from this broker lands at b.statePath regardless of
	// what other brokers (constructed later, with other paths) are doing
	// in the same process.
	boundPath := filepath.Join(t.TempDir(), "bound-state.json")
	b := NewBrokerAt(boundPath)

	// Construct a second broker at a distinct path — simulates another
	// test running alongside this one. Its statePath must not bleed into
	// b's saves.
	unboundPath := filepath.Join(t.TempDir(), "should-not-be-written.json")
	_ = NewBrokerAt(unboundPath)

	b.mu.Lock()
	b.messages = []channelMessage{{
		ID:        "bound-msg",
		From:      "ceo",
		Content:   "belongs to the bound path",
		Timestamp: "2026-04-25T00:00:00Z",
	}}
	b.counter = 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	if _, err := os.Stat(boundPath); err != nil {
		t.Fatalf("bound path missing after save: %v", err)
	}
	if _, err := os.Stat(unboundPath); !os.IsNotExist(err) {
		t.Fatalf("unbound path was written: err=%v", err)
	}
	if b.stateSnapshotPath() != boundPath+".last-good" {
		t.Fatalf("snapshot method did not derive from bound path: got %q, want %q",
			b.stateSnapshotPath(), boundPath+".last-good")
	}
}

func TestNewBrokerAt_PanicsOnEmptyPath(t *testing.T) {
	// Empty statePath would silently degrade saveLocked into writing
	// `<empty>.tmp.<rand>` and `.last-good` into the process cwd. The
	// constructor must panic instead so the foot-gun surfaces at
	// construction time, not on the first save attempt half a second
	// later.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected NewBrokerAt(\"\") to panic; got nil")
		}
	}()
	_ = NewBrokerAt("")
}

func TestBrokerStop_ClosesStopChannelAndPreservesState(t *testing.T) {
	// Stop()'s observable contract: stopCh is closed (so any goroutine
	// selecting on it has been told to exit) and the on-disk state file
	// the broker last saved is byte-identical when Stop returns.
	//
	// This is an honest test of what Stop() actually guarantees today.
	// The previous incarnation slept 250ms and asserted no late writes,
	// which (a) violated this repo's no-sleeps-in-tests rule and (b)
	// would pass for the wrong reason if the offending goroutine was
	// quiescent during the window. A real "no late writes" check
	// requires a sync.WaitGroup on the goroutine set, which is broker
	// instrumentation worth doing separately if/when the goroutine
	// surface grows.
	//
	// Starts the broker via StartOnPort(0) so the HTTP listener
	// goroutine is actually present — without that, Stop is a near-noop
	// and the test wouldn't exercise the drain path at all.
	statePath := leakedBrokerStatePath(t)
	b := NewBrokerAt(statePath)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}

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

	beforeBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read before Stop: %v", err)
	}

	b.Stop()

	// Deterministic check: stopCh must be closed when Stop returns.
	// Any background goroutine that selects on it has observed the
	// signal — the read here cannot happen before Stop closes the
	// channel, since Stop is synchronous.
	select {
	case <-b.stopCh:
		// closed, as required
	default:
		t.Fatal("Stop returned but b.stopCh is not closed")
	}

	// State file is byte-identical immediately after Stop returns.
	// If a future regression makes Stop trigger a save (intentionally
	// or via a leaked goroutine that wins a race against Stop's
	// completion), this catches it without sleeping.
	afterBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read after Stop: %v", err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("state file content changed across Stop:\nbefore: %s\nafter: %s",
			beforeBytes, afterBytes)
	}
}
