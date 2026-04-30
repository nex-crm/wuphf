package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func TestConfirmationForResetTeamMode(t *testing.T) {
	m := channelModel{}
	got := m.confirmationForReset()
	if got == nil {
		t.Fatalf("expected confirmation")
	}
	if got.Action != channelui.ChannelConfirmActionResetTeam {
		t.Errorf("expected action=reset_team, got %q", got.Action)
	}
	if !strings.Contains(strings.ToLower(got.Title), "office") {
		t.Errorf("expected office in title for team mode, got %q", got.Title)
	}
}

func TestConfirmationForResetOneOnOneMode(t *testing.T) {
	m := channelModel{}
	m.sessionMode = "1o1"
	m.oneOnOneAgent = "fe"
	got := m.confirmationForReset()
	if got == nil {
		t.Fatalf("expected confirmation")
	}
	if !strings.Contains(strings.ToLower(got.Title), "direct") {
		t.Errorf("expected 'direct' in title for 1:1 mode, got %q", got.Title)
	}
	if got.Agent != "fe" {
		t.Errorf("expected agent=fe, got %q", got.Agent)
	}
}

func TestConfirmationForResetDM(t *testing.T) {
	got := channelui.ConfirmationForResetDM("fe", "office__fe")
	if got == nil || got.Action != channelui.ChannelConfirmActionResetDM {
		t.Fatalf("expected reset_dm action, got %#v", got)
	}
	if got.Agent != "fe" || got.Channel != "office__fe" {
		t.Errorf("expected agent/channel echoed, got %#v", got)
	}
}

func TestConfirmationForSessionSwitchToOneOnOne(t *testing.T) {
	got := confirmationForSessionSwitch("1o1", "fe")
	if got.Action != channelui.ChannelConfirmActionSwitchMode {
		t.Errorf("expected switch_mode action, got %q", got.Action)
	}
	if !strings.Contains(strings.ToLower(got.Title), "direct") {
		t.Errorf("expected 'direct' in title, got %q", got.Title)
	}
	if got.SessionMode != "1o1" || got.Agent != "fe" {
		t.Errorf("expected mode/agent echoed, got %#v", got)
	}
}

func TestConfirmationForSessionSwitchToOffice(t *testing.T) {
	got := confirmationForSessionSwitch("office", "")
	if !strings.Contains(strings.ToLower(got.Title), "office") {
		t.Errorf("expected 'office' in title, got %q", got.Title)
	}
}

func TestConfirmationForInterviewAnswerWithChoiceAndCustomText(t *testing.T) {
	interview := channelui.Interview{
		ID:       "req-1",
		Question: "Approve?",
	}
	option := &channelui.InterviewOption{ID: "yes", Label: "Approve"}
	got := channelui.ConfirmationForInterviewAnswer(interview, option, "let's ship Friday")
	if got.Action != channelui.ChannelConfirmActionSubmitRequest {
		t.Fatalf("expected submit_request action, got %q", got.Action)
	}
	if got.ChoiceID != "yes" || got.ChoiceText != "Approve" {
		t.Errorf("expected choice carried, got %#v", got)
	}
	if !strings.Contains(got.Detail, "Approve?") {
		t.Errorf("expected question in detail, got %q", got.Detail)
	}
	if !strings.Contains(got.Detail, "let's ship Friday") {
		t.Errorf("expected custom text in detail, got %q", got.Detail)
	}
}

func TestConfirmationForInterviewAnswerNoOptionPromptsForAnswer(t *testing.T) {
	got := channelui.ConfirmationForInterviewAnswer(channelui.Interview{Question: "Why?"}, nil, "")
	if !strings.Contains(got.Detail, "Type an answer before submitting") {
		t.Fatalf("expected coaching detail when no option/text, got %q", got.Detail)
	}
}

func TestRenderConfirmCardContainsTitleAndDetail(t *testing.T) {
	confirm := channelui.ChannelConfirm{
		Title:        "Reset Office Session",
		Detail:       "This clears the live transcript.",
		ConfirmLabel: "Enter",
		CancelLabel:  "Esc",
	}
	got := stripANSI(channelui.RenderConfirmCard(confirm, 80))
	for _, want := range []string{"Reset Office Session", "clears the live transcript", "Enter", "Esc"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in card, got %q", want, got)
		}
	}
}

func TestRenderConfirmCardEnforcesMinimumWidth(t *testing.T) {
	confirm := channelui.ChannelConfirm{Title: "X", Detail: "y", ConfirmLabel: "ok", CancelLabel: "no"}
	if got := channelui.RenderConfirmCard(confirm, 10); got == "" {
		t.Fatalf("undersized confirm card should still render")
	}
}

func TestExecuteConfirmationSubmitRequestWithoutRequestSetsNotice(t *testing.T) {
	m := channelModel{}
	confirm := channelui.ChannelConfirm{Action: channelui.ChannelConfirmActionSubmitRequest, Request: nil}
	model, cmd := m.executeConfirmation(confirm)
	if cmd != nil {
		t.Errorf("expected nil cmd when request missing, got %T", cmd)
	}
	out, ok := model.(channelModel)
	if !ok {
		t.Fatalf("expected channelModel, got %T", model)
	}
	if out.notice == "" {
		t.Errorf("expected notice when request is missing")
	}
}

func TestExecuteConfirmationDefaultActionClearsConfirm(t *testing.T) {
	m := channelModel{}
	m.confirm = &channelui.ChannelConfirm{}
	confirm := channelui.ChannelConfirm{Action: "unknown"}
	model, cmd := m.executeConfirmation(confirm)
	out := model.(channelModel)
	if out.confirm != nil {
		t.Errorf("expected confirm cleared on unknown action")
	}
	if cmd != nil {
		t.Errorf("expected nil cmd on unknown action")
	}
}

func TestExecuteConfirmationSwitchModeReturnsCmd(t *testing.T) {
	m := channelModel{}
	confirm := channelui.ChannelConfirm{Action: channelui.ChannelConfirmActionSwitchMode, SessionMode: "office"}
	_, cmd := m.executeConfirmation(confirm)
	if cmd == nil {
		t.Fatalf("expected non-nil cmd for switch_mode")
	}
}

// Sanity: tea.Cmd returned for reset/dm actions is non-nil.
func TestExecuteConfirmationResetReturnsCmd(t *testing.T) {
	m := channelModel{}
	for _, action := range []channelui.ChannelConfirmAction{channelui.ChannelConfirmActionResetTeam, channelui.ChannelConfirmActionResetDM} {
		_, cmd := m.executeConfirmation(channelui.ChannelConfirm{Action: action, Agent: "fe", Channel: "office"})
		if cmd == nil {
			t.Errorf("action %q expected non-nil cmd", action)
		}
	}
	// keep unused tea import quiet on platforms that need it
	_ = tea.KeyMsg{}
}
