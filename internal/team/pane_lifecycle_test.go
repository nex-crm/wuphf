package team

// Tests for the pane-lifecycle pure helpers extracted in C5a. The shell-out
// methods (spawnVisibleAgents, trySpawnWebAgentPanes, watchChannelPaneLoop,
// etc.) stay on Launcher pending the tmuxRunner interface introduced in C5b
// per PLAN.md §C5; this PR's coverage focuses on the helpers that don't
// need a tmux fake.

import (
	"strings"
	"testing"
)

func TestParseAgentPaneIndices_SkipsZeroAndChannelPanes(t *testing.T) {
	// pane 0 is the channel/observer; pane 1 is the lead, pane 2/3 are
	// specialists. The "channel" string in the title also marks panes to
	// skip — the launcher relies on this distinction when listing agent
	// panes for capture/dispatch.
	out := "0 channel\n1 ceo\n2 fe\n3 channel-misc"
	got := parseAgentPaneIndices(out)
	want := []int{1, 2}
	if len(got) != len(want) {
		t.Fatalf("parseAgentPaneIndices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseAgentPaneIndices[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseAgentPaneIndices_ToleratesEmptyAndMalformed(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"\n\n", 0},
		{"abc def\n", 0},      // non-numeric index
		{"42", 1},             // single index, no title
		{" 7 ceo \n8 fe ", 2}, // whitespace and trailing newline
	}
	for _, tc := range cases {
		got := parseAgentPaneIndices(tc.in)
		if len(got) != tc.want {
			t.Errorf("parseAgentPaneIndices(%q) returned %d entries, want %d", tc.in, len(got), tc.want)
		}
	}
}

func TestIsMissingTmuxSession_RecognizesCommonOutputs(t *testing.T) {
	for _, s := range []string{
		"no server running on /tmp/tmux-501/wuphf",
		"can't find session: wuphf-team",
		"failed to connect to server",
		"error connecting to /tmp/tmux-501/default",
		"open /private/tmp/foo: no such file or directory",
		"  NO SERVER  ",
	} {
		if !isMissingTmuxSession(s) {
			t.Errorf("isMissingTmuxSession(%q) = false, want true", s)
		}
	}
}

func TestIsMissingTmuxSession_RejectsUnrelatedOutput(t *testing.T) {
	for _, s := range []string{
		"",
		"some other error",
		"permission denied",
		"   ",
	} {
		if isMissingTmuxSession(s) {
			t.Errorf("isMissingTmuxSession(%q) = true, want false", s)
		}
	}
}

func TestIsNoSessionError_TrueOnFamiliarMessages(t *testing.T) {
	for _, s := range []string{"can't find session", "no server running"} {
		if !isNoSessionError(s) {
			t.Errorf("isNoSessionError(%q) = false, want true", s)
		}
	}
	if isNoSessionError("operation timed out") {
		t.Errorf("isNoSessionError unrelated message returned true")
	}
}

func TestChannelPaneNeedsRespawn_TrueWhenFirstFieldIsOne(t *testing.T) {
	if !channelPaneNeedsRespawn("1 ") {
		t.Errorf("status starting with 1 should signal respawn-needed")
	}
	if channelPaneNeedsRespawn("0 alive") {
		t.Errorf("status starting with 0 should not signal respawn")
	}
	if channelPaneNeedsRespawn("") {
		t.Errorf("empty status should not signal respawn")
	}
	if channelPaneNeedsRespawn("   ") {
		t.Errorf("whitespace-only should not signal respawn")
	}
}

func TestShouldPrimeClaudePane_RecognizesStartupPrompts(t *testing.T) {
	for _, s := range []string{
		"Trust this folder?",
		"See the security guide for more",
		"press Enter to confirm",
		"Welcome to Claude in Chrome",
	} {
		if !shouldPrimeClaudePane(s) {
			t.Errorf("shouldPrimeClaudePane(%q) = false, want true", s)
		}
	}
	if shouldPrimeClaudePane("regular pane content") {
		t.Errorf("regular content should not need priming")
	}
}

func TestPaneFallbackMessages_TmuxMissingDirectsToInstall(t *testing.T) {
	stderr, broker := paneFallbackMessages(false, "tmux not on PATH")
	if !strings.Contains(stderr, "tmux not found") {
		t.Errorf("stderr should mention tmux not found; got %q", stderr)
	}
	if !strings.Contains(stderr, "Install tmux") {
		t.Errorf("stderr should suggest installing tmux; got %q", stderr)
	}
	if !strings.Contains(broker, "headless mode") {
		t.Errorf("broker message should explain headless fallback; got %q", broker)
	}
}

func TestPaneFallbackMessages_TmuxInstalledFiledAsBug(t *testing.T) {
	stderr, broker := paneFallbackMessages(true, "tmux new-session failed")
	if !strings.Contains(stderr, "tmux new-session failed") {
		t.Errorf("stderr should include detail; got %q", stderr)
	}
	if strings.Contains(stderr, "Install tmux") {
		t.Errorf("install-suggestion should not appear when tmux is present; got %q", stderr)
	}
	if !strings.Contains(stderr, "file a bug") {
		t.Errorf("stderr should advise filing a bug when tmux is installed but rejected the spawn; got %q", stderr)
	}
	if !strings.Contains(broker, "headless mode") {
		t.Errorf("broker fallback note should still appear when tmux is installed; got %q", broker)
	}
	if !strings.Contains(broker, "rejected the pane-spawn command") {
		t.Errorf("broker should mention the rejected spawn; got %q", broker)
	}
}

func TestShellQuote_HandlesEmbeddedSingleQuotes(t *testing.T) {
	cases := map[string]string{
		"":        "''",
		"hello":   "'hello'",
		"it's me": "'it'\\''s me'",
		"a'b'c":   "'a'\\''b'\\''c'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChannelStderrLogPath_NonEmpty(t *testing.T) {
	got := channelStderrLogPath()
	if got == "" {
		t.Fatalf("channelStderrLogPath should never be empty")
	}
	if !strings.HasSuffix(got, "channel-stderr.log") && !strings.HasSuffix(got, ".wuphf-channel-stderr.log") {
		t.Errorf("channelStderrLogPath suffix unexpected: %q", got)
	}
}

func TestChannelPaneSnapshotPath_NonEmpty(t *testing.T) {
	got := channelPaneSnapshotPath()
	if got == "" {
		t.Fatalf("channelPaneSnapshotPath should never be empty")
	}
	if !strings.HasSuffix(got, "channel-pane.log") && !strings.HasSuffix(got, ".wuphf-channel-pane.log") {
		t.Errorf("channelPaneSnapshotPath suffix unexpected: %q", got)
	}
}
