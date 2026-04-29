package team

// Tests for the C5e ownership consolidation: the spawn orchestration
// methods (SpawnVisibleAgents, SpawnOverflowAgents, ReportPaneFallback,
// TrySpawnWebAgentPanes) now live on paneLifecycle and consult deps
// via the paneLifecycleDeps struct. These tests construct paneLifecycle
// directly with stubbed deps + fakeTmuxRunner, exercising the
// orchestration logic without the full Launcher.
//
// The simpler helper tests (one tmux call per assertion) live in
// pane_lifecycle_spawn_test.go; this file is for end-to-end paths
// where we want to assert on the *sequence* of tmux args.

import (
	"fmt"
	"testing"
)

func TestSpawnVisibleAgents_OneOnOneMode(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["split-window"] = []byte("ok")
	setTmuxRunnerForTest(t, fake)

	deps := paneLifecycleDeps{
		cwd:           "/repo",
		isOneOnOne:    func() bool { return true },
		oneOnOneAgent: func() string { return "ceo" },
		claudeCommand: func(slug, prompt string) (string, error) {
			return fmt.Sprintf("claude --slug=%s", slug), nil
		},
		buildPrompt:   func(slug string) string { return "<prompt:" + slug + ">" },
		agentName:     func(slug string) string { return "Boss" },
		recordFailure: func(slug, reason string) { t.Fatalf("unexpected recordFailure: slug=%s reason=%s", slug, reason) },
	}
	pl := newPaneLifecycleWithDeps("wuphf-team", deps)

	got, err := pl.SpawnVisibleAgents()
	if err != nil {
		t.Fatalf("SpawnVisibleAgents err = %v", err)
	}
	if len(got) != 1 || got[0] != "ceo" {
		t.Fatalf("returned slugs = %v, want [ceo]", got)
	}

	// Sequence pinning: 1 split-window, 1 select-layout, 2 select-pane (titles),
	// 1 select-window, 1 select-pane (focus). 6 total tmux calls.
	splits := fake.callsFor("split-window")
	if len(splits) != 1 || splits[0][1] != "-h" {
		t.Errorf("expected 1 split-window with -h, got %v", splits)
	}
	titles := fake.callsFor("select-pane")
	if len(titles) != 3 {
		// 2 SetPaneTitle (-T) + 1 FocusPane (no -T) = 3
		t.Errorf("expected 3 select-pane calls (2 titles + 1 focus), got %d: %v", len(titles), titles)
	}
	if len(fake.callsFor("select-layout")) != 1 {
		t.Errorf("expected 1 select-layout, got %v", fake.callsFor("select-layout"))
	}
	if len(fake.callsFor("select-window")) != 1 {
		t.Errorf("expected 1 select-window, got %v", fake.callsFor("select-window"))
	}
}

func TestSpawnVisibleAgents_FirstSplitFailureAborts(t *testing.T) {
	fake := newFakeTmuxRunner()
	fake.outputs["split-window"] = []byte("tmux: terminal too small\n")
	fake.errors["split-window"] = fmt.Errorf("exit 1")
	setTmuxRunnerForTest(t, fake)

	deps := paneLifecycleDeps{
		cwd:                  "/repo",
		isOneOnOne:           func() bool { return false },
		visibleOfficeMembers: func() []officeMember { return []officeMember{{Slug: "ceo"}, {Slug: "fe"}} },
		claudeCommand:        func(slug, prompt string) (string, error) { return "claude", nil },
		buildPrompt:          func(slug string) string { return "" },
		agentName:            func(slug string) string { return slug },
		recordFailure:        func(slug, reason string) {},
	}
	pl := newPaneLifecycleWithDeps("wuphf-team", deps)

	if _, err := pl.SpawnVisibleAgents(); err == nil {
		t.Fatalf("SpawnVisibleAgents err = nil, want error from first split failure")
	}
	// Only one split-window call should have been made: the failed first.
	// Subsequent agents are not attempted because the first split is mandatory.
	if got := len(fake.callsFor("split-window")); got != 1 {
		t.Errorf("split-window calls = %d, want 1 (first failed -> abort)", got)
	}
}

func TestSpawnVisibleAgents_AdditionalSplitFailureRecordsAndContinues(t *testing.T) {
	fake := newFakeTmuxRunner()
	// First split (the -h variant) succeeds; subsequent splits (no -h)
	// cannot be distinguished by sub-command alone — they all share
	// "split-window". Use a hook on the fake to fail only the second call.
	callIdx := 0
	fake.outputs["split-window"] = []byte("ok")
	originalRun := fake // unused; we extend via a wrapper instead
	_ = originalRun
	setTmuxRunnerForTest(t, &countingRunner{
		inner:            fake,
		failOnNthCommand: 2, // fail the second split-window
		failingSubcmd:    "split-window",
		callIdx:          &callIdx,
	})

	recorded := map[string]string{}
	deps := paneLifecycleDeps{
		cwd:                  "/repo",
		isOneOnOne:           func() bool { return false },
		visibleOfficeMembers: func() []officeMember { return []officeMember{{Slug: "ceo"}, {Slug: "fe"}, {Slug: "be"}} },
		claudeCommand:        func(slug, prompt string) (string, error) { return "claude --" + slug, nil },
		buildPrompt:          func(slug string) string { return "" },
		agentName:            func(slug string) string { return slug },
		recordFailure: func(slug, reason string) {
			recorded[slug] = reason
		},
	}
	pl := newPaneLifecycleWithDeps("wuphf-team", deps)

	got, err := pl.SpawnVisibleAgents()
	if err != nil {
		t.Fatalf("SpawnVisibleAgents err = %v, want nil (later split failures don't abort)", err)
	}
	// All three slugs are returned (the recorded failure doesn't remove them
	// from the visibleSlugs list — that's the targeter's job through
	// failedPaneSlugs).
	if len(got) != 3 {
		t.Errorf("returned slugs = %v, want 3 entries", got)
	}
	// "fe" is the second agent, which is the one we made fail.
	if _, ok := recorded["fe"]; !ok {
		t.Errorf("recordFailure was not called for fe; recorded = %v", recorded)
	}
}

func TestSpawnOverflowAgents_SkipsHeadlessOneShotMembers(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	deps := paneLifecycleDeps{
		cwd: "/repo",
		overflowOfficeMembers: func() []officeMember {
			return []officeMember{
				{Slug: "ceo-extra"}, // claude — should spawn
				{Slug: "codex-bot"}, // codex — should skip
				{Slug: "extra-eng"}, // claude — should spawn
			}
		},
		memberUsesHeadlessOneShotRuntime: func(slug string) bool {
			return slug == "codex-bot"
		},
		claudeCommand: func(slug, prompt string) (string, error) { return "claude", nil },
		buildPrompt:   func(slug string) string { return "" },
		recordFailure: func(slug, reason string) { t.Fatalf("unexpected failure for %s: %s", slug, reason) },
	}
	pl := newPaneLifecycleWithDeps("wuphf-team", deps)
	pl.SpawnOverflowAgents()

	news := fake.callsFor("new-window")
	if len(news) != 2 {
		t.Fatalf("new-window calls = %d, want 2 (codex-bot skipped)", len(news))
	}
	// First call should be ceo-extra; second extra-eng. The window name
	// is at args index 5 (-d -t session -n NAME -c cwd cmd).
	if news[0][5] != overflowWindowName("ceo-extra") {
		t.Errorf("new-window[0] window name = %q, want %q", news[0][5], overflowWindowName("ceo-extra"))
	}
	if news[1][5] != overflowWindowName("extra-eng") {
		t.Errorf("new-window[1] window name = %q, want %q", news[1][5], overflowWindowName("extra-eng"))
	}
}

func TestReportPaneFallback_BrokerlessIsStderrOnly(t *testing.T) {
	pl := newPaneLifecycleWithDeps("wuphf-team", paneLifecycleDeps{})
	// No postSystemMessage wired — should not panic, should not error.
	pl.ReportPaneFallback(false, "tmux missing", fmt.Errorf("not found"))
}

func TestReportPaneFallback_BrokerCallsPostSystemMessage(t *testing.T) {
	var posted []string
	pl := newPaneLifecycleWithDeps("wuphf-team", paneLifecycleDeps{
		postSystemMessage: func(channel, body, kind string) {
			posted = append(posted, channel+"|"+kind+"|"+body)
		},
	})
	pl.ReportPaneFallback(true, "spawn failed", fmt.Errorf("exit 1"))

	if len(posted) != 1 {
		t.Fatalf("posted = %v, want 1 entry", posted)
	}
	// The exact body comes from paneFallbackMessages; just spot-check the
	// channel / kind pinning.
	if want := "general|runtime|"; len(posted[0]) <= len(want) || posted[0][:len(want)] != want {
		t.Errorf("posted entry = %q, want prefix %q", posted[0], want)
	}
}

func TestTrySpawnWebAgentPanes_NilBrokerNoOp(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	pl := newPaneLifecycleWithDeps("wuphf-team", paneLifecycleDeps{
		// postSystemMessage nil simulates l.broker == nil.
		usesPaneRuntime: func() bool { return true },
	})
	pl.TrySpawnWebAgentPanes()

	if got := len(fake.calls); got != 0 {
		t.Errorf("expected zero tmux calls when broker is nil, got %d", got)
	}
}

func TestTrySpawnWebAgentPanes_HeadlessRuntimeNoOp(t *testing.T) {
	fake := newFakeTmuxRunner()
	setTmuxRunnerForTest(t, fake)

	posted := 0
	pl := newPaneLifecycleWithDeps("wuphf-team", paneLifecycleDeps{
		postSystemMessage: func(channel, body, kind string) { posted++ },
		usesPaneRuntime:   func() bool { return false },
	})
	pl.TrySpawnWebAgentPanes()

	if got := len(fake.calls); got != 0 {
		t.Errorf("expected zero tmux calls in headless runtime, got %d", got)
	}
	if posted != 0 {
		t.Errorf("expected zero broker posts when bailing on headless, got %d", posted)
	}
}

// countingRunner wraps a fakeTmuxRunner to inject a one-shot failure on
// the Nth call to a specific tmux subcommand. Used by the
// "additional split failure" test where we need success on the first
// split-window and failure on the second.
type countingRunner struct {
	inner            *fakeTmuxRunner
	failOnNthCommand int
	failingSubcmd    string
	callIdx          *int
}

func (c *countingRunner) shouldFail(args []string) bool {
	if len(args) == 0 || args[0] != c.failingSubcmd {
		return false
	}
	*c.callIdx++
	return *c.callIdx == c.failOnNthCommand
}

func (c *countingRunner) Run(args ...string) error {
	if c.shouldFail(args) {
		return fmt.Errorf("synthetic failure on %s call %d", c.failingSubcmd, c.failOnNthCommand)
	}
	return c.inner.Run(args...)
}

func (c *countingRunner) Output(args ...string) ([]byte, error) {
	if c.shouldFail(args) {
		return []byte("synthetic stderr"), fmt.Errorf("synthetic failure")
	}
	return c.inner.Output(args...)
}

func (c *countingRunner) Combined(args ...string) ([]byte, error) {
	if c.shouldFail(args) {
		return []byte("synthetic stderr"), fmt.Errorf("synthetic failure")
	}
	return c.inner.Combined(args...)
}
