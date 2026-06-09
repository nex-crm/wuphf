import type { LLMRuntimeKind } from "../api/client";

// Reasoning-effort options for the new-task composer. Effort is model-specific:
// the levels a runtime accepts depend on which runtime (and model family) will
// execute the task, so the composer derives its effort chip from the selected
// provider + model rather than offering a fixed global scale.
//
// These sets MIRROR the dispatch-time guardrails in
// internal/team/headless_effort.go. Keep them in lockstep — the broker
// re-validates effort per runtime at dispatch and silently drops an unknown
// level, so offering a level here that the Go side rejects would be a no-op.
//
// Only claude-code and codex apply effort at dispatch today (the two headline
// coding runtimes). opencode, mlx-lm, ollama, and exo do not thread effort, so
// their composer chip offers "Default" only — honest with the backend.

export interface EffortOption {
  value: string;
  label: string;
}

// Empty value = "use the runtime's default effort" (broker stores no override).
export const DEFAULT_EFFORT_VALUE = "";

// claude `--effort` levels (low/medium/high/xhigh/max; high is the CLI default).
const CLAUDE_EFFORT_LEVELS = ["low", "medium", "high", "xhigh", "max"] as const;

// codex `model_reasoning_effort` levels (minimal/low/medium/high/xhigh).
const CODEX_EFFORT_LEVELS = [
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
] as const;

const DEFAULT_OPTION: EffortOption = {
  value: DEFAULT_EFFORT_VALUE,
  label: "Default effort",
};

function titleCase(level: string): string {
  if (level === "xhigh") return "Extra high";
  return level.charAt(0).toUpperCase() + level.slice(1);
}

// effortLevelsForKind returns the bare effort levels a runtime accepts, or an
// empty array for runtimes that do not thread effort at dispatch.
//
// opencode is intentionally treated as effort-less even though it can proxy a
// claude or gpt model: its WUPHF runner does not pass an effort flag, so an
// effort here would be a no-op. Revisit if the opencode runner gains effort.
export function effortLevelsForKind(kind: LLMRuntimeKind | ""): string[] {
  if (kind === "claude-code") return [...CLAUDE_EFFORT_LEVELS];
  if (kind === "codex") return [...CODEX_EFFORT_LEVELS];
  return [];
}

// effortOptionsForKind returns the composer's effort dropdown options for a
// runtime. Always begins with the "Default effort" entry.
export function effortOptionsForKind(
  kind: LLMRuntimeKind | "",
): EffortOption[] {
  return [
    DEFAULT_OPTION,
    ...effortLevelsForKind(kind).map((level) => ({
      value: level,
      label: titleCase(level),
    })),
  ];
}

// runtimeSupportsEffort reports whether the composer should enable the effort
// chip for a runtime (false for opencode/local runtimes — default only).
export function runtimeSupportsEffort(kind: LLMRuntimeKind | ""): boolean {
  return effortLevelsForKind(kind).length > 0;
}

// normalizeEffortForKind clamps a stored/selected effort to the runtime's
// accepted set, returning "" (default) when the level is unknown for that
// runtime. Used when the provider/model changes so a now-invalid level
// (e.g. codex "max" after switching to codex) falls back to default instead
// of being sent and dropped.
export function normalizeEffortForKind(
  kind: LLMRuntimeKind | "",
  effort: string,
): string {
  const level = effort.trim().toLowerCase();
  if (!level) return DEFAULT_EFFORT_VALUE;
  return effortLevelsForKind(kind).includes(level)
    ? level
    : DEFAULT_EFFORT_VALUE;
}
