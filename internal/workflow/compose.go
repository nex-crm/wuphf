package workflow

import "fmt"

// MaxCompositionDepth is the maximum nesting level for sub-workflow execution.
// A depth of 3 means: root -> child -> grandchild (3 frames).
const MaxCompositionDepth = 3

// WorkflowLoader loads workflow specs by key (for sub-workflow composition).
type WorkflowLoader interface {
	LoadWorkflow(key string) (*WorkflowSpec, error)
}

// CompositionStack tracks nested workflow execution to enforce depth limits
// and detect cycles. The view layer uses this when a run step has a Workflow
// field set: it pushes a frame before creating a child Runtime, and pops it
// when the child completes.
type CompositionStack struct {
	frames []compositionFrame
}

type compositionFrame struct {
	WorkflowID string
	StepID     string
}

// Push adds a frame to the stack. Returns an error if the depth limit would
// be exceeded or if the workflow ID is already on the stack (cycle).
func (s *CompositionStack) Push(workflowID, stepID string) error {
	if len(s.frames) >= MaxCompositionDepth {
		return fmt.Errorf("sub-workflow depth limit exceeded: max %d levels (trying to push %q from step %q)",
			MaxCompositionDepth, workflowID, stepID)
	}
	if s.Contains(workflowID) {
		return fmt.Errorf("sub-workflow cycle detected: %q is already on the composition stack", workflowID)
	}
	s.frames = append(s.frames, compositionFrame{
		WorkflowID: workflowID,
		StepID:     stepID,
	})
	return nil
}

// Pop removes the top frame from the stack. It is a no-op if the stack is empty.
func (s *CompositionStack) Pop() {
	if len(s.frames) > 0 {
		s.frames = s.frames[:len(s.frames)-1]
	}
}

// Depth returns the current nesting level (number of frames on the stack).
func (s *CompositionStack) Depth() int {
	return len(s.frames)
}

// Contains checks whether a workflow ID is already on the stack.
// This is used for cycle detection before pushing a new frame.
func (s *CompositionStack) Contains(workflowID string) bool {
	for _, f := range s.frames {
		if f.WorkflowID == workflowID {
			return true
		}
	}
	return false
}
