package channelui

import (
	"strings"
	"testing"
	"time"
)

func TestContainsStringMatchesTrimmedTarget(t *testing.T) {
	items := []string{" fe ", "be", "ceo"}
	if !ContainsString(items, "fe") {
		t.Fatalf("expected trimmed match for fe")
	}
	if !ContainsString(items, "be") {
		t.Fatalf("expected match for be")
	}
	if ContainsString(items, "pm") {
		t.Fatalf("unexpected match for pm")
	}
	if ContainsString(nil, "fe") {
		t.Fatalf("nil slice should not match")
	}
}

func TestRenderTimingSummaryJoinsParts(t *testing.T) {
	got := RenderTimingSummary("2030-01-01T10:00:00Z", "", "", "")
	if got == "" {
		t.Fatalf("expected non-empty timing summary, got empty")
	}
	if !strings.Contains(got, "due") {
		t.Fatalf("expected 'due' label in timing summary, got %q", got)
	}
}

func TestRenderTimingSummaryAllBlank(t *testing.T) {
	if got := RenderTimingSummary("", "", "", ""); got != "" {
		t.Fatalf("blank inputs should yield empty timing summary, got %q", got)
	}
}

func TestPrettyWhenUnparsable(t *testing.T) {
	got := PrettyWhen("not-a-time", "due")
	if !strings.Contains(got, "not-a-time") {
		t.Fatalf("unparsable timestamps should fall through, got %q", got)
	}
}

// PrettyRelativeTime should render future timestamps with an "in …"
// prefix instead of "… ago" so deadlines don't read as history. Past
// timestamps keep the "ago" wording.
func TestPrettyRelativeTimeFutureUsesInPrefix(t *testing.T) {
	in15m := time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)
	got := PrettyRelativeTime(in15m)
	if !strings.HasPrefix(got, "in ") {
		t.Fatalf("expected future minute timestamp to start with 'in ', got %q", got)
	}
	if strings.Contains(got, "ago") {
		t.Fatalf("future timestamp must not say 'ago', got %q", got)
	}
}

func TestPrettyRelativeTimeFutureHourUsesInPrefix(t *testing.T) {
	in3h := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	got := PrettyRelativeTime(in3h)
	if !strings.HasPrefix(got, "in ") {
		t.Fatalf("expected future hour timestamp to start with 'in ', got %q", got)
	}
	if !strings.Contains(got, "h") {
		t.Fatalf("expected hour-grain output, got %q", got)
	}
}

func TestPrettyRelativeTimePastUsesAgoSuffix(t *testing.T) {
	past := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339)
	got := PrettyRelativeTime(past)
	if !strings.HasSuffix(got, " ago") {
		t.Fatalf("expected past timestamp to end with ' ago', got %q", got)
	}
}

func TestPrettyRelativeTimeJustNowWithinAMinute(t *testing.T) {
	near := time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339)
	if got := PrettyRelativeTime(near); got != "just now" {
		t.Fatalf("expected 'just now' for sub-minute future, got %q", got)
	}
}

// TruncateText must respect the rune-count contract: multibyte runes
// stay intact, the result never exceeds max runes, and empty / negative
// max are handled gracefully.
func TestTruncateTextRespectsRuneBudget(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 5, "hello"},
		{"hello world", 5, "hello…"},
		{"héllo", 5, "héllo"},
		{"héllo world", 5, "héllo…"},
		{"日本語テスト", 3, "日本語…"},
		{"", 5, ""},
		{"abc", 0, ""},
		{"abc", -1, ""},
	}
	for _, tc := range cases {
		got := TruncateText(tc.in, tc.max)
		if got != tc.want {
			t.Errorf("TruncateText(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

// MergeOfficeMembers must enrich Name/Role for every member — both the
// ones that follow the channel order AND broker-only entries appended
// at the end. Before the recent fix, only the in-order set was
// enriched, so broker-only members surfaced with blank UI identities.
func TestMergeOfficeMembersEnrichesBrokerOnlyAppendees(t *testing.T) {
	office := []OfficeMember{
		{Slug: "ceo", Name: "Chief Executive", Role: "strategy"},
		{Slug: "fe", Name: "Frontend Engineer", Role: "frontend"},
		{Slug: "guest", Name: "Visiting Investor", Role: "advisor"},
	}
	broker := []Member{
		{Slug: "ceo"},   // in-order: should pick up office Name/Role
		{Slug: "guest"}, // broker-only: should pick up office Name/Role
		{Slug: "stranger", Name: "Surprise Engineer"}, // broker-only, no office entry: should keep its own name + RoleLabel fallback
	}
	channel := &ChannelInfo{Slug: "general", Members: []string{"ceo"}}

	merged := MergeOfficeMembers(office, broker, channel)

	byslug := make(map[string]Member, len(merged))
	for _, m := range merged {
		byslug[m.Slug] = m
	}

	if got := byslug["ceo"]; got.Name != "Chief Executive" || got.Role != "strategy" {
		t.Errorf("in-order member not enriched: %+v", got)
	}
	guest, ok := byslug["guest"]
	if !ok {
		t.Fatalf("expected broker-only 'guest' to be appended, got %v", byslug)
	}
	if guest.Name != "Visiting Investor" || guest.Role != "advisor" {
		t.Errorf("broker-only member missing office enrichment: %+v", guest)
	}
	stranger, ok := byslug["stranger"]
	if !ok {
		t.Fatalf("expected broker-only 'stranger' to be appended, got %v", byslug)
	}
	if stranger.Name != "Surprise Engineer" {
		t.Errorf("non-empty broker Name should win, got %q", stranger.Name)
	}
	if stranger.Role == "" {
		t.Errorf("broker-only member with no office match should fall back to RoleLabel, got empty")
	}
}

// RenderConfirmCard must clamp its card content width to
// min(48, width); a regression here (MaxInt direction) would let the
// card render wider than 48 on wide terminals and overflow into the
// next panel. lipgloss adds a 1-column border on each side of the
// inner content, so visible width = cardWidth + 2.
func TestRenderConfirmCardWidthClamp(t *testing.T) {
	confirm := ChannelConfirm{
		Title:        "Confirm reset",
		Detail:       "This will clear the active conversation.",
		ConfirmLabel: "[enter] confirm",
		CancelLabel:  "[esc] cancel",
	}
	const borderOverhead = 2
	cases := []struct {
		name  string
		width int
	}{
		{"exactly_48", 48},
		{"wider_than_48_clamps_to_48", 200},
		{"absurdly_wide_still_clamps_to_48", 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderConfirmCard(confirm, tc.width)
			if out == "" {
				t.Fatalf("expected non-empty rendered card")
			}
			expectedContent := tc.width
			if expectedContent > 48 {
				expectedContent = 48
			}
			ceiling := expectedContent + borderOverhead
			for _, line := range strings.Split(out, "\n") {
				if visible := visibleWidth(line); visible != ceiling {
					t.Errorf("line width %d != expected %d (cardWidth=%d, terminal=%d): %q", visible, ceiling, expectedContent, tc.width, line)
				}
			}
		})
	}
}

// visibleWidth approximates the rendered column count of a line by
// stripping ANSI CSI escape sequences and counting runes. Sufficient
// for the confirm-card test; not a general-purpose utility.
func visibleWidth(s string) int {
	stripped := stripANSIRunes(s)
	return len([]rune(stripped))
}

func stripANSIRunes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if c >= 0x40 && c <= 0x7e {
				in = false
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Behavior smoke for the "in N minutes" branch — checks the exact
// format string. RFC3339 has second-level precision, so adding an
// integer number of minutes to a now-rounded baseline keeps the diff
// at exactly N minutes when the function reads time.Now() right after.
// Use a small +1s buffer so the diff floor lands cleanly at N.
func TestPrettyRelativeTimeFutureFormatString(t *testing.T) {
	target := time.Now().Truncate(time.Second).Add(42*time.Minute + time.Second)
	in42m := target.UTC().Format(time.RFC3339)
	got := PrettyRelativeTime(in42m)
	want := "in 42m"
	if got != want {
		t.Fatalf("PrettyRelativeTime(in42m) = %q, want %q", got, want)
	}
	pastTarget := time.Now().Truncate(time.Second).Add(-(42*time.Minute + time.Second))
	past := pastTarget.UTC().Format(time.RFC3339)
	if got := PrettyRelativeTime(past); got != "42m ago" {
		t.Fatalf("PrettyRelativeTime(-42m) = %q, want %q", got, "42m ago")
	}
}
