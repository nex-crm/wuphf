package team

import "strings"

// Per-task reasoning-effort wiring. The new-task composer lets a user pick a
// model-specific effort level for a task; teamTask.Effort persists it. At
// dispatch time each runner translates that level into the runtime's native
// flag — claude-code's `--effort <level>` and codex's
// `-c model_reasoning_effort=<level>`.
//
// The valid sets below are model-specific guardrails: even though the composer
// only offers levels the selected runtime supports, the broker also persists
// and serves these values, so we re-validate at dispatch to avoid feeding an
// unknown level to a CLI (an unknown value can hard-fail the spawn). An
// unrecognised or empty value normalises to "" — "use the runtime default".

// claudeEffortLevels are the levels the `claude` CLI accepts via `--effort`.
// Source: Claude Code CLI reference (low/medium/high/xhigh/max; high is the
// default). Local validation only — the CLI is the final authority per model.
var claudeEffortLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// codexEffortLevels are the levels codex accepts via
// `-c model_reasoning_effort=<level>` (minimal/low/medium/high/xhigh).
var codexEffortLevels = map[string]bool{
	"minimal": true,
	"low":     true,
	"medium":  true,
	"high":    true,
	"xhigh":   true,
}

// normalizeClaudeEffort lower-cases and validates effort against the claude
// CLI's accepted set. Returns "" for empty/unknown values (use default).
func normalizeClaudeEffort(effort string) string {
	level := strings.ToLower(strings.TrimSpace(effort))
	if level == "" || !claudeEffortLevels[level] {
		return ""
	}
	return level
}

// normalizeCodexEffort lower-cases and validates effort against codex's
// accepted model_reasoning_effort set. Returns "" for empty/unknown values.
func normalizeCodexEffort(effort string) string {
	level := strings.ToLower(strings.TrimSpace(effort))
	if level == "" || !codexEffortLevels[level] {
		return ""
	}
	return level
}

// activeTaskEffort returns the trimmed Effort of the task slug is currently
// running (in_progress), or "" when slug has no active task. Both headless
// runners call this at dispatch to resolve the per-task effort override.
func (l *Launcher) activeTaskEffort(slug string) string {
	if l == nil {
		return ""
	}
	task := l.agentActiveTask(slug)
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Effort)
}
