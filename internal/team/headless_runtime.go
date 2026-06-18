package team

import (
	"context"
	"strings"
)

// Per-task runtime resolution.
//
// The LLM provider + model are a property of the TASK, not the agent — an
// agent is a persona that can run different tasks on different models, and (per
// the parallel-instances change) several at once. Dispatch therefore prefers
// the running turn's task Provider/Model over the owner agent's binding (now
// only a soft default), which itself falls back to the install-wide default.
// Effort is resolved the same way in headless_effort.go.
//
// The provider+model are chosen together in the composer, so a task that sets a
// Model also sets a Provider. taskModelForKind only returns the task's model
// when the task's provider matches the runtime asking — this prevents a codex
// task's model id from leaking into the claude runner (and vice versa) when the
// agent's own binding routed the turn.
//
// ── Per-turn task identity ──────────────────────────────────────────────
// An agent can run several tasks concurrently, so "the agent's active task" is
// ambiguous on the execution path. Every headless turn carries its task id; we
// stash it on the turn's context.Context (set in beginHeadlessCodexTurn) so the
// runtime helpers below resolve the SPECIFIC task the turn is for, rather than
// guessing via agentActiveTask(slug) (which returns the first in_progress task
// — wrong once an agent owns more than one). Callers off the headless path
// (e.g. the interactive pane builder) pass a background context and fall back
// to agentActiveTask, which is correct there because panes are single-task.

type headlessTurnTaskIDKey struct{}

// withHeadlessTurnTaskID returns ctx tagged with the executing turn's task id.
// Empty ids are a no-op so chat turns (no task) don't shadow the fallback.
func withHeadlessTurnTaskID(ctx context.Context, taskID string) context.Context {
	taskID = strings.TrimSpace(taskID)
	if ctx == nil || taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, headlessTurnTaskIDKey{}, taskID)
}

// headlessTurnTaskID reads the executing turn's task id off ctx, or "" when the
// caller is not on a tagged headless turn.
func headlessTurnTaskID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(headlessTurnTaskIDKey{}).(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}

// turnTaskForCtx resolves the task a turn is running, preferring the turn's
// task id carried on ctx over the legacy "first in_progress task for slug"
// lookup. With parallel instances agentActiveTask(slug) alone is ambiguous; the
// ctx task id disambiguates. Falls back to agentActiveTask when ctx carries no
// id (chat turns, the pane path) so existing single-task callers are unchanged.
func (l *Launcher) turnTaskForCtx(ctx context.Context, slug string) *teamTask {
	if l == nil || l.broker == nil {
		return nil
	}
	if id := headlessTurnTaskID(ctx); id != "" {
		if task := l.broker.TaskByID(id); task != nil {
			return task
		}
	}
	return l.agentActiveTask(slug)
}

// raisePlanApprovalAfterTurn surfaces a finished planning turn's plan for human
// approval via the broker. No-op when the task is no longer in Planning (already
// approved/changed) or the broker is unavailable. plan is the harvested plan
// text used as the approval question's context.
func (l *Launcher) raisePlanApprovalAfterTurn(taskID, slug, plan string) {
	if l == nil || l.broker == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	l.broker.RaisePlanApproval(taskID, slug, plan)
}

// turnTaskIDForCtx returns the running turn's task id, preferring the id carried
// on ctx over the legacy agentActiveTaskID(slug) lookup. Used for stream/event
// labelling so each parallel instance's output is tagged with its own task.
func (l *Launcher) turnTaskIDForCtx(ctx context.Context, slug string) string {
	if id := headlessTurnTaskID(ctx); id != "" {
		return id
	}
	return l.agentActiveTaskID(slug)
}

// effectiveProviderKindForAgent picks the runtime kind for slug's current turn,
// preferring the turn task's per-task provider over the agent binding / global
// default. The dispatch switch uses it to route to the right runner.
func (l *Launcher) effectiveProviderKindForAgent(ctx context.Context, slug string) string {
	if l == nil {
		return ""
	}
	if task := l.turnTaskForCtx(ctx, slug); task != nil {
		if kind := strings.TrimSpace(task.Provider); kind != "" {
			return normalizeProviderKind(kind)
		}
	}
	return l.targeter().MemberEffectiveProviderKind(slug)
}

// taskModelForKind returns the turn task's per-task model when the task's
// provider matches kind, else "" (let the caller fall back to the binding /
// runtime default). Matching on kind keeps a codex task's model out of the
// claude runner and vice versa.
func (l *Launcher) taskModelForKind(ctx context.Context, slug, kind string) string {
	if l == nil {
		return ""
	}
	task := l.turnTaskForCtx(ctx, slug)
	if task == nil {
		return ""
	}
	if normalizeProviderKind(strings.TrimSpace(task.Provider)) != normalizeProviderKind(kind) {
		return ""
	}
	return strings.TrimSpace(task.Model)
}
