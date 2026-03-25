package tui

import (
	"testing"
	"time"
)

func TestSpinnerFrameAdvancesOnTick(t *testing.T) {
	s := NewSpinner("loading")
	s.SetActive(true)

	initial := s.frame

	msg := SpinnerTickMsg{Time: time.Now()}
	s2, _ := s.Update(msg)

	if s2.frame == initial {
		t.Fatal("expected frame to advance on SpinnerTickMsg")
	}
	expected := (initial + 1) % len(spinnerFrames)
	if s2.frame != expected {
		t.Fatalf("expected frame %d, got %d", expected, s2.frame)
	}
}

func TestSpinnerNoTickWhenInactive(t *testing.T) {
	s := NewSpinner("loading")
	// active defaults to false

	initial := s.frame
	msg := SpinnerTickMsg{Time: time.Now()}
	s2, cmd := s.Update(msg)

	if s2.frame != initial {
		t.Fatal("expected frame NOT to advance when spinner is inactive")
	}
	if cmd != nil {
		t.Fatal("expected inactive spinner not to schedule ticks")
	}
}

func TestSpinnerViewEmpty_WhenInactive(t *testing.T) {
	s := NewSpinner("loading")
	if s.View() != "" {
		t.Fatal("expected empty view when inactive")
	}
}

func TestSpinnerViewContainsLabel(t *testing.T) {
	s := NewSpinner("doing stuff")
	s.SetActive(true)
	v := s.View()
	if v == "" {
		t.Fatal("expected non-empty view when active")
	}
}

func TestSpinnerWrapsAround(t *testing.T) {
	s := NewSpinner("x")
	s.SetActive(true)
	s.frame = len(spinnerFrames) - 1

	msg := SpinnerTickMsg{Time: time.Now()}
	s2, _ := s.Update(msg)

	if s2.frame != 0 {
		t.Fatalf("expected frame to wrap to 0, got %d", s2.frame)
	}
}
