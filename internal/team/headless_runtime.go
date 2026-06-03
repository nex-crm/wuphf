package team

import "strings"

// Per-task runtime resolution.
//
// The LLM provider + model are a property of the TASK, not the agent — an
// agent is a persona that can run different tasks on different models (and, in
// a later phase, several at once). Dispatch therefore prefers the active task's
// Provider/Model over the owner agent's binding (now only a soft default),
// which itself falls back to the install-wide default. Effort is resolved the
// same way in headless_effort.go.
//
// The provider+model are chosen together in the composer, so a task that sets a
// Model also sets a Provider. taskModelForKind only returns the task's model
// when the task's provider matches the runtime asking — this prevents a codex
// task's model id from leaking into the claude runner (and vice versa) when the
// agent's own binding routed the turn.

// effectiveProviderKindForAgent picks the runtime kind for slug's next turn,
// preferring the active task's per-task provider over the agent binding /
// global default. The dispatch switch uses it to route to the right runner.
func (l *Launcher) effectiveProviderKindForAgent(slug string) string {
	if l == nil {
		return ""
	}
	if task := l.agentActiveTask(slug); task != nil {
		if kind := strings.TrimSpace(task.Provider); kind != "" {
			return normalizeProviderKind(kind)
		}
	}
	return l.targeter().MemberEffectiveProviderKind(slug)
}

// taskModelForKind returns the active task's per-task model when the task's
// provider matches kind, else "" (let the caller fall back to the binding /
// runtime default). Matching on kind keeps a codex task's model out of the
// claude runner and vice versa.
func (l *Launcher) taskModelForKind(slug, kind string) string {
	if l == nil {
		return ""
	}
	task := l.agentActiveTask(slug)
	if task == nil {
		return ""
	}
	if normalizeProviderKind(strings.TrimSpace(task.Provider)) != normalizeProviderKind(kind) {
		return ""
	}
	return strings.TrimSpace(task.Model)
}
