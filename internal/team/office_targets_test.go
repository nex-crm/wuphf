package team

// Tests for the extracted officeTargeter type. Written test-first against
// the surface in PLAN.md §C2 before the type exists, so the first run is a
// compile failure by design.
//
// Coverage focus: the routing-decision branches that were 0% before
// (overflow window naming, pane-failed fallback, headless-one-shot routing,
// 1:1 mode collapsing the targets map). Most existing tests reach this
// surface via &Launcher{...}; the new tests exercise the type directly so
// we don't need a tmux-shaped fixture to assert pane address formats.

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/provider"
)

// fixtureTargeter builds an officeTargeter with sane defaults; tests
// override only the bits they care about.
func fixtureTargeter(t *testing.T, members []officeMember, opts ...func(*officeTargeter)) *officeTargeter {
	t.Helper()
	paneBacked := true
	failed := map[string]string{}
	tg := &officeTargeter{
		sessionName:     "test-sess",
		pack:            &agent.PackDefinition{LeadSlug: "ceo"},
		provider:        provider.KindClaudeCode,
		paneBackedFlag:  &paneBacked,
		failedPaneSlugs: failed,
		isOneOnOne:      func() bool { return false },
		oneOnOneSlug:    func() string { return "" },
		isChannelDM: func(channelSlug string) (bool, string) {
			// Mirror Launcher.isChannelDMRaw's legacy-prefix check so tests
			// that pass channels like "dm-eng" route the same way as
			// production. Tests that need pure no-DM semantics override
			// this hook explicitly.
			if IsDMSlug(channelSlug) {
				return true, DMTargetAgent(channelSlug)
			}
			return false, ""
		},
		snapshotMembers: func() []officeMember {
			return append([]officeMember(nil), members...)
		},
		memberProviderKind: func(string) string { return "" },
	}
	for _, opt := range opts {
		opt(tg)
	}
	return tg
}

func TestTargeter_LeadSlug_PrefersPackLead(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
	})
	if got := tg.LeadSlug(); got != "ceo" {
		t.Errorf("LeadSlug() = %q, want %q", got, "ceo")
	}
}

func TestTargeter_LeadSlug_FallsBackToBuiltIn(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "alpha", BuiltIn: true},
		{Slug: "beta"},
	}, func(o *officeTargeter) { o.pack = &agent.PackDefinition{} })
	if got := tg.LeadSlug(); got != "alpha" {
		t.Errorf("LeadSlug() = %q, want %q", got, "alpha")
	}
}

func TestTargeter_LeadSlug_FallsBackToFirstMember(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "alpha"},
		{Slug: "beta"},
	}, func(o *officeTargeter) { o.pack = &agent.PackDefinition{} })
	if got := tg.LeadSlug(); got != "alpha" {
		t.Errorf("LeadSlug() = %q, want %q", got, "alpha")
	}
}

func TestTargeter_LeadSlug_EmptyWhenNoMembers(t *testing.T) {
	tg := fixtureTargeter(t, nil, func(o *officeTargeter) { o.pack = &agent.PackDefinition{} })
	if got := tg.LeadSlug(); got != "" {
		t.Errorf("LeadSlug() = %q, want empty", got)
	}
}

func TestTargeter_AgentOrder_LeadFirst(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "fe"},
		{Slug: "ceo", BuiltIn: true},
		{Slug: "be"},
	})
	got := tg.AgentOrder()
	if len(got) == 0 || got[0].Slug != "ceo" {
		t.Fatalf("AgentOrder()[0] = %v, want ceo first; full: %v", got, got)
	}
}

// Regression: AgentOrder and PaneSlugs must follow pack.Agents order, not
// the broker snapshot order. Pack indexing is the contract pane spawn
// relies on, so a shuffled snapshot must not perturb the resulting order.
func TestTargeter_AgentOrderAndPaneSlugs_FollowPackNotSnapshot(t *testing.T) {
	// Snapshot order is intentionally non-pack: be, fe, ceo.
	members := []officeMember{
		{Slug: "be"},
		{Slug: "fe"},
		{Slug: "ceo", BuiltIn: true},
	}
	tg := fixtureTargeter(t, members, func(o *officeTargeter) {
		// Pack order: ceo, fe, be — different from snapshot order.
		o.pack = &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo"}, {Slug: "fe"}, {Slug: "be"},
			},
		}
	})
	gotOrder := tg.AgentOrder()
	wantSlugs := []string{"ceo", "fe", "be"}
	if len(gotOrder) != len(wantSlugs) {
		t.Fatalf("AgentOrder length = %d, want %d (got %+v)", len(gotOrder), len(wantSlugs), gotOrder)
	}
	for i, want := range wantSlugs {
		if gotOrder[i].Slug != want {
			t.Errorf("AgentOrder[%d] = %q, want %q (full: %+v)", i, gotOrder[i].Slug, want, gotOrder)
		}
	}
	gotPane := tg.PaneSlugs()
	if len(gotPane) != len(wantSlugs) {
		t.Fatalf("PaneSlugs length = %d, want %d (got %+v)", len(gotPane), len(wantSlugs), gotPane)
	}
	for i, want := range wantSlugs {
		if gotPane[i] != want {
			t.Errorf("PaneSlugs[%d] = %q, want %q (full: %+v)", i, gotPane[i], want, gotPane)
		}
	}
}

func TestTargeter_PaneEligibleMembers_ExcludesHeadlessOneShot(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "codexer"},
	}, func(o *officeTargeter) {
		o.memberProviderKind = func(slug string) string {
			if slug == "codexer" {
				return provider.KindCodex
			}
			return ""
		}
	})
	got := tg.PaneEligibleMembers()
	for _, m := range got {
		if m.Slug == "codexer" {
			t.Fatalf("PaneEligibleMembers should exclude headless-one-shot member; got %v", got)
		}
	}
}

func TestTargeter_VisibleAndOverflow_RespectsCap(t *testing.T) {
	members := []officeMember{
		{Slug: "ceo", BuiltIn: true},
	}
	for _, s := range []string{"a", "b", "c", "d", "e", "f"} {
		members = append(members, officeMember{Slug: s})
	}
	tg := fixtureTargeter(t, members)
	visible := tg.VisibleMembers()
	overflow := tg.OverflowMembers()
	if len(visible) != maxVisibleOfficeAgents {
		t.Fatalf("VisibleMembers length = %d, want %d", len(visible), maxVisibleOfficeAgents)
	}
	if len(visible)+len(overflow) != len(members) {
		t.Fatalf("visible+overflow = %d, want %d", len(visible)+len(overflow), len(members))
	}
	if visible[0].Slug != "ceo" {
		t.Errorf("VisibleMembers[0] = %q, want ceo first", visible[0].Slug)
	}
}

func TestTargeter_OverflowWindowName(t *testing.T) {
	if got := overflowWindowName("designer"); got != "agent-designer" {
		t.Errorf("overflowWindowName(designer) = %q, want agent-designer", got)
	}
}

func TestTargeter_PaneTargets_OneOnOne(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
	}, func(o *officeTargeter) {
		o.isOneOnOne = func() bool { return true }
		o.oneOnOneSlug = func() string { return "fe" }
	})
	targets := tg.PaneTargets()
	if len(targets) != 1 {
		t.Fatalf("PaneTargets() len = %d, want 1; got %v", len(targets), targets)
	}
	tgt, ok := targets["fe"]
	if !ok {
		t.Fatalf("PaneTargets() missing fe entry: %v", targets)
	}
	if tgt.PaneTarget != "test-sess:team.1" {
		t.Errorf("PaneTarget = %q, want test-sess:team.1", tgt.PaneTarget)
	}
}

func TestTargeter_PaneTargets_VisibleAndOverflowAddresses(t *testing.T) {
	members := []officeMember{{Slug: "ceo", BuiltIn: true}}
	for _, s := range []string{"a", "b", "c", "d", "e", "f"} {
		members = append(members, officeMember{Slug: s})
	}
	tg := fixtureTargeter(t, members)
	targets := tg.PaneTargets()

	// First 5 entries are addressed test-sess:team.{1..5} in pack order.
	visible := tg.VisibleMembers()
	for i, m := range visible {
		want := "test-sess:team." + itoa(i+1)
		if got := targets[m.Slug].PaneTarget; got != want {
			t.Errorf("PaneTarget[%s] = %q, want %q", m.Slug, got, want)
		}
	}
	// Overflow: each in its own window addressed test-sess:agent-{slug}.0
	overflow := tg.OverflowMembers()
	for _, m := range overflow {
		want := "test-sess:agent-" + m.Slug + ".0"
		if got := targets[m.Slug].PaneTarget; got != want {
			t.Errorf("PaneTarget[%s] (overflow) = %q, want %q", m.Slug, got, want)
		}
	}
}

func TestTargeter_PaneTargets_EmptyWhenNotPaneBacked(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{{Slug: "ceo", BuiltIn: true}}, func(o *officeTargeter) {
		f := false
		o.paneBackedFlag = &f
	})
	if targets := tg.PaneTargets(); len(targets) != 0 {
		t.Fatalf("PaneTargets when not pane-backed should be empty, got %v", targets)
	}
}

func TestTargeter_PaneTargets_SkipsFailedSlugs(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
	}, func(o *officeTargeter) {
		o.failedPaneSlugs["fe"] = "boom"
	})
	targets := tg.PaneTargets()
	if _, ok := targets["fe"]; ok {
		t.Fatalf("expected fe to be skipped from PaneTargets when in failedPaneSlugs; got %v", targets)
	}
	if _, ok := targets["ceo"]; !ok {
		t.Fatalf("expected ceo to remain in PaneTargets")
	}
}

func TestTargeter_NotificationTargets_AddsHeadlessFallbackForFailedPaneSlug(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
	}, func(o *officeTargeter) {
		o.failedPaneSlugs["fe"] = "spawn failed"
	})
	targets := tg.NotificationTargets()
	tgt, ok := targets["fe"]
	if !ok {
		t.Fatalf("NotificationTargets missing fe: %v", targets)
	}
	if tgt.PaneTarget != "" {
		t.Errorf("fe should fall back to headless (empty PaneTarget); got %q", tgt.PaneTarget)
	}
}

func TestTargeter_NotificationTargets_OneOnOneCollapsesToSingleAgent(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
	}, func(o *officeTargeter) {
		o.isOneOnOne = func() bool { return true }
		o.oneOnOneSlug = func() string { return "fe" }
	})
	targets := tg.NotificationTargets()
	if _, ok := targets["fe"]; !ok {
		t.Fatalf("expected fe in NotificationTargets, got %v", targets)
	}
	if _, ok := targets["ceo"]; ok {
		t.Fatalf("ceo should be excluded in 1:1 mode, got %v", targets)
	}
}

func TestTargeter_ShouldUseHeadlessForSlug_TruthTable(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*officeTargeter)
		slug   string
		want   bool
		reason string
	}{
		{
			name: "non-pane runtime ⇒ headless",
			setup: func(o *officeTargeter) {
				o.provider = provider.KindCodex
			},
			slug: "ceo", want: true, reason: "codex provider is non-pane",
		},
		{
			name:  "pane runtime + pane backed + clean slug ⇒ pane",
			setup: func(*officeTargeter) {},
			slug:  "ceo", want: false, reason: "default Claude path",
		},
		{
			name: "pane runtime + not pane backed ⇒ headless",
			setup: func(o *officeTargeter) {
				f := false
				o.paneBackedFlag = &f
			},
			slug: "ceo", want: true, reason: "no live panes",
		},
		{
			name: "pane runtime + failed pane slug ⇒ headless",
			setup: func(o *officeTargeter) {
				o.failedPaneSlugs["ceo"] = "spawn failed"
			},
			slug: "ceo", want: true, reason: "fallback path",
		},
		{
			name: "pane runtime + member bound to codex ⇒ headless",
			setup: func(o *officeTargeter) {
				o.memberProviderKind = func(slug string) string {
					if slug == "ceo" {
						return provider.KindCodex
					}
					return ""
				}
			},
			slug: "ceo", want: true, reason: "per-member provider override",
		},
		{
			name:  "empty slug ⇒ false (defensive)",
			setup: func(*officeTargeter) {},
			slug:  "  ", want: false, reason: "no slug, no decision",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tg := fixtureTargeter(t, []officeMember{{Slug: "ceo", BuiltIn: true}}, tc.setup)
			if got := tg.ShouldUseHeadlessForSlug(tc.slug); got != tc.want {
				t.Errorf("ShouldUseHeadlessForSlug(%q) = %v, want %v (%s)", tc.slug, got, tc.want, tc.reason)
			}
		})
	}
}

func TestTargeter_ShouldUseHeadlessForTarget_EmptyPaneTargetIsHeadless(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{{Slug: "ceo", BuiltIn: true}})
	if !tg.ShouldUseHeadlessForTarget(notificationTarget{Slug: "ceo", PaneTarget: ""}) {
		t.Errorf("empty PaneTarget should mean headless dispatch")
	}
}

func TestTargeter_SkipPane_FailedAndHeadlessOneShot(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true},
		{Slug: "fe"},
		{Slug: "codexer"},
	}, func(o *officeTargeter) {
		o.failedPaneSlugs["fe"] = "boom"
		o.memberProviderKind = func(s string) string {
			if s == "codexer" {
				return provider.KindCodex
			}
			return ""
		}
	})
	if !tg.SkipPane("fe") {
		t.Errorf("SkipPane(fe) = false, want true (failed pane)")
	}
	if !tg.SkipPane("codexer") {
		t.Errorf("SkipPane(codexer) = false, want true (headless one-shot)")
	}
	if !tg.SkipPane("  ") {
		t.Errorf("SkipPane(empty) should be true")
	}
	if tg.SkipPane("ceo") {
		t.Errorf("SkipPane(ceo) = true, want false")
	}
}

func TestTargeter_NameFor_FallsBackToSlug(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", Name: "Chief"},
		{Slug: "ghost"}, // no Name
	})
	if got := tg.NameFor("ceo"); got != "Chief" {
		t.Errorf("NameFor(ceo) = %q, want Chief", got)
	}
	if got := tg.NameFor("ghost"); got != "ghost" {
		t.Errorf("NameFor(ghost) = %q, want ghost (slug fallback)", got)
	}
}

func TestTargeter_ResolvePaneTarget_FoundAndMissing(t *testing.T) {
	tg := fixtureTargeter(t, []officeMember{{Slug: "ceo", BuiltIn: true}})
	addr, ok := tg.ResolvePaneTarget("ceo")
	if !ok || addr == "" {
		t.Errorf("ResolvePaneTarget(ceo) = (%q, %v), want (non-empty, true)", addr, ok)
	}
	if addr, ok := tg.ResolvePaneTarget("nope"); ok || addr != "" {
		t.Errorf("ResolvePaneTarget(nope) = (%q, %v), want empty,false", addr, ok)
	}
}

func TestTargeter_ProviderCapabilityHelpers(t *testing.T) {
	tg := fixtureTargeter(t, nil)
	if !tg.UsesPaneRuntime() {
		t.Errorf("Claude default should be pane-eligible")
	}
	if !tg.RequiresClaudeSessionReset() {
		t.Errorf("Claude default should require session reset")
	}
	tg.provider = provider.KindCodex
	if tg.UsesPaneRuntime() {
		t.Errorf("Codex must not be pane-eligible")
	}
	if tg.RequiresClaudeSessionReset() {
		t.Errorf("Codex must not require Claude session reset")
	}
}

func TestOfficeLeadSlugFrom_PrefersCEO(t *testing.T) {
	got := officeLeadSlugFrom([]officeMember{
		{Slug: "alpha", BuiltIn: true},
		{Slug: "ceo"},
	})
	if got != "ceo" {
		t.Errorf("officeLeadSlugFrom should prefer ceo, got %q", got)
	}
}

// itoa avoids depending on strconv just for one call site in this test file.
func itoa(n int) string {
	var buf [12]byte
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Sanity: ensure tests can also drive through Launcher.targets() and get
// the same answers. Catches the wiring step where Launcher constructs the
// targeter and shares state with it.
func TestLauncher_TargeterWiringMatchesPaneTargets(t *testing.T) {
	l := &Launcher{
		sessionName:      "wuphf-team",
		paneBackedAgents: true,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend"},
			},
		},
		failedPaneSlugs: map[string]string{},
	}
	got := l.agentPaneTargets()
	if _, ok := got["ceo"]; !ok {
		t.Fatalf("expected ceo in launcher pane targets, got %v", got)
	}
	if !strings.Contains(got["ceo"].PaneTarget, "wuphf-team:team.") {
		t.Fatalf("expected wuphf-team:team.* address, got %q", got["ceo"].PaneTarget)
	}
}
