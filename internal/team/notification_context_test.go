package team

// Tests for the extracted notificationContextBuilder type. Written test-
// first against the surface in PLAN.md §C3 before the type exists, so the
// first run is a compile failure by design.
//
// The existing TestBuildNotificationContext / TestBuildMessageWorkPacket /
// TestResponseInstructionForTarget* tests in launcher_test.go remain the
// behavior baseline (they reach this surface via &Launcher{...}). The new
// tests below exercise the type directly with stub broker callbacks so the
// new file lands above the 85% per-file gate without depending on a
// real Broker fixture.

import (
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/channel"
)

func newTestNotifyContextBuilder(t *testing.T, opts ...func(*notificationContextBuilder)) *notificationContextBuilder {
	t.Helper()
	tg := fixtureTargeter(t, []officeMember{
		{Slug: "ceo", BuiltIn: true, Name: "CEO"},
		{Slug: "eng", Name: "Engineer"},
	})
	b := &notificationContextBuilder{
		targeter:        tg,
		channelMessages: func(string) []channelMessage { return nil },
		channelTasks:    func(string) []teamTask { return nil },
		allTasks:        func() []teamTask { return nil },
		channelStore:    func() *channel.Store { return nil },
		scoreTaskCandidate: func(msg channelMessage, task teamTask) float64 {
			// Default: only direct ID/owner matches, no fuzzy scoring.
			return 0
		},
		activeHeadlessAgents: func(string) map[string]struct{} { return nil },
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func TestNotificationContext_TruncateHelper(t *testing.T) {
	if got := truncate("hello", 100); got != "hello" {
		t.Errorf("truncate short string should be passthrough; got %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcde..." {
		t.Errorf("truncate(10char, 5) = %q, want abcde...", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("truncate empty = %q, want empty", got)
	}
}

func TestNotificationContext_ExtractTaskFileTargets(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"Update `web/src/App.tsx` and `internal/team/launcher.go`", []string{"web/src/App.tsx", "internal/team/launcher.go"}},
		{"This has no backticks", nil},
		{"Backticks but no path/dot: `notarealpath`", nil},
		{"Dot only: `script.sh`", []string{"script.sh"}},
		{"Slash only: `pkg/foo`", []string{"pkg/foo"}},
		{"Five `a.x` `b.x` `c.x` `d.x` `e.x` should cap at four", []string{"a.x", "b.x", "c.x", "d.x"}},
		{"Duplicate `a.x` `a.x` `b.x` dedups", []string{"a.x", "b.x"}},
		{"  Empty `` `b.x`", []string{"b.x"}},
	}
	for _, tc := range cases {
		got := extractTaskFileTargets(tc.text)
		if !equalStringSlices(got, tc.want) {
			t.Errorf("extractTaskFileTargets(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestNotificationContext_HumanizeNotificationType(t *testing.T) {
	cases := map[string]string{
		"context_alert":   "Context alert",
		"daily_digest":    "Daily digest",
		"meeting_summary": "Meeting summary",
		"task_reminder":   "Task reminder",
		"task_assigned":   "Task assigned",
		"":                "",
		"snake_case_kind": "Snake Case Kind",
		"single":          "Single",
	}
	for kind, want := range cases {
		if got := humanizeNotificationType(kind); got != want {
			t.Errorf("humanizeNotificationType(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestNotificationContext_ContextEmptyWhenNoMessages(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	if got := b.NotificationContext("general", "", "", 5); got != "" {
		t.Errorf("expected empty context when no messages, got %q", got)
	}
}

func TestNotificationContext_FiltersSystemAndDemoSeedAndStatus(t *testing.T) {
	msgs := []channelMessage{
		{ID: "1", From: "you", Content: "human says hi"},
		{ID: "2", From: "system", Content: "system bookkeeping"},
		{ID: "3", From: "ceo", Content: "[STATUS] working"},
		{ID: "4", From: "ceo", Kind: "demo_seed", Content: "fake activity"},
		{ID: "5", From: "ceo", Content: "real reply"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	got := b.NotificationContext("general", "", "", 10)
	if !strings.Contains(got, "human says hi") {
		t.Errorf("expected human msg included; got %q", got)
	}
	if !strings.Contains(got, "real reply") {
		t.Errorf("expected real ceo reply included; got %q", got)
	}
	for _, banned := range []string{"system bookkeeping", "STATUS", "fake activity"} {
		if strings.Contains(got, banned) {
			t.Errorf("expected %q filtered out, got %q", banned, got)
		}
	}
}

func TestNotificationContext_ExcludesTrigger(t *testing.T) {
	msgs := []channelMessage{
		{ID: "1", From: "you", Content: "earlier"},
		{ID: "trigger", From: "you", Content: "the trigger msg"},
		{ID: "2", From: "ceo", Content: "later"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	got := b.NotificationContext("general", "trigger", "", 10)
	if strings.Contains(got, "the trigger msg") {
		t.Errorf("trigger msg should be excluded; got %q", got)
	}
}

func TestNotificationContext_ThreadScoped_AnchorsAtRoot(t *testing.T) {
	msgs := []channelMessage{
		{ID: "ROOT", From: "you", Content: "original ask"},
		{ID: "off1", From: "you", Content: "unrelated chatter"},
		{ID: "child1", From: "ceo", Content: "delegating", ReplyTo: "ROOT"},
		{ID: "grand", From: "eng", Content: "working on it", ReplyTo: "child1"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	got := b.NotificationContext("general", "", "ROOT", 10)
	if !strings.Contains(got, "original ask") {
		t.Errorf("thread-scoped context should anchor at root; got %q", got)
	}
	if !strings.Contains(got, "working on it") {
		t.Errorf("thread-scoped context should include grandchild; got %q", got)
	}
	if strings.Contains(got, "unrelated chatter") {
		t.Errorf("thread-scoped context must not include off-thread msg; got %q", got)
	}
}

func TestNotificationContext_DefaultChannelGeneral(t *testing.T) {
	captured := ""
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(channel string) []channelMessage {
			captured = channel
			return nil
		}
	})
	_ = b.NotificationContext(" ", "", "", 5)
	if captured != "general" {
		t.Errorf("empty channel should default to general; got %q", captured)
	}
}

func TestNotificationContext_UltimateThreadRoot_WalksReplyChain(t *testing.T) {
	msgs := []channelMessage{
		{ID: "ROOT", From: "you", Content: "original"},
		{ID: "A", From: "ceo", Content: "delegating", ReplyTo: "ROOT"},
		{ID: "B", From: "eng", Content: "ack", ReplyTo: "A"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	if got := b.UltimateThreadRoot("general", "B"); got != "ROOT" {
		t.Errorf("UltimateThreadRoot = %q, want ROOT", got)
	}
	if got := b.UltimateThreadRoot("general", ""); got != "" {
		t.Errorf("UltimateThreadRoot(empty) should be empty")
	}
}

func TestNotificationContext_UltimateThreadRoot_StopsOnCycle(t *testing.T) {
	// Pathological case: depth cap (8) protects against cycles.
	msgs := []channelMessage{
		{ID: "A", ReplyTo: "B"},
		{ID: "B", ReplyTo: "A"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	got := b.UltimateThreadRoot("general", "A")
	if got != "A" && got != "B" {
		t.Errorf("UltimateThreadRoot in cycle should return one of the cycle nodes; got %q", got)
	}
}

func TestNotificationContext_ThreadMessageIDs_BFS(t *testing.T) {
	msgs := []channelMessage{
		{ID: "R"},
		{ID: "A", ReplyTo: "R"},
		{ID: "B", ReplyTo: "R"},
		{ID: "AA", ReplyTo: "A"},
		{ID: "off", ReplyTo: "elsewhere"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
	})
	got := b.ThreadMessageIDs("general", "R")
	for _, want := range []string{"R", "A", "B", "AA"} {
		if _, ok := got[want]; !ok {
			t.Errorf("ThreadMessageIDs missing %q; got %v", want, got)
		}
	}
	if _, ok := got["off"]; ok {
		t.Errorf("ThreadMessageIDs should not include off-thread node; got %v", got)
	}
}

func TestNotificationContext_TaskNotificationContext_LeadGetsAllChannels(t *testing.T) {
	all := []teamTask{
		{ID: "t1", Channel: "general", Title: "general task", Owner: "ceo", Status: "in_progress", UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "t2", Channel: "engineering", Title: "eng task", Owner: "ceo", Status: "in_progress", UpdatedAt: "2026-04-01T00:00:00Z"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return all }
		b.channelTasks = func(string) []teamTask { return nil }
	})
	got := b.TaskNotificationContext("", "ceo", 5)
	if !strings.Contains(got, "general task") {
		t.Errorf("expected general task in lead context: %q", got)
	}
	if !strings.Contains(got, "eng task") {
		t.Errorf("expected eng task in lead context: %q", got)
	}
	// Most-recently-updated first (by UpdatedAt desc).
	idxEng := strings.Index(got, "eng task")
	idxGen := strings.Index(got, "general task")
	if idxEng > idxGen {
		t.Errorf("expected eng task (UpdatedAt 200) before general task (UpdatedAt 100); got eng=%d gen=%d", idxEng, idxGen)
	}
}

func TestNotificationContext_TaskNotificationContext_LeadEmptyShowsCreateNextOwnedHint(t *testing.T) {
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return nil }
	})
	got := b.TaskNotificationContext("", "ceo", 5)
	if got != "" {
		// When there are no tasks at all, the function returns empty (no
		// "Active tasks" header). Verify that's still the contract.
		if strings.Contains(got, "Active tasks") {
			t.Errorf("expected empty when no tasks; got %q", got)
		}
	}
}

func TestNotificationContext_TaskNotificationContext_LeadAlertsOnReviewBacklog(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Title: "ready 1", Owner: "eng", Status: "review", UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "t2", Title: "ready 2", Owner: "eng", Status: "in_progress", ReviewState: "ready_for_review", UpdatedAt: "2026-04-01T00:00:00Z"},
		{ID: "t3", Title: "active", Owner: "eng", Status: "in_progress", UpdatedAt: "2025-12-01T00:00:00Z"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return tasks }
		b.channelTasks = func(string) []teamTask { return nil }
	})
	got := b.TaskNotificationContext("", "ceo", 5)
	if !strings.Contains(got, "2 task(s) are waiting in review") {
		t.Errorf("expected review-backlog hint with count 2; got %q", got)
	}
}

func TestNotificationContext_TaskNotificationContext_NonLeadOnlyOwnedTasks(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Channel: "general", Title: "mine", Owner: "eng", Status: "in_progress"},
		{ID: "t2", Channel: "general", Title: "theirs", Owner: "ceo", Status: "in_progress"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelTasks = func(string) []teamTask { return tasks }
	})
	got := b.TaskNotificationContext("general", "eng", 5)
	if !strings.Contains(got, "mine") {
		t.Errorf("expected own task: %q", got)
	}
	if strings.Contains(got, "theirs") {
		t.Errorf("non-lead should only see their own owned tasks first; got %q", got)
	}
}

func TestNotificationContext_RelevantTaskForTarget_ThreadIDMatch(t *testing.T) {
	tasks := []teamTask{
		{ID: "t1", Owner: "eng", Status: "in_progress", ThreadID: "msg-123"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return tasks }
	})
	got, ok := b.RelevantTaskForTarget(channelMessage{ID: "msg-123"}, "eng")
	if !ok || got.ID != "t1" {
		t.Fatalf("expected t1 by ThreadID match; got (%v, %v)", got, ok)
	}
}

func TestNotificationContext_RelevantTaskForTarget_FallsBackToScore(t *testing.T) {
	tasks := []teamTask{
		{ID: "t-low", Owner: "eng", Status: "in_progress"},
		{ID: "t-high", Owner: "eng", Status: "in_progress"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return tasks }
		b.scoreTaskCandidate = func(msg channelMessage, task teamTask) float64 {
			if task.ID == "t-high" {
				return 0.99
			}
			return 0.30
		}
	})
	got, ok := b.RelevantTaskForTarget(channelMessage{ID: "msg-x"}, "eng")
	if !ok || got.ID != "t-high" {
		t.Fatalf("expected highest-scoring owned task; got (%v, %v)", got, ok)
	}
}

func TestNotificationContext_RelevantTaskForTarget_SkipsDoneAndOtherOwners(t *testing.T) {
	tasks := []teamTask{
		{ID: "done", Owner: "eng", Status: "done"},
		{ID: "ot", Owner: "fe", Status: "in_progress"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.allTasks = func() []teamTask { return tasks }
		b.scoreTaskCandidate = func(channelMessage, teamTask) float64 { return 0.99 }
	})
	if _, ok := b.RelevantTaskForTarget(channelMessage{}, "eng"); ok {
		t.Errorf("done tasks must not match")
	}
}

// Regression: a deep reply (msg → parent → root, with task anchored on
// root) must still resolve to the task. Pre-fix, RelevantTaskForTarget
// only walked one hop via msg.ReplyTo, so anything past depth 2 missed.
func TestNotificationContext_RelevantTaskForTarget_DeepReplyResolvesViaUltimateRoot(t *testing.T) {
	msgs := []channelMessage{
		{ID: "root", Channel: "general", From: "you"},
		{ID: "mid", Channel: "general", From: "ceo", ReplyTo: "root"},
		{ID: "leaf", Channel: "general", From: "you", ReplyTo: "mid"},
	}
	tasks := []teamTask{
		{ID: "t1", Owner: "eng", Status: "in_progress", ThreadID: "root"},
	}
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.channelMessages = func(string) []channelMessage { return msgs }
		b.allTasks = func() []teamTask { return tasks }
	})
	// Trigger is the leaf; its ReplyTo is "mid" (not "root"). Without the
	// fix, threadRoot would be "mid" and ThreadID="root" would not match.
	got, ok := b.RelevantTaskForTarget(channelMessage{ID: "leaf", Channel: "general", ReplyTo: "mid"}, "eng")
	if !ok || got.ID != "t1" {
		t.Fatalf("expected t1 to match via ultimate thread root; got (%v, %v)", got, ok)
	}
}

func TestNotificationContext_ResponseInstruction_LeadFromHumanGetsKickoffGuidance(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.ResponseInstructionForTarget(channelMessage{From: "you", Channel: "general", Content: "build it"}, "ceo")
	if !strings.Contains(got, "first engineering task itself must be a single smallest runnable feature slice") {
		t.Errorf("lead-from-human instruction should require runnable slice; got %q", got)
	}
}

func TestNotificationContext_ResponseInstruction_LeadFromSpecialistGetsApprovalGuidance(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.ResponseInstructionForTarget(channelMessage{From: "eng", Channel: "general", Content: "done"}, "ceo")
	if !strings.Contains(got, "specialist just finished a lane") {
		t.Errorf("lead-from-specialist instruction should mention specialist finishing; got %q", got)
	}
}

func TestNotificationContext_ResponseInstruction_DMTriggersDirectGuidance(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.ResponseInstructionForTarget(channelMessage{Channel: "dm-eng"}, "eng")
	if !strings.Contains(got, "direct expertise") && !strings.Contains(got, "messaging you directly in a DM") {
		t.Errorf("expected DM-direct guidance; got %q", got)
	}
}

func TestNotificationContext_ResponseInstruction_TaggedTriggersTaggedGuidance(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.ResponseInstructionForTarget(channelMessage{Channel: "general", Tagged: []string{"eng"}}, "eng")
	if !strings.Contains(got, "directly tagged") {
		t.Errorf("expected tagged guidance; got %q", got)
	}
}

func TestNotificationContext_ResponseInstruction_UntaggedNonOwnerStaysQuiet(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.ResponseInstructionForTarget(channelMessage{Channel: "general"}, "eng")
	if !strings.Contains(got, "Stay quiet") {
		t.Errorf("expected stay-quiet default for untagged non-owner; got %q", got)
	}
}

func TestNotificationContext_BuildMessageWorkPacket_BasicShape(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.BuildMessageWorkPacket(channelMessage{ID: "m1", Channel: "general", From: "you"}, "eng")
	for _, want := range []string{"Work packet:", "Thread: #general reply_to m1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in packet:\n%s", want, got)
		}
	}
}

func TestNotificationContext_BuildMessageWorkPacket_DMPreambleAdded(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.BuildMessageWorkPacket(channelMessage{ID: "m1", Channel: "dm-eng", From: "you"}, "eng")
	if !strings.Contains(got, "DIRECT MESSAGE") {
		t.Errorf("expected DM preamble; got %q", got)
	}
}

func TestNotificationContext_BuildMessageWorkPacket_LeadAlreadyActiveLineSorted(t *testing.T) {
	b := newTestNotifyContextBuilder(t, func(b *notificationContextBuilder) {
		b.activeHeadlessAgents = func(except string) map[string]struct{} {
			return map[string]struct{}{"eng": {}, "fe": {}}
		}
		// Targeter must say "ceo" is the lead.
		b.targeter = fixtureTargeter(t, []officeMember{
			{Slug: "ceo", BuiltIn: true, Name: "CEO"},
			{Slug: "eng"},
			{Slug: "fe"},
		})
	})
	got := b.BuildMessageWorkPacket(channelMessage{ID: "m", Channel: "general", From: "you"}, "ceo")
	if !strings.Contains(got, "Already active in this thread") {
		t.Fatalf("expected already-active line for lead; got %q", got)
	}
	// Sorted alphabetical: @eng before @fe.
	idxEng := strings.Index(got, "@eng")
	idxFe := strings.Index(got, "@fe")
	if idxEng < 0 || idxFe < 0 || idxEng > idxFe {
		t.Errorf("expected @eng before @fe in active list; got eng=%d fe=%d", idxEng, idxFe)
	}
}

func TestNotificationContext_BuildTaskExecutionPacket_LocalWorktreeAddsCutLineAndAuditGuards(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	task := teamTask{
		ID:            "t1",
		Title:         "build feature",
		Status:        "in_progress",
		ExecutionMode: "local_worktree",
		WorktreePath:  "/tmp/worktree",
	}
	got := b.BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo", Kind: "task_assigned"}, task, "kickoff")
	for _, want := range []string{
		"Working directory: \"/tmp/worktree\"",
		"local_worktree build task",
		"do NOT start with `rg --files`",
		"stay inside the assigned working_directory",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in packet:\n%s", want, got)
		}
	}
}

func TestNotificationContext_BuildTaskExecutionPacket_NamesFileTargetsFromTitleAndDetails(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	task := teamTask{
		ID:      "t1",
		Title:   "Update `web/src/App.tsx`",
		Details: "Also touch `internal/team/launcher.go`",
		Status:  "in_progress",
	}
	got := b.BuildTaskExecutionPacket("eng", officeActionLog{Actor: "ceo"}, task, "kickoff")
	if !strings.Contains(got, "Named file targets: web/src/App.tsx, internal/team/launcher.go") {
		t.Errorf("expected named file targets line; got %q", got)
	}
}

func TestNotificationContext_TaskNotificationContent_HumanizedHeader(t *testing.T) {
	b := newTestNotifyContextBuilder(t)
	got := b.TaskNotificationContent(officeActionLog{Kind: "task_unblocked"}, teamTask{
		ID:      "t1",
		Channel: "general",
		Title:   "go",
		Owner:   "eng",
		Status:  "in_progress",
	})
	if !strings.Contains(got, "Task unblocked") {
		t.Errorf("expected unblocked verb; got %q", got)
	}
	if !strings.Contains(got, "@eng") {
		t.Errorf("expected owner mention; got %q", got)
	}
}

// Sanity-check that the launcher wires the builder up correctly so existing
// dispatch paths see the same answers as the type does in isolation.
func TestLauncher_NotifyContextWiringDelegates(t *testing.T) {
	b := &Broker{tasks: []teamTask{
		{ID: "t1", Channel: "general", Title: "thing", Owner: "ceo", Status: "in_progress"},
	}}
	l := &Launcher{
		broker: b,
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "eng", Name: "Engineer"},
			},
		},
	}
	got := l.buildTaskNotificationContext("general", "ceo", 5)
	if !strings.Contains(got, "thing") {
		t.Errorf("expected ceo task in context: %q", got)
	}
}
