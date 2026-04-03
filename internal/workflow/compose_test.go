package workflow

import (
	"strings"
	"testing"
)

func TestCompositionStack_Push_Pop(t *testing.T) {
	var s CompositionStack

	if s.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", s.Depth())
	}

	if err := s.Push("wf-a", "step-1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if s.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", s.Depth())
	}
	if !s.Contains("wf-a") {
		t.Error("expected stack to contain wf-a")
	}

	if err := s.Push("wf-b", "step-2"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if s.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", s.Depth())
	}

	s.Pop()
	if s.Depth() != 1 {
		t.Errorf("expected depth 1 after pop, got %d", s.Depth())
	}
	if s.Contains("wf-b") {
		t.Error("expected wf-b to be removed after pop")
	}
	if !s.Contains("wf-a") {
		t.Error("expected wf-a to remain after popping wf-b")
	}

	s.Pop()
	if s.Depth() != 0 {
		t.Errorf("expected depth 0 after second pop, got %d", s.Depth())
	}

	// Pop on empty stack is a no-op.
	s.Pop()
	if s.Depth() != 0 {
		t.Errorf("expected depth 0 after pop on empty stack, got %d", s.Depth())
	}
}

func TestCompositionStack_DepthLimit(t *testing.T) {
	var s CompositionStack

	// Fill to max depth.
	for i := 0; i < MaxCompositionDepth; i++ {
		id := "wf-" + string(rune('a'+i))
		if err := s.Push(id, "step"); err != nil {
			t.Fatalf("Push %d: %v", i, err)
		}
	}
	if s.Depth() != MaxCompositionDepth {
		t.Errorf("expected depth %d, got %d", MaxCompositionDepth, s.Depth())
	}

	// One more should fail.
	err := s.Push("wf-overflow", "step")
	if err == nil {
		t.Fatal("expected error when exceeding max depth")
	}
	if !strings.Contains(err.Error(), "depth limit exceeded") {
		t.Errorf("expected depth limit error, got: %v", err)
	}

	// Depth should not have changed.
	if s.Depth() != MaxCompositionDepth {
		t.Errorf("depth should remain %d after failed push, got %d", MaxCompositionDepth, s.Depth())
	}
}

func TestCompositionStack_CycleDetection(t *testing.T) {
	var s CompositionStack

	if err := s.Push("wf-a", "step-1"); err != nil {
		t.Fatalf("Push wf-a: %v", err)
	}
	if err := s.Push("wf-b", "step-2"); err != nil {
		t.Fatalf("Push wf-b: %v", err)
	}

	// Pushing wf-a again should fail (A -> B -> A cycle).
	err := s.Push("wf-a", "step-3")
	if err == nil {
		t.Fatal("expected error for cycle detection")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected cycle detection error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wf-a") {
		t.Errorf("error should mention the cycled workflow ID, got: %v", err)
	}

	// Depth should not have changed.
	if s.Depth() != 2 {
		t.Errorf("expected depth 2 after failed push, got %d", s.Depth())
	}
}

func TestCompositionStack_NoCycleForDifferentWorkflows(t *testing.T) {
	var s CompositionStack

	// A -> B -> C should all succeed (no cycles, within depth limit).
	if err := s.Push("wf-a", "step-1"); err != nil {
		t.Fatalf("Push wf-a: %v", err)
	}
	if err := s.Push("wf-b", "step-2"); err != nil {
		t.Fatalf("Push wf-b: %v", err)
	}
	if err := s.Push("wf-c", "step-3"); err != nil {
		t.Fatalf("Push wf-c: %v", err)
	}

	if s.Depth() != 3 {
		t.Errorf("expected depth 3, got %d", s.Depth())
	}
	if !s.Contains("wf-a") || !s.Contains("wf-b") || !s.Contains("wf-c") {
		t.Error("expected all three workflows on the stack")
	}
}
