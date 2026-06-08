package team

import (
	"context"
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
)

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

// knownEffortLevels is the union of every reasoning-effort level any runner
// accepts (claudeEffortLevels ∪ codexEffortLevels). The task-create boundary
// validates an incoming Effort against this union; each runner then
// re-validates against its own runtime-specific set at dispatch
// (normalizeClaudeEffort / normalizeCodexEffort). Deriving the union from the
// two source maps keeps this from drifting into a third hand-maintained copy.
var knownEffortLevels = func() map[string]bool {
	levels := make(map[string]bool, len(claudeEffortLevels)+len(codexEffortLevels))
	for level := range claudeEffortLevels {
		levels[level] = true
	}
	for level := range codexEffortLevels {
		levels[level] = true
	}
	return levels
}()

// maxTaskModelLen bounds a persisted per-task Model id. The model is free-form
// (the runtime validates it), but the boundary still caps the length so a
// stray or hostile payload never lands in broker-state.json. 256 comfortably
// fits every real provider/model id.
const maxTaskModelLen = 256

// validateTaskRuntimeFields rejects a task create/update whose per-task LLM
// runtime override is malformed, validating at the system boundary instead of
// trusting the composer to have done it. Provider must be a known runtime kind
// (provider.ValidateKind); Effort must be a recognised reasoning level (the
// claude ∪ codex union — dispatch refines it per the resolved runtime); Model
// is free-form but length-bounded. Empty values are always valid and mean
// "fall back to the owner binding, then the global default".
func validateTaskRuntimeFields(providerKind, model, effort string) error {
	if err := provider.ValidateKind(strings.TrimSpace(providerKind)); err != nil {
		return fmt.Errorf("invalid task provider: %w", err)
	}
	if level := strings.ToLower(strings.TrimSpace(effort)); level != "" && !knownEffortLevels[level] {
		return fmt.Errorf("invalid task effort %q (valid: minimal, low, medium, high, xhigh, max, or empty)", effort)
	}
	if trimmed := strings.TrimSpace(model); len(trimmed) > maxTaskModelLen {
		return fmt.Errorf("task model id too long (%d characters; max %d)", len(trimmed), maxTaskModelLen)
	}
	return nil
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

// activeTaskEffort returns the trimmed Effort of the task the current turn is
// running, or "" when there is no such task. Both headless runners call this at
// dispatch to resolve the per-task effort override. Resolves the turn's task
// via ctx (see turnTaskForCtx) so a parallel instance gets its own task's
// effort, not whichever in_progress task happens to be first.
func (l *Launcher) activeTaskEffort(ctx context.Context, slug string) string {
	if l == nil {
		return ""
	}
	task := l.turnTaskForCtx(ctx, slug)
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.Effort)
}
