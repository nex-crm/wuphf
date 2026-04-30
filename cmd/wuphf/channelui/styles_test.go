package channelui

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
	styleProducesOutput(t, "SidebarStyle", SidebarStyle(20, 5), "x")
	styleProducesOutput(t, "MainPanelStyle", MainPanelStyle(40, 10), "x")
	styleProducesOutput(t, "ThreadPanelStyle", ThreadPanelStyle(40, 10), "x")
	styleProducesOutput(t, "StatusBarStyle", StatusBarStyle(80), "x")
	styleProducesOutput(t, "ChannelHeaderStyle", ChannelHeaderStyle(80), "x")
	styleProducesOutput(t, "ComposerBorderStyle/blur", ComposerBorderStyle(40, false), "x")
	styleProducesOutput(t, "ComposerBorderStyle/focus", ComposerBorderStyle(40, true), "x")
	styleProducesOutput(t, "TimestampStyle", TimestampStyle(), "10:00")
	styleProducesOutput(t, "MutedTextStyle", MutedTextStyle(), "muted")
	styleProducesOutput(t, "AgentNameStyle/known", AgentNameStyle("ceo"), "CEO")
	styleProducesOutput(t, "AgentNameStyle/unknown", AgentNameStyle("does-not-exist"), "x")
	styleProducesOutput(t, "ActiveChannelStyle", ActiveChannelStyle(), "office")
	styleProducesOutput(t, "DateSeparatorStyle", DateSeparatorStyle(), "today")
	styleProducesOutput(t, "ThreadIndicatorStyle", ThreadIndicatorStyle(), "thread")
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
		if got := AgentAvatar(slug); got != want {
			t.Errorf("AgentAvatar(%q) = %q, want %q", slug, got, want)
		}
	}
}

func TestMascotAccentFallsBackForUnknown(t *testing.T) {
	if got := MascotAccent("ceo"); got != "⌐" {
		t.Errorf("ceo accent should be ⌐, got %q", got)
	}
	if got := MascotAccent("does-not-exist"); got != "•" {
		t.Errorf("unknown slug should fall back to •, got %q", got)
	}
}

func TestMascotEyesFallsBackForUnknown(t *testing.T) {
	l, r := MascotEyes("ceo")
	if l != "■" || r != "■" {
		t.Errorf("ceo eyes should be ■ ■, got %q %q", l, r)
	}
	l, r = MascotEyes("ai")
	if l != "◉" || r != "◉" {
		t.Errorf("ai eyes should be ◉ ◉, got %q %q", l, r)
	}
	l, r = MascotEyes("nobody")
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
		if got := MascotMouth(tc.activity, tc.frame); got != tc.want {
			t.Errorf("MascotMouth(%q,%d) = %q, want %q", tc.activity, tc.frame, got, tc.want)
		}
	}
}

func TestMascotTopVariesByActivity(t *testing.T) {
	if got := MascotTop("talking", 0); got == "" {
		t.Fatalf("expected MascotTop output for talking")
	}
	if got := MascotTop("talking", 1); got == "" {
		t.Fatalf("expected MascotTop output for talking frame 1")
	}
	if got := MascotTop("plotting", 0); got == "" {
		t.Fatalf("expected MascotTop output for plotting")
	}
	if got := MascotTop("idle", 0); got == "" {
		t.Fatalf("expected MascotTop fallback")
	}
}
