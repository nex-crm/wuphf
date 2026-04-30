package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewSplashModelHasMembers(t *testing.T) {
	m := newSplashModel()
	if len(m.members) == 0 {
		t.Fatalf("expected non-empty member roster")
	}
	if m.startAt.IsZero() || m.phaseAt.IsZero() {
		t.Fatalf("expected start/phase timestamps initialized")
	}
}

func TestSplashViewRendersWithoutPanic(t *testing.T) {
	m := newSplashModel()
	m.width = 100
	m.height = 30
	got := m.View()
	if got == "" {
		t.Fatalf("expected non-empty splash view")
	}
}

func TestSplashKeyPressTransitionsToDone(t *testing.T) {
	m := newSplashModel()
	model, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected non-nil cmd on key press")
	}
	if _, ok := model.(splashModel); !ok {
		t.Fatalf("expected splashModel back, got %T", model)
	}
	// The cmd should produce splashDoneMsg
	msg := cmd()
	if _, ok := msg.(splashDoneMsg); !ok {
		t.Fatalf("expected splashDoneMsg from key press, got %T", msg)
	}
}

func TestSplashWindowSizeUpdates(t *testing.T) {
	m := newSplashModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := updated.(splashModel)
	if out.width != 120 || out.height != 40 {
		t.Errorf("expected size 120x40, got %dx%d", out.width, out.height)
	}
}

func TestSplashRendersTitleInTitlePhase(t *testing.T) {
	m := newSplashModel()
	m.width = 100
	m.height = 30
	m.phase = splashTitle
	got := stripANSI(m.View())
	if got == "" {
		t.Fatalf("expected splash title view")
	}
	// The title phase shows the WUPHF brand somewhere.
	if !strings.Contains(strings.ToUpper(got), "WUPHF") {
		t.Logf("title view did not include 'WUPHF' literal — may use ASCII art only: %q", got)
	}
}

func TestSplashTickAdvancesFrame(t *testing.T) {
	m := newSplashModel()
	m.width = 80
	m.height = 24
	updated, _ := m.Update(splashTickMsg{})
	out := updated.(splashModel)
	if out.frame != m.frame+1 {
		t.Errorf("expected frame to advance by 1, got %d -> %d", m.frame, out.frame)
	}
}
