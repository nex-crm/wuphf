package team

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// This file reproduces three bug scenarios reported by the user:
//
//  1. Tagging any other agent but CEO: CEO receives/routes instead of the
//     specialist responding directly.
//  2. CEO tagging a specialist: specialist does not respond.
//  3. DMs to specialists: do not work.
//
// Tests are at two layers:
//   A. Target computation: notificationTargetsForMessage returns the specialist
//      as an immediate target.
//   B. Full dispatch: deliverMessageNotification enqueues a headless turn for
//      the specialist. This is where the "specialist never responds" bug would
//      manifest even if target computation is correct.

// -----------------------------------------------------------------------------
// Layer A — target computation
// -----------------------------------------------------------------------------

func collaborativeTestLauncher(t *testing.T) *Launcher {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })
	return &Launcher{
		// focusMode intentionally left false: collaborative is the default.
		pack: &agent.PackDefinition{
			LeadSlug: "ceo",
			Agents: []agent.AgentConfig{
				{Slug: "ceo", Name: "CEO"},
				{Slug: "fe", Name: "Frontend Engineer"},
				{Slug: "be", Name: "Backend Engineer"},
				{Slug: "cmo", Name: "CMO"},
			},
		},
	}
}

func containsSlugSet(targets []notificationTarget, want string) bool {
	for _, t := range targets {
		if t.Slug == want {
			return true
		}
	}
	return false
}

// Scenario 1 (target layer): human tags @fe in #general. Specialist must be
// among the immediate targets.
func TestBug_HumanTagsSpecialist_CollaborativeMode_SpecialistIsImmediate(t *testing.T) {
	l := collaborativeTestLauncher(t)

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Channel: "general",
		Content: "@fe fix the button",
		Tagged:  []string{"fe"},
	})

	if !containsSlugSet(immediate, "fe") {
		t.Fatalf("bug: human tagged @fe but specialist was NOT notified; got %+v", immediate)
	}
}

// Scenario 2 (target layer): CEO tags @fe. Specialist must be among the
// immediate targets.
func TestBug_CEOTagsSpecialist_CollaborativeMode_SpecialistIsImmediate(t *testing.T) {
	l := collaborativeTestLauncher(t)

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		From:    "ceo",
		Channel: "general",
		Content: "@fe please take this",
		Tagged:  []string{"fe"},
	})

	if !containsSlugSet(immediate, "fe") {
		t.Fatalf("bug: CEO tagged @fe but specialist was NOT notified; got %+v", immediate)
	}
}

// Scenario 3 (target layer): human DMs a specialist. Specialist must be the
// only immediate target (DMs must not leak to CEO).
func TestBug_HumanDMsSpecialist_CollaborativeMode_SpecialistIsImmediate(t *testing.T) {
	l := collaborativeTestLauncher(t)

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		From:    "you",
		Channel: DMSlugFor("fe"),
		Content: "hey, got a minute?",
	})

	if len(immediate) == 0 {
		t.Fatalf("bug: DM to @fe produced zero immediate targets")
	}
	if !containsSlugSet(immediate, "fe") {
		t.Fatalf("bug: DM to @fe did not notify specialist; got %+v", immediate)
	}
	// DMs must not leak to CEO.
	if containsSlugSet(immediate, "ceo") {
		t.Fatalf("bug: DM to @fe leaked to CEO; got %+v", immediate)
	}
}

// -----------------------------------------------------------------------------
// Layer B — full dispatch (target + headless queue)
// -----------------------------------------------------------------------------

// fullDispatchLauncher wires a broker, headless state, and notify debouncer so
// deliverMessageNotification exercises the real dispatch path. Using the codex
// provider routes all non-lead agents through the headless path (no tmux pane
// required), which lets us deterministically observe what got enqueued.
func fullDispatchLauncher(t *testing.T) (*Launcher, chan string, func()) {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }

	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO"},
		{Slug: "fe", Name: "Frontend Engineer"},
		{Slug: "be", Name: "Backend Engineer"},
	}
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex" // forces headless dispatch for every agent
	l.notifyLastDelivered = make(map[string]time.Time)
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "fe", Name: "Frontend Engineer"},
			{Slug: "be", Name: "Backend Engineer"},
		},
	}

	processed := make(chan string, 8)
	oldRunTurn := headlessCodexRunTurn
	headlessCodexRunTurn = func(_ *Launcher, _ context.Context, slug, notification string, channel ...string) error {
		ch := ""
		if len(channel) > 0 {
			ch = channel[0]
		}
		processed <- slug + "|" + ch + "|" + notification
		return nil
	}

	cleanup := func() {
		headlessCodexRunTurn = oldRunTurn
		brokerStatePath = oldPathFn
	}
	return l, processed, cleanup
}

// drainFor waits up to `d` and returns every slug that got a headless turn.
func drainFor(ch <-chan string, d time.Duration) []string {
	var got []string
	deadline := time.After(d)
	for {
		select {
		case msg := <-ch:
			if idx := strings.Index(msg, "|"); idx > 0 {
				got = append(got, msg[:idx])
			}
		case <-deadline:
			return got
		}
	}
}

// Scenario 1 (dispatch): human tags @fe. fe MUST receive a headless turn.
func TestBug_HumanTagsSpecialist_Dispatch_SpecialistReceivesTurn(t *testing.T) {
	l, processed, cleanup := fullDispatchLauncher(t)
	defer cleanup()

	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "@fe fix the button",
		Tagged:  []string{"fe"},
	}

	immediate, _ := l.notificationTargetsForMessage(msg)
	t.Logf("notification targets: %+v", immediate)
	t.Logf("agentNotificationTargets: %+v", l.agentNotificationTargets())
	t.Logf("activeSessionMembers: %+v", l.activeSessionMembers())
	t.Logf("officeLeadSlug: %q", l.officeLeadSlug())

	l.deliverMessageNotification(msg)

	slugs := drainFor(processed, 500*time.Millisecond)
	if !hasSlug(slugs, "fe") {
		t.Fatalf("bug reproduced: human @fe did not dispatch a turn to fe; targets were %+v, headless runs were %v", immediate, slugs)
	}
}

// Scenario 2 (dispatch): CEO tags @fe. fe MUST receive a headless turn.
func TestBug_CEOTagsSpecialist_Dispatch_SpecialistReceivesTurn(t *testing.T) {
	l, processed, cleanup := fullDispatchLauncher(t)
	defer cleanup()

	l.deliverMessageNotification(channelMessage{
		ID:      "msg-2",
		From:    "ceo",
		Channel: "general",
		Content: "@fe please take this",
		Tagged:  []string{"fe"},
	})

	slugs := drainFor(processed, 500*time.Millisecond)
	if !hasSlug(slugs, "fe") {
		t.Fatalf("bug reproduced: CEO @fe did not dispatch a turn to fe; got turns to %v", slugs)
	}
}

// Scenario 3 (dispatch): human DMs fe. fe MUST receive a headless turn and
// CEO must not.
func TestBug_HumanDMsSpecialist_Dispatch_SpecialistReceivesTurn(t *testing.T) {
	l, processed, cleanup := fullDispatchLauncher(t)
	defer cleanup()

	l.deliverMessageNotification(channelMessage{
		ID:      "msg-3",
		From:    "you",
		Channel: DMSlugFor("fe"),
		Content: "got a minute?",
	})

	slugs := drainFor(processed, 500*time.Millisecond)
	if !hasSlug(slugs, "fe") {
		t.Fatalf("bug reproduced: DM to fe did not dispatch a turn to fe; got turns to %v", slugs)
	}
	if hasSlug(slugs, "ceo") {
		t.Fatalf("bug reproduced: DM to fe leaked a turn to ceo; got turns to %v", slugs)
	}
}

func hasSlug(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// DM path: specialist hired via the wizard must be reachable via DM.
// -----------------------------------------------------------------------------

// TestBug_DMToWizardHiredPM_Dispatch reproduces symptom 3 exactly as the user
// described: hire a PM via the web wizard, open the DM with PM, send a
// message. Today pm is added to b.members but NOT to l.pack.Agents, so
// activeSessionMembers (which is pack-gated) excludes pm, agentNotificationTargets
// never registers pm, and DM dispatch silently returns zero targets.
func TestBug_DMToWizardHiredPM_Dispatch(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	// Simulate the wizard flow: POST /office-members adds to b.members, but
	// the Launcher's pack is frozen at launch time.
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "pm", Name: "Product Manager"})
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex"
	l.notifyLastDelivered = make(map[string]time.Time)
	// Pack was set at launch — does NOT include pm.
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "planner", Name: "Planner"},
			{Slug: "executor", Name: "Executor"},
			{Slug: "reviewer", Name: "Reviewer"},
		},
	}

	targetMap := l.agentNotificationTargets()
	t.Logf("targetMap after hiring pm: %+v", targetMap)

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		ID:      "dm-msg-1",
		From:    "you",
		Channel: DMSlugFor("pm"),
		Content: "hey PM, got a minute?",
	})

	if !containsSlugSet(immediate, "pm") {
		t.Fatalf(
			"bug reproduced: DM to wizard-hired pm produced no target for pm. "+
				"targetMap=%+v immediate=%+v. Wizard-hired agents must be reachable via DM.",
			targetMap, immediate,
		)
	}
}

// TestBug_TagWizardHiredPM_InGeneral_Dispatch covers the parallel case for
// public-channel tagging: the user @tags pm in #general, but pm is missing
// from targetMap entirely, so even the explicit-tag bypass (just-applied fix)
// can't save the delivery.
func TestBug_TagWizardHiredPM_InGeneral_Dispatch(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "pm", Name: "Product Manager"})
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex"
	l.notifyLastDelivered = make(map[string]time.Time)
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "planner", Name: "Planner"},
			{Slug: "executor", Name: "Executor"},
			{Slug: "reviewer", Name: "Reviewer"},
		},
	}

	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		ID:      "tag-msg-1",
		From:    "you",
		Channel: "general",
		Content: "@pm please scope this epic",
		Tagged:  []string{"pm"},
	})

	if !containsSlugSet(immediate, "pm") {
		t.Fatalf(
			"bug reproduced: @pm in #general did not reach pm. targetMap=%+v immediate=%+v. "+
				"Wizard-hired agents must be reachable via explicit @-tag.",
			l.agentNotificationTargets(), immediate,
		)
	}
}

// -----------------------------------------------------------------------------
// Root-cause test: channel-membership filter silently drops explicit @-tags.
// -----------------------------------------------------------------------------

// This test mirrors the real-world state: the default #general channel is
// seeded with a fixed roster (ceo/planner/executor/reviewer). When the user
// adds a new specialist via the wizard (e.g. "fe") and then @-tags that
// specialist in #general, the specialist is NOT in ch.Members yet — so the
// allowTarget / isEnabled check silently drops the explicit mention and only
// CEO gets notified. This is symptom 1 of the reported bug.
func TestBug_RootCause_ChannelMembershipFilterDropsExplicitMention(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	// Add a specialist AFTER the broker has seeded default channels (this is
	// what happens when a user hires a new agent via the wizard).
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "fe", Name: "Frontend Engineer"})
	// Note: #general's ch.Members was NOT updated to include "fe".
	b.mu.Unlock()

	l := newHeadlessLauncherForTest()
	l.broker = b
	l.provider = "codex"
	l.notifyLastDelivered = make(map[string]time.Time)
	l.pack = &agent.PackDefinition{
		LeadSlug: "ceo",
		Agents: []agent.AgentConfig{
			{Slug: "ceo", Name: "CEO"},
			{Slug: "fe", Name: "Frontend Engineer"},
		},
	}

	// Sanity: fe IS in the target map (pane/headless resolution is correct).
	targetMap := l.agentNotificationTargets()
	if _, ok := targetMap["fe"]; !ok {
		t.Fatalf("pre-condition failed: fe should be in agentNotificationTargets: %+v", targetMap)
	}

	// Now the actual bug: human @-tags fe in #general. fe should be in the
	// immediate target list, but the enabledMembers filter drops it.
	immediate, _ := l.notificationTargetsForMessage(channelMessage{
		ID:      "msg-rc",
		From:    "you",
		Channel: "general",
		Content: "@fe fix the button",
		Tagged:  []string{"fe"},
	})

	if !containsSlugSet(immediate, "fe") {
		t.Fatalf(
			"ROOT CAUSE: explicit @fe was silently dropped by enabledMembers filter (general members = %v); "+
				"immediate targets = %+v. Explicit tags must bypass the channel-membership filter.",
			b.EnabledMembers("general"), immediate,
		)
	}
}
