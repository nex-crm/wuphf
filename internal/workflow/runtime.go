package workflow

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// RuntimeState represents the current state of a workflow execution.
//
// State machine:
//
//	pending ──▶ active ──▶ awaiting_input ──▶ executing_action ──▶ active
//	   │           │              │                │                  │
//	   │           ▼              ▼                ▼                  ▼
//	   │        (error) ◀── (user cancel) ◀── (exec fail)          done
//	   │           │
//	   ▼           ▼
//	 (abort)    (retry / skip / halt)
type RuntimeState string

const (
	StatePending         RuntimeState = "pending"
	StateActive          RuntimeState = "active"
	StateAwaitingInput   RuntimeState = "awaiting_input"
	StateExecutingAction RuntimeState = "executing_action"
	StateError           RuntimeState = "error"
	StateDone            RuntimeState = "done"
	StateAborted         RuntimeState = "aborted"
)

const defaultMaxRetries = 2

// StepEvent records what happened at each step for audit and resume.
type StepEvent struct {
	StepID    string        `json:"step_id"`
	Action    string        `json:"action"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration"`
	Result    map[string]any `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Runtime is the synchronous state machine for executing workflow specs.
// It does not perform async I/O — the bubbletea layer calls HandleAction
// to learn what to do, executes side-effects in a goroutine, then calls
// CompleteAction with the result.
type Runtime struct {
	mu sync.RWMutex

	spec          WorkflowSpec
	state         RuntimeState
	currentStepID string
	dataStore     map[string]any
	stepHistory   []StepEvent
	retryCount    int
	maxRetries    int
	lastError     error

	// Pending action tracking for CompleteAction.
	pendingAction    *ActionSpec
	pendingStepID    string
	pendingStartTime time.Time

	// Injected dependencies.
	actionProvider  ActionProvider
	agentDispatcher AgentDispatcher
	stateStore      StateStore
	workflowLoader  WorkflowLoader
}

// RuntimeOption configures a Runtime via the functional options pattern.
type RuntimeOption func(*Runtime)

// WithActionProvider sets the provider used to execute side-effects.
func WithActionProvider(p ActionProvider) RuntimeOption {
	return func(r *Runtime) { r.actionProvider = p }
}

// WithAgentDispatcher sets the dispatcher for agent tasks.
func WithAgentDispatcher(d AgentDispatcher) RuntimeOption {
	return func(r *Runtime) { r.agentDispatcher = d }
}

// WithStateStore sets the store for persisting runtime snapshots.
func WithStateStore(s StateStore) RuntimeOption {
	return func(r *Runtime) { r.stateStore = s }
}

// WithWorkflowLoader sets the loader for sub-workflow composition.
func WithWorkflowLoader(l WorkflowLoader) RuntimeOption {
	return func(r *Runtime) { r.workflowLoader = l }
}

// WithMaxRetries overrides the default retry limit for failed actions.
func WithMaxRetries(n int) RuntimeOption {
	return func(r *Runtime) { r.maxRetries = n }
}

// NewRuntime validates the spec and creates a runtime in the pending state.
func NewRuntime(spec WorkflowSpec, opts ...RuntimeOption) (*Runtime, error) {
	if err := ValidateSpec(spec); err != nil {
		return nil, fmt.Errorf("invalid workflow spec: %w", err)
	}
	r := &Runtime{
		spec:       spec,
		state:      StatePending,
		dataStore:  make(map[string]any),
		maxRetries: defaultMaxRetries,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// Start transitions the runtime from pending to active and sets the first step.
// Data sources would be hydrated here (via ActionProvider) in a full implementation;
// for now, it initialises the data store and advances to the first step.
func (r *Runtime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != StatePending {
		return fmt.Errorf("cannot start: runtime is in state %s (expected pending)", r.state)
	}
	if len(r.spec.Steps) == 0 {
		return fmt.Errorf("cannot start: workflow has no steps")
	}

	r.currentStepID = r.spec.Steps[0].ID
	r.state = StateActive
	r.transitionToStep()
	return nil
}

// State returns the current runtime state.
func (r *Runtime) State() RuntimeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// CurrentStep returns the current step spec, or nil if the workflow is done/aborted.
func (r *Runtime) CurrentStep() *StepSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.findStep(r.currentStepID)
}

// DataStore returns a copy of the data store for rendering.
func (r *Runtime) DataStore() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]any, len(r.dataStore))
	for k, v := range r.dataStore {
		out[k] = v
	}
	return out
}

// SetData sets a value in the data store at the given JSON Pointer path.
func (r *Runtime) SetData(pointer string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	setPointerPath(r.dataStore, pointer, value)
}

// StepHistory returns a copy of the step history.
func (r *Runtime) StepHistory() []StepEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]StepEvent, len(r.stepHistory))
	copy(out, r.stepHistory)
	return out
}

// LastError returns the most recent error, if any.
func (r *Runtime) LastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastError
}

// NeedsAbortConfirmation returns true if aborting should be confirmed
// (i.e., work has already been done).
func (r *Runtime) NeedsAbortConfirmation() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.stepHistory) > 0
}

// HandleAction processes a key press against the current step's actions.
// It does NOT execute side-effects. Instead, it returns the transition target
// and the execute spec (if any) so the caller can run them asynchronously.
//
// If the action has an execute spec, the runtime transitions to executing_action
// and records the pending action. The caller must call CompleteAction when done.
//
// If the action has no execute spec, the runtime transitions immediately.
func (r *Runtime) HandleAction(key string) (transition string, execute *ExecuteSpec, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != StateActive && r.state != StateAwaitingInput {
		return "", nil, fmt.Errorf("cannot handle action in state %s", r.state)
	}

	step := r.findStep(r.currentStepID)
	if step == nil {
		return "", nil, fmt.Errorf("current step %q not found", r.currentStepID)
	}

	action := r.findAction(step, key)
	if action == nil {
		return "", nil, fmt.Errorf("no action for key %q in step %q", key, step.ID)
	}

	if action.Execute != nil {
		// Action has a side-effect. Transition to executing_action
		// and let the caller run it asynchronously.
		r.state = StateExecutingAction
		r.pendingAction = action
		r.pendingStepID = step.ID
		r.pendingStartTime = time.Now()
		r.retryCount = 0
		return action.Transition, action.Execute, nil
	}

	// No side-effect — transition immediately.
	transition = action.Transition
	r.recordEvent(step.ID, action.Key, nil, nil)

	if transition == TransitionDone {
		r.state = StateDone
		r.currentStepID = ""
		return transition, nil, nil
	}
	if transition != "" {
		if err := r.transitionTo(transition); err != nil {
			return "", nil, err
		}
	}
	return transition, nil, nil
}

// CompleteAction is called after an async action execution finishes.
// On success: stores result in dataStore, records event, transitions.
// On error: retries up to maxRetries, then enters error state.
func (r *Runtime) CompleteAction(result map[string]any, actionErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state != StateExecutingAction {
		return fmt.Errorf("cannot complete action in state %s (expected executing_action)", r.state)
	}

	action := r.pendingAction
	stepID := r.pendingStepID

	// For auto-dispatched run steps, there's no pending action.
	// Use the current step and its first action/transition.
	if action == nil {
		step := r.findStep(r.currentStepID)
		if step == nil {
			return fmt.Errorf("no pending action to complete")
		}
		stepID = step.ID
		// Use step-level transition or first action's transition.
		transition := step.Transition
		if transition == "" && len(step.Actions) > 0 {
			transition = step.Actions[0].Transition
		}
		// Create a synthetic action for the completion flow.
		action = &ActionSpec{Key: "(auto)", Label: "auto", Transition: transition}
	}

	if actionErr != nil {
		r.retryCount++
		if r.retryCount <= r.maxRetries {
			// Stay in executing_action — caller should retry.
			r.lastError = actionErr
			return fmt.Errorf("action failed (attempt %d/%d): %w", r.retryCount, r.maxRetries, actionErr)
		}
		// Exhausted retries — enter error state.
		r.lastError = actionErr
		r.state = StateError
		r.recordEvent(stepID, action.Key, nil, actionErr)
		r.clearPending()
		return fmt.Errorf("action failed after %d attempts: %w", r.maxRetries, actionErr)
	}

	// Success — store result, record event, transition.
	if result != nil {
		// Store the result under the step ID in the data store.
		r.dataStore[stepID] = result
		// Also merge top-level keys for convenience.
		for k, v := range result {
			setPointerPath(r.dataStore, "/"+k, v)
		}
	}

	elapsed := time.Since(r.pendingStartTime)
	r.recordEvent(stepID, action.Key, result, nil)
	_ = elapsed // recorded in event via timestamp delta
	r.lastError = nil

	transition := action.Transition
	r.clearPending()

	if transition == TransitionDone {
		r.state = StateDone
		r.currentStepID = ""
		return nil
	}
	if transition != "" {
		return r.transitionTo(transition)
	}

	// No transition specified — stay on current step.
	r.state = StateActive
	r.transitionToStep()
	return nil
}

// Transition moves the runtime to a specific step by ID. This is used
// for programmatic transitions (e.g., submit/run step auto-transitions).
func (r *Runtime) Transition(targetStepID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == StateDone || r.state == StateAborted {
		return fmt.Errorf("cannot transition: workflow is %s", r.state)
	}

	return r.transitionTo(targetStepID)
}

// Abort cancels the workflow and enters the aborted state.
func (r *Runtime) Abort() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == StateDone {
		return fmt.Errorf("cannot abort: workflow is already done")
	}
	if r.state == StateAborted {
		return fmt.Errorf("cannot abort: workflow is already aborted")
	}

	r.state = StateAborted
	r.clearPending()
	return nil
}

// Snapshot returns a RuntimeSnapshot suitable for persistence.
func (r *Runtime) Snapshot() RuntimeSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ds := make(map[string]any, len(r.dataStore))
	for k, v := range r.dataStore {
		ds[k] = v
	}
	hist := make([]StepEvent, len(r.stepHistory))
	copy(hist, r.stepHistory)

	return RuntimeSnapshot{
		WorkflowID:    r.spec.ID,
		CurrentStepID: r.currentStepID,
		State:         r.state,
		DataStore:     ds,
		StepHistory:   hist,
		RetryCount:    r.retryCount,
		SavedAt:       time.Now(),
	}
}

// --- Internal helpers (must be called with lock held) ---

// findStep looks up a step by ID in the spec.
func (r *Runtime) findStep(id string) *StepSpec {
	for i := range r.spec.Steps {
		if r.spec.Steps[i].ID == id {
			return &r.spec.Steps[i]
		}
	}
	return nil
}

// Spec returns the workflow spec.
func (r *Runtime) Spec() WorkflowSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.spec
}

// ActionProviderFor returns the action provider for a given provider name.
// Returns nil if no matching provider is configured.
func (r *Runtime) ActionProviderFor(provider string) ActionProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.actionProvider
}

// AgentDispatcher returns the agent dispatcher, or nil if not configured.
func (r *Runtime) AgentDispatcher() AgentDispatcher {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentDispatcher
}

// findAction looks up an action by key within a step (case-insensitive).
func (r *Runtime) findAction(step *StepSpec, key string) *ActionSpec {
	for i := range step.Actions {
		if strings.EqualFold(step.Actions[i].Key, key) {
			return &step.Actions[i]
		}
	}
	return nil
}

// transitionTo moves to a target step, validating it exists.
func (r *Runtime) transitionTo(targetID string) error {
	if targetID == TransitionDone {
		r.state = StateDone
		r.currentStepID = ""
		return nil
	}
	step := r.findStep(targetID)
	if step == nil {
		return fmt.Errorf("transition target %q not found", targetID)
	}
	r.currentStepID = targetID
	r.state = StateActive
	r.transitionToStep()
	return nil
}

// transitionToStep sets the runtime sub-state based on step type.
// Interactive steps (select, confirm, edit) → awaiting_input.
// Auto steps (submit, run) stay active for the caller to execute.
func (r *Runtime) transitionToStep() {
	step := r.findStep(r.currentStepID)
	if step == nil {
		return
	}
	switch step.Type {
	case StepSelect, StepConfirm, StepEdit:
		r.state = StateAwaitingInput
	case StepSubmit, StepRun:
		// Stay active — caller will execute and call CompleteAction or Transition.
		r.state = StateActive
	}
}

// recordEvent appends a StepEvent to the history.
func (r *Runtime) recordEvent(stepID, action string, result map[string]any, err error) {
	event := StepEvent{
		StepID:    stepID,
		Action:    action,
		Timestamp: time.Now(),
		Result:    result,
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.stepHistory = append(r.stepHistory, event)
}

// SetExecuting transitions to executing_action state for async operations.
func (r *Runtime) SetExecuting() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = StateExecutingAction
	r.pendingStartTime = time.Now()
}

// clearPending resets pending action state.
func (r *Runtime) clearPending() {
	r.pendingAction = nil
	r.pendingStepID = ""
	r.pendingStartTime = time.Time{}
}

// --- JSON Pointer helpers ---

// resolvePointerPath implements RFC 6901 JSON Pointer resolution.
func resolvePointerPath(data any, pointer string) any {
	if pointer == "" || pointer == "/" {
		return data
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for _, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		switch v := current.(type) {
		case map[string]any:
			current = v[part]
		case []any:
			var idx int
			fmt.Sscanf(part, "%d", &idx)
			if idx >= 0 && idx < len(v) {
				current = v[idx]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

// setPointerPath sets a value at the given JSON Pointer path, creating
// intermediate maps as needed.
func setPointerPath(data map[string]any, pointer string, value any) {
	if pointer == "" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	current := data
	for i, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		if sub, ok := current[part].(map[string]any); ok {
			current = sub
		} else {
			sub := make(map[string]any)
			current[part] = sub
			current = sub
		}
	}
}
