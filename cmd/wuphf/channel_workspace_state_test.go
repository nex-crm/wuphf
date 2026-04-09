package main

import (
	"strings"
	"testing"
)

func TestBuildOfficeIntroLinesUsesWorkspaceState(t *testing.T) {
	m := newChannelModel(false)
	m.brokerConnected = true
	m.members = []channelMember{{Slug: "ceo", Name: "CEO"}, {Slug: "pm", Name: "Product Manager"}}
	m.tasks = []channelTask{{ID: "task-1", Title: "Ship launch", Status: "in_progress", Owner: "pm"}}
	m.requests = []channelInterview{{ID: "req-1", Kind: "approval", Status: "pending", Title: "Approve launch copy", Question: "Approve launch copy?", From: "ceo"}}

	lines := m.buildOfficeIntroLines(96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Welcome to The WUPHF Office.") {
		t.Fatalf("expected office welcome copy, got %q", plain)
	}
	if !strings.Contains(plain, "Ready to work") {
		t.Fatalf("expected ready-to-work card, got %q", plain)
	}
	if !strings.Contains(plain, "Use /switcher to move through the office, or /recover to regain context before replying.") {
		t.Fatalf("expected switcher guidance, got %q", plain)
	}
}

func TestBuildOfficeIntroLinesShowsOfflinePreviewGuidance(t *testing.T) {
	m := newChannelModel(false)
	m.brokerConnected = false

	lines := m.buildOfficeIntroLines(96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Offline preview") {
		t.Fatalf("expected offline preview messaging, got %q", plain)
	}
	if !strings.Contains(plain, "Launch WUPHF to attach the live office, or run /doctor to inspect runtime readiness.") {
		t.Fatalf("expected doctor guidance, got %q", plain)
	}
}

func TestBuildDirectIntroLinesPreservesDirectSessionResetLanguage(t *testing.T) {
	m := newChannelModel(false)
	m.sessionMode = "1o1"
	m.oneOnOneAgent = "be"

	lines := m.buildDirectIntroLines(96)
	plain := stripANSI(joinRenderedLines(lines))

	if !strings.Contains(plain, "Direct session reset. Agent pane reloaded in place.") {
		t.Fatalf("expected direct-session reset copy, got %q", plain)
	}
	if !strings.Contains(plain, "Use /switcher to jump back to the office.") {
		t.Fatalf("expected switcher guidance in direct intro, got %q", plain)
	}
}

func TestCurrentHeaderMetaUsesWorkspaceStateForOfficeMessages(t *testing.T) {
	m := newChannelModel(false)
	m.activeApp = officeAppMessages
	m.activeChannel = "launch"
	m.brokerConnected = true
	m.members = []channelMember{{Slug: "ceo", Name: "CEO"}, {Slug: "pm", Name: "Product Manager"}}
	m.tasks = []channelTask{{ID: "task-1", Title: "Ship launch", Status: "in_progress", Owner: "pm"}}
	m.requests = []channelInterview{{ID: "req-1", Kind: "approval", Status: "pending", Title: "Approve launch copy", Question: "Approve launch copy?", From: "ceo", Blocking: true}}

	meta := stripANSI(m.currentHeaderMeta())
	if !strings.Contains(meta, "2 teammates") {
		t.Fatalf("expected teammate count in header meta, got %q", meta)
	}
	if !strings.Contains(meta, "1 waiting on you") {
		t.Fatalf("expected blocking request count in header meta, got %q", meta)
	}
}

func TestCurrentWorkspaceUIStatePromotesDoctorWarningsIntoReadiness(t *testing.T) {
	m := newChannelModel(false)
	m.brokerConnected = true
	m.activeChannel = "general"
	m.doctor = &channelDoctorReport{
		Checks: []doctorCheck{
			{
				Label:    "Connected accounts",
				Severity: doctorWarn,
				Detail:   "No accounts connected.",
				NextStep: "Connect Gmail, CRM, or another account in the provider dashboard.",
			},
		},
	}

	state := m.currentWorkspaceUIState()
	if state.Readiness.Level != workspaceReadinessWarn {
		t.Fatalf("expected warning readiness, got %+v", state.Readiness)
	}
	if !strings.Contains(state.Readiness.NextStep, "Connect Gmail, CRM") {
		t.Fatalf("expected doctor next step to flow into readiness, got %+v", state.Readiness)
	}
}
