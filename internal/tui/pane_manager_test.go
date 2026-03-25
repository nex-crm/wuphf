package tui

import (
	"fmt"
	"strings"
	"testing"
)

// mockPane implements PaneInterface for testing.
type mockPane struct {
	slug    string
	name    string
	focused bool
	alive   bool
	keys    [][]byte
	w, h    int
	closed  bool
}

func newMockPane(slug, name string) *mockPane {
	return &mockPane{slug: slug, name: name, alive: true}
}

func (m *mockPane) Slug() string        { return m.slug }
func (m *mockPane) Name() string        { return m.name }
func (m *mockPane) View() string        { return fmt.Sprintf("[%s output]", m.slug) }
func (m *mockPane) SendKey(data []byte)  { m.keys = append(m.keys, append([]byte{}, data...)) }
func (m *mockPane) IsAlive() bool       { return m.alive }
func (m *mockPane) IsFocused() bool     { return m.focused }
func (m *mockPane) SetFocused(f bool)   { m.focused = f }
func (m *mockPane) Resize(w, h int)     { m.w = w; m.h = h }
func (m *mockPane) Close()              { m.closed = true; m.alive = false }

func TestPaneManagerAddRemove(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("leader", "Leader Agent")
	p2 := newMockPane("coder", "Coder Agent")
	p3 := newMockPane("tester", "Tester Agent")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	if pm.PaneCount() != 3 {
		t.Fatalf("expected 3 panes, got %d", pm.PaneCount())
	}

	// First pane should be focused.
	if !p1.IsFocused() {
		t.Error("first pane should be focused after add")
	}

	// Duplicate slug should be ignored.
	pm.AddPane(newMockPane("leader", "Dupe"))
	if pm.PaneCount() != 3 {
		t.Error("duplicate slug should not be added")
	}

	// Remove middle pane.
	pm.RemovePane("coder")
	if pm.PaneCount() != 2 {
		t.Fatalf("expected 2 panes after remove, got %d", pm.PaneCount())
	}
	if !p2.closed {
		t.Error("removed pane should be closed")
	}

	// Slugs should be ["leader", "tester"].
	if pm.Panes()[0].Slug() != "leader" || pm.Panes()[1].Slug() != "tester" {
		t.Error("unexpected pane order after removal")
	}

	// Remove non-existent slug — no panic.
	pm.RemovePane("nonexistent")
	if pm.PaneCount() != 2 {
		t.Error("removing non-existent slug should be a no-op")
	}
}

func TestPaneManagerFocusCycle(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")
	p3 := newMockPane("c", "C")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	// Initial focus on first pane.
	if pm.Focused().Slug() != "a" {
		t.Fatalf("expected focus on 'a', got '%s'", pm.Focused().Slug())
	}

	// FocusNext cycles forward.
	pm.FocusNext()
	if pm.Focused().Slug() != "b" {
		t.Errorf("expected focus on 'b', got '%s'", pm.Focused().Slug())
	}
	if p1.IsFocused() {
		t.Error("p1 should be unfocused after FocusNext")
	}

	pm.FocusNext()
	if pm.Focused().Slug() != "c" {
		t.Errorf("expected focus on 'c', got '%s'", pm.Focused().Slug())
	}

	// Wrap around.
	pm.FocusNext()
	if pm.Focused().Slug() != "a" {
		t.Errorf("expected wrap to 'a', got '%s'", pm.Focused().Slug())
	}

	// FocusPrev wraps backward.
	pm.FocusPrev()
	if pm.Focused().Slug() != "c" {
		t.Errorf("expected wrap to 'c', got '%s'", pm.Focused().Slug())
	}

	// FocusPane by slug.
	pm.FocusPane("b")
	if pm.Focused().Slug() != "b" {
		t.Errorf("expected focus on 'b', got '%s'", pm.Focused().Slug())
	}

	// HandleKey ctrl+n / ctrl+p.
	pm.HandleKey("ctrl+n")
	if pm.Focused().Slug() != "c" {
		t.Errorf("ctrl+n should move to 'c', got '%s'", pm.Focused().Slug())
	}

	pm.HandleKey("ctrl+p")
	if pm.Focused().Slug() != "b" {
		t.Errorf("ctrl+p should move to 'b', got '%s'", pm.Focused().Slug())
	}

	// HandleKey ctrl+1 jumps to index 0.
	pm.HandleKey("ctrl+1")
	if pm.Focused().Slug() != "a" {
		t.Errorf("ctrl+1 should focus 'a', got '%s'", pm.Focused().Slug())
	}

	pm.HandleKey("ctrl+3")
	if pm.Focused().Slug() != "c" {
		t.Errorf("ctrl+3 should focus 'c', got '%s'", pm.Focused().Slug())
	}

	// Out-of-range ctrl+7 should be a no-op.
	pm.HandleKey("ctrl+7")
	if pm.Focused().Slug() != "c" {
		t.Error("ctrl+7 out of range should be a no-op")
	}
}

func TestPaneManagerViewLayout(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("leader", "Leader")
	p2 := newMockPane("coder", "Coder")
	p3 := newMockPane("tester", "Tester")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	view := pm.View(120, 40)

	if view == "" {
		t.Fatal("View should not be empty")
	}

	// All pane slugs should appear in the rendered output.
	for _, slug := range []string{"leader", "coder", "tester"} {
		if !strings.Contains(view, slug) {
			t.Errorf("View should contain pane slug %q", slug)
		}
	}

	// Focused pane indicator should be present.
	if !strings.Contains(view, "*") {
		t.Error("View should contain focused indicator '*'")
	}
}

func TestPaneManagerBroadcast(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")

	pm.AddPane(p1)
	pm.AddPane(p2)

	// Normal mode: only focused pane receives keys.
	pm.HandleKey("x")
	if len(p1.keys) != 1 {
		t.Error("focused pane should receive key")
	}
	if len(p2.keys) != 0 {
		t.Error("unfocused pane should not receive key in normal mode")
	}

	// Broadcast mode: all panes receive keys.
	pm.SetBroadcastMode(true)
	if !pm.IsBroadcastMode() {
		t.Error("broadcast mode should be on")
	}

	pm.HandleKey("y")
	if len(p1.keys) != 2 {
		t.Error("p1 should receive broadcast key")
	}
	if len(p2.keys) != 1 {
		t.Error("p2 should receive broadcast key")
	}
}

func TestPaneManagerEmptyOps(t *testing.T) {
	pm := NewPaneManager()

	// These should not panic on empty manager.
	pm.FocusNext()
	pm.FocusPrev()
	pm.HandleKey("ctrl+n")
	pm.HandleKey("x")

	if pm.Focused() != nil {
		t.Error("Focused on empty manager should return nil")
	}

	view := pm.View(80, 24)
	if !strings.Contains(view, "No panes") {
		t.Error("empty view should show 'No panes' placeholder")
	}
}

func TestPaneManagerResizeAll(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")

	pm.AddPane(p1)
	pm.AddPane(p2)

	pm.ResizeAll(200, 50)

	if p1.w != 200 || p1.h != 50 {
		t.Errorf("p1 resize: got %dx%d, want 200x50", p1.w, p1.h)
	}
	if p2.w != 200 || p2.h != 50 {
		t.Errorf("p2 resize: got %dx%d, want 200x50", p2.w, p2.h)
	}
}

func TestPaneManagerRemoveFocused(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")
	p3 := newMockPane("c", "C")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	// Focus the last pane, then remove it.
	pm.FocusPane("c")
	pm.RemovePane("c")

	// Focus should fall back to the new last pane.
	if pm.Focused() == nil {
		t.Fatal("focused should not be nil after removing last focused pane")
	}
	if pm.Focused().Slug() != "b" {
		t.Errorf("expected focus to fall back to 'b', got '%s'", pm.Focused().Slug())
	}
}

func TestPaneManagerCloseAll(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")
	p3 := newMockPane("c", "C")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	pm.CloseAll()

	if !p1.closed || !p2.closed || !p3.closed {
		t.Error("CloseAll should close all panes")
	}
	if p1.alive || p2.alive || p3.alive {
		t.Error("all panes should be dead after CloseAll")
	}
}

func TestPaneManagerRemoveDeadPanes(t *testing.T) {
	pm := NewPaneManager()

	p1 := newMockPane("a", "A")
	p2 := newMockPane("b", "B")
	p3 := newMockPane("c", "C")

	pm.AddPane(p1)
	pm.AddPane(p2)
	pm.AddPane(p3)

	// Kill p2 (simulate process exit).
	p2.alive = false

	dead := pm.RemoveDeadPanes()

	if len(dead) != 1 || dead[0] != "b" {
		t.Errorf("expected dead=[b], got %v", dead)
	}
	if pm.PaneCount() != 2 {
		t.Errorf("expected 2 panes after removing dead, got %d", pm.PaneCount())
	}

	// Kill all remaining.
	p1.alive = false
	p3.alive = false
	dead = pm.RemoveDeadPanes()
	if len(dead) != 2 {
		t.Errorf("expected 2 dead panes, got %d", len(dead))
	}
	if pm.PaneCount() != 0 {
		t.Errorf("expected 0 panes after removing all dead, got %d", pm.PaneCount())
	}
}
