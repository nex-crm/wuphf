package orchestration

import (
	"fmt"
	"sync"
	"time"
)

// ExecutorEvent is emitted when a task transitions state.
type ExecutorEvent struct {
	Type      string // "task:start", "task:complete", "task:fail", "task:timeout"
	TaskID    string
	AgentSlug string
	Result    string
	Error     string
}

// Executor manages task lifecycle with concurrency limits and timeout enforcement.
type Executor struct {
	config      OrchestratorConfig
	tasks       map[string]*TaskDefinition
	locks       map[string]string // taskID → agentSlug
	activeCount int
	listeners   []func(ExecutorEvent)
	timers      map[string]*time.Timer
	stopped     bool
	mu          sync.Mutex
}

// NewExecutor returns an Executor using the provided configuration.
func NewExecutor(config OrchestratorConfig) *Executor {
	return &Executor{
		config:  config,
		tasks:   make(map[string]*TaskDefinition),
		locks:   make(map[string]string),
		timers:  make(map[string]*time.Timer),
	}
}

// OnEvent registers a handler for task events. Returns an unsubscribe function.
func (e *Executor) OnEvent(handler func(ExecutorEvent)) func() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.listeners = append(e.listeners, handler)
	idx := len(e.listeners) - 1
	return func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if idx < len(e.listeners) {
			e.listeners = append(e.listeners[:idx], e.listeners[idx+1:]...)
		}
	}
}

// Submit adds a task to the executor's queue (status "pending").
// Returns an error if a task with the same ID already exists.
func (e *Executor) Submit(task TaskDefinition) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return fmt.Errorf("executor is stopped")
	}
	if _, exists := e.tasks[task.ID]; exists {
		return fmt.Errorf("task %q already submitted", task.ID)
	}
	task.Status = "pending"
	copy := task
	e.tasks[task.ID] = &copy
	return nil
}

// Checkout atomically assigns taskID to agentSlug and starts the task.
// Returns (true, nil) on success, (false, nil) if the task is already locked
// or the concurrency limit is reached, or (false, err) on other errors.
func (e *Executor) Checkout(taskID, agentSlug string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stopped {
		return false, fmt.Errorf("executor is stopped")
	}

	task, ok := e.tasks[taskID]
	if !ok {
		return false, fmt.Errorf("task %q not found", taskID)
	}
	if task.Status != "pending" {
		return false, nil
	}
	if _, locked := e.locks[taskID]; locked {
		return false, nil
	}
	if e.config.MaxConcurrentAgents > 0 && e.activeCount >= e.config.MaxConcurrentAgents {
		return false, nil
	}

	task.Status = "in_progress"
	task.AssignedAgent = agentSlug
	e.locks[taskID] = agentSlug
	e.activeCount++

	// Schedule timeout if configured.
	if e.config.TaskTimeout > 0 {
		timer := time.AfterFunc(e.config.TaskTimeout, func() {
			e.handleTimeout(taskID, agentSlug)
		})
		e.timers[taskID] = timer
	}

	e.emit(ExecutorEvent{Type: "task:start", TaskID: taskID, AgentSlug: agentSlug})
	return true, nil
}

// Release marks a task as completed or failed and frees the agent slot.
// Pass a non-nil result string for success, a non-nil err string for failure.
func (e *Executor) Release(taskID string, result *string, errStr *string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	task, ok := e.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	if _, locked := e.locks[taskID]; !locked {
		return fmt.Errorf("task %q is not checked out", taskID)
	}

	agentSlug := e.locks[taskID]
	delete(e.locks, taskID)
	if e.activeCount > 0 {
		e.activeCount--
	}

	// Cancel timeout timer.
	if timer, ok := e.timers[taskID]; ok {
		timer.Stop()
		delete(e.timers, taskID)
	}

	task.CompletedAt = time.Now().UnixMilli()

	if errStr != nil && *errStr != "" {
		task.Status = "failed"
		e.emit(ExecutorEvent{
			Type:      "task:fail",
			TaskID:    taskID,
			AgentSlug: agentSlug,
			Error:     *errStr,
		})
	} else {
		task.Status = "completed"
		if result != nil {
			task.Result = *result
		}
		e.emit(ExecutorEvent{
			Type:      "task:complete",
			TaskID:    taskID,
			AgentSlug: agentSlug,
			Result:    task.Result,
		})
	}
	return nil
}

// GetActive returns a snapshot of all currently in-progress tasks.
func (e *Executor) GetActive() []TaskDefinition {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []TaskDefinition
	for _, t := range e.tasks {
		if t.Status == "in_progress" {
			out = append(out, *t)
		}
	}
	return out
}

// StopAll cancels all in-progress tasks and marks them as failed.
func (e *Executor) StopAll() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stopped = true

	for id, timer := range e.timers {
		timer.Stop()
		delete(e.timers, id)
	}

	for taskID, agentSlug := range e.locks {
		task := e.tasks[taskID]
		task.Status = "failed"
		task.CompletedAt = time.Now().UnixMilli()
		delete(e.locks, taskID)
		if e.activeCount > 0 {
			e.activeCount--
		}
		e.emit(ExecutorEvent{
			Type:      "task:fail",
			TaskID:    taskID,
			AgentSlug: agentSlug,
			Error:     "executor stopped",
		})
	}
	return nil
}

// handleTimeout is called by the per-task timer; fires a timeout event.
func (e *Executor) handleTimeout(taskID, agentSlug string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	task, ok := e.tasks[taskID]
	if !ok {
		return
	}
	if task.Status != "in_progress" {
		return
	}

	task.Status = "failed"
	task.CompletedAt = time.Now().UnixMilli()
	delete(e.locks, taskID)
	delete(e.timers, taskID)
	if e.activeCount > 0 {
		e.activeCount--
	}

	e.emit(ExecutorEvent{
		Type:      "task:timeout",
		TaskID:    taskID,
		AgentSlug: agentSlug,
		Error:     "task timed out",
	})
}

// emit fires all registered listeners. Must be called with mu held.
func (e *Executor) emit(event ExecutorEvent) {
	for _, fn := range e.listeners {
		go fn(event) // fire async to avoid deadlock with mu held
	}
}
