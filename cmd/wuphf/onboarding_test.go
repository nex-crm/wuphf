package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOnboardingModelInit(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	if m.step != stepWelcome {
		t.Fatalf("expected stepWelcome, got %d", m.step)
	}
	if m.done {
		t.Fatal("model should not be done on init")
	}
}

func TestOnboardingModelStepAdvance(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)

	// Populate step 1 inputs.
	m.companyInput.SetValue("Dunder Mifflin")
	m.descInput.SetValue("Paper company")
	m.priorityInput.SetValue("Sell more paper")

	// Press Enter to advance.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(onboardingModel)

	if m2.step != stepSetup {
		t.Fatalf("expected stepSetup after valid step 1, got %d", m2.step)
	}
	if m2.err != "" {
		t.Fatalf("unexpected error: %s", m2.err)
	}
}

func TestOnboardingModelStepAdvanceRequiresCompanyName(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)

	// Leave company name empty.
	m.descInput.SetValue("Paper company")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(onboardingModel)

	// Should stay on stepWelcome.
	if m2.step != stepWelcome {
		t.Fatalf("expected stepWelcome when company name empty, got %d", m2.step)
	}
	if m2.err == "" {
		t.Fatal("expected validation error when company name missing")
	}
}

func TestOnboardingModelPrereqBlock(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepSetup

	// Inject failing prereqs.
	m.prereqs = []prereqResult{
		{Name: "git", Required: true, Found: false},
	}
	m.prereqsOk = false
	m.anthropicKey.SetValue("sk-ant-test")
	m.keyStatus = "valid"

	// Press Enter without acknowledging prereq failure.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(onboardingModel)

	// Should NOT advance to stepTask.
	if m2.step != stepSetup {
		t.Fatalf("expected stepSetup when required prereqs missing, got %d", m2.step)
	}
	if m2.err == "" {
		t.Fatal("expected error message about missing tools")
	}
}

func TestOnboardingModelPrereqBypassWithC(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepSetup
	m.prereqs = []prereqResult{
		{Name: "git", Required: true, Found: false},
	}
	m.prereqsOk = false
	m.anthropicKey.SetValue("sk-ant-test")
	m.keyStatus = "valid"

	// Press 'c' to continue anyway.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m2 := updated.(onboardingModel)
	if !m2.continueUnverified {
		t.Fatal("expected continueUnverified=true after pressing c")
	}

	// Now Enter should advance.
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := updated2.(onboardingModel)
	if m3.step != stepTask {
		t.Fatalf("expected stepTask after bypass, got %d", m3.step)
	}
}

func TestOnboardingModelComplete(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepTask

	// Inject completeMsg directly.
	updated, cmd := m.Update(completeMsg{err: nil})
	m2 := updated.(onboardingModel)

	if !m2.done {
		t.Fatal("expected done=true after completeMsg with nil error")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after completeMsg")
	}
}

func TestOnboardingModelCompleteWithError(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepTask

	updated, _ := m.Update(completeMsg{err: errOnboarding("broker rejected")})
	m2 := updated.(onboardingModel)

	if m2.done {
		t.Fatal("expected done=false when completeMsg has error")
	}
	if m2.err == "" {
		t.Fatal("expected error string set when completeMsg has error")
	}
}

func TestOnboardingModelWindowResize(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(onboardingModel)

	if m2.width != 120 || m2.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", m2.width, m2.height)
	}
}

func TestOnboardingModelKeyValidatedMsg(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepSetup
	m.keyStatus = "checking"

	updated, _ := m.Update(keyValidatedMsg{status: "valid"})
	m2 := updated.(onboardingModel)
	if m2.keyStatus != "valid" {
		t.Fatalf("expected valid key status, got %s", m2.keyStatus)
	}
}

func TestOnboardingModelPrereqsLoaded(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepSetup

	prereqs := []prereqResult{
		{Name: "git", Required: true, Found: true, Version: "2.43.0"},
		{Name: "node", Required: false, Found: true, Version: "20.11.0"},
	}
	updated, _ := m.Update(prereqsLoadedMsg{results: prereqs})
	m2 := updated.(onboardingModel)

	if len(m2.prereqs) != 2 {
		t.Fatalf("expected 2 prereqs, got %d", len(m2.prereqs))
	}
	if !m2.prereqsOk {
		t.Fatal("expected prereqsOk=true when all required prereqs found")
	}
}

func TestOnboardingModelTemplatesLoaded(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	m.step = stepTask

	templates := []taskTemplate{
		{ID: "t1", Title: "Do the thing", OwnerSlug: "eng"},
	}
	updated, _ := m.Update(templatesLoadedMsg{templates: templates})
	m2 := updated.(onboardingModel)

	if len(m2.templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(m2.templates))
	}
}

func TestOnboardingModelView(t *testing.T) {
	m := newOnboardingModel(brokerBaseURL, 80, 24)
	v := m.View()
	if v == "" {
		t.Fatal("View() should return non-empty string")
	}
}

func TestRenderOnboardingChecklist(t *testing.T) {
	cl := onboardingChecklist{
		Dismissed: false,
		Items: []onboardingChecklistItem{
			{Label: "Pick your real team", Done: true},
			{Label: "Add a second key", Done: false},
			{Label: "Connect a GitHub repo", Done: false},
		},
	}
	out := renderOnboardingChecklist(cl, 30)
	if out == "" {
		t.Fatal("expected non-empty checklist output")
	}
}

func TestRenderOnboardingChecklistDismissed(t *testing.T) {
	cl := onboardingChecklist{
		Dismissed: true,
		Items:     []onboardingChecklistItem{{Label: "Anything", Done: false}},
	}
	out := renderOnboardingChecklist(cl, 30)
	if out != "" {
		t.Fatal("expected empty output when checklist is dismissed")
	}
}

func TestRenderOnboardingChecklistAllDone(t *testing.T) {
	cl := onboardingChecklist{
		Dismissed: false,
		Items: []onboardingChecklistItem{
			{Label: "Task A", Done: true},
			{Label: "Task B", Done: true},
		},
	}
	out := renderOnboardingChecklist(cl, 30)
	if out != "" {
		t.Fatal("expected empty output when all items done")
	}
}

func TestAllRequiredPrereqsOk(t *testing.T) {
	cases := []struct {
		name    string
		prereqs []prereqResult
		want    bool
	}{
		{
			name: "all found",
			prereqs: []prereqResult{
				{Name: "git", Required: true, Found: true},
			},
			want: true,
		},
		{
			name: "required missing",
			prereqs: []prereqResult{
				{Name: "git", Required: true, Found: false},
			},
			want: false,
		},
		{
			name: "optional missing is ok",
			prereqs: []prereqResult{
				{Name: "git", Required: true, Found: true},
				{Name: "node", Required: false, Found: false},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := allRequiredPrereqsOk(tc.prereqs)
			if got != tc.want {
				t.Fatalf("allRequiredPrereqsOk() = %v, want %v", got, tc.want)
			}
		})
	}
}

// errOnboarding is a simple error type for tests.
type errOnboarding string

func (e errOnboarding) Error() string { return string(e) }
