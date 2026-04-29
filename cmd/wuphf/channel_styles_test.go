package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Pure smoke tests: style constructors return non-zero, runtime-stable
// lipgloss.Style values. Catches accidental nil returns or panics in
// constructors that have no other test signal.

func styleProducesOutput(t *testing.T, name string, style lipgloss.Style, sample string) {
	t.Helper()
	rendered := style.Render(sample)
	if rendered == "" {
		t.Fatalf("%s.Render(%q) produced empty output", name, sample)
	}
}

func TestStylesProduceRenderableOutput(t *testing.T) {
	styleProducesOutput(t, "sidebarStyle", sidebarStyle(20, 5), "x")
	styleProducesOutput(t, "mainPanelStyle", mainPanelStyle(40, 10), "x")
	styleProducesOutput(t, "threadPanelStyle", threadPanelStyle(40, 10), "x")
	styleProducesOutput(t, "statusBarStyle", statusBarStyle(80), "x")
	styleProducesOutput(t, "channelHeaderStyle", channelHeaderStyle(80), "x")
	styleProducesOutput(t, "composerBorderStyle/blur", composerBorderStyle(40, false), "x")
	styleProducesOutput(t, "composerBorderStyle/focus", composerBorderStyle(40, true), "x")
	styleProducesOutput(t, "timestampStyle", timestampStyle(), "10:00")
	styleProducesOutput(t, "mutedTextStyle", mutedTextStyle(), "muted")
	styleProducesOutput(t, "agentNameStyle/known", agentNameStyle("ceo"), "CEO")
	styleProducesOutput(t, "agentNameStyle/unknown", agentNameStyle("does-not-exist"), "x")
	styleProducesOutput(t, "activeChannelStyle", activeChannelStyle(), "office")
	styleProducesOutput(t, "dateSeparatorStyle", dateSeparatorStyle(), "today")
	styleProducesOutput(t, "threadIndicatorStyle", threadIndicatorStyle(), "thread")
}

func TestAgentAvatarKnownAndUnknownSlugs(t *testing.T) {
	cases := map[string]string{
		"ceo":      "◆",
		"pm":       "▣",
		"fe":       "▤",
		"be":       "▥",
		"ai":       "◉",
		"designer": "◌",
		"cmo":      "✶",
		"cro":      "◈",
		"nex":      "◎",
		"you":      "●",
		"random":   "•",
		"":         "•",
	}
	for slug, want := range cases {
		if got := agentAvatar(slug); got != want {
			t.Errorf("agentAvatar(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestMascotAccentFallsBackForUnknown(t *testing.T) {
	if got := mascotAccent("ceo"); got != "⌐" {
		t.Errorf("ceo accent should be ⌐, got %q", got)
	}
	if got := mascotAccent("does-not-exist"); got != "•" {
		t.Errorf("unknown slug should fall back to •, got %q", got)
	}
}

func TestMascotEyesFallsBackForUnknown(t *testing.T) {
	l, r := mascotEyes("ceo")
	if l != "■" || r != "■" {
		t.Errorf("ceo eyes should be ■ ■, got %q %q", l, r)
	}
	l, r = mascotEyes("ai")
	if l != "◉" || r != "◉" {
		t.Errorf("ai eyes should be ◉ ◉, got %q %q", l, r)
	}
	l, r = mascotEyes("nobody")
	if l != "•" || r != "•" {
		t.Errorf("unknown slug eyes should be • •, got %q %q", l, r)
	}
}

func TestMascotMouthVariesByActivityAndFrame(t *testing.T) {
	cases := []struct {
		activity string
		frame    int
		want     string
	}{
		{"talking", 0, "o"},
		{"talking", 1, "ᴗ"},
		{"shipping", 0, "⌣"},
		{"shipping", 1, "▿"},
		{"plotting", 0, "~"},
		{"plotting", 1, "ˎ"},
		{"unknown", 0, "‿"},
		{"unknown", 1, "_"},
	}
	for _, tc := range cases {
		if got := mascotMouth(tc.activity, tc.frame); got != tc.want {
			t.Errorf("mascotMouth(%q,%d) = %q, want %q", tc.activity, tc.frame, got, tc.want)
		}
	}
}

func TestMascotTopVariesByActivity(t *testing.T) {
	if got := mascotTop("talking", 0); got == "" {
		t.Fatalf("expected mascotTop output for talking")
	}
	if got := mascotTop("talking", 1); got == "" {
		t.Fatalf("expected mascotTop output for talking frame 1")
	}
	if got := mascotTop("plotting", 0); got == "" {
		t.Fatalf("expected mascotTop output for plotting")
	}
	if got := mascotTop("idle", 0); got == "" {
		t.Fatalf("expected mascotTop fallback")
	}
}
