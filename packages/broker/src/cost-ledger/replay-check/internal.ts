// Internal helpers shared across the replay-check modules.
//
// This module is private to the cost-ledger/replay-check/ directory:
// nothing here is part of the package's public surface (`runReplayCheck`,
// `ReplayCheckReport`, `ReplayDiscrepancy` only). It holds:
//
// - DB row shapes for the prepared statements the orchestrator runs;
// - The key encoders used by the in-memory maps;
// - Small predicates (`arraysEqual`, `eventTypeToKind`); and
// - The lone bounded-parser (`parseStoredThresholds`) shared between
//   `compareBudgets` and any future budget-state checks.
import type { AuditEventKind } from "@wuphf/protocol";

export const BATCH_SIZE = 1_000;

export interface CostEventBatchRow {
  readonly lsn: number;
  readonly type: string;
  readonly payload: Buffer;
}

export interface AgentDayDbRow {
  readonly agentSlug: string;
  readonly dayUtc: string;
  // Aggregate totals are read with `.safeIntegers(true)` so a hostile
  // INTEGER value in the projection (past `Number.MAX_SAFE_INTEGER`)
  // doesn't silently round in the diagnostic — the "diagnostic of last
  // resort" preserves exact bytes from the row.
  readonly totalMicroUsd: bigint;
}

export interface TaskDbRow {
  readonly taskId: string;
  readonly totalMicroUsd: bigint;
}

export interface BudgetDbRow {
  readonly budgetId: string;
  readonly scope: string;
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: string;
  readonly setAtLsn: number;
  readonly tombstoned: number;
}

export interface HighestLsnRow {
  readonly lsn: number;
}

export interface ThresholdCrossingDbRow {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  // Decimal-string form — see the `CAST(observed_micro_usd AS TEXT)`
  // comment on the listCrossingsStmt in the orchestrator. Compare via
  // bigint widening or string equality; never `Number(...)`.
  readonly observedMicroUsd: string;
  readonly limitMicroUsd: number;
}

export function crossingKey(budgetId: string, budgetSetLsn: number, thresholdBps: number): string {
  return `${budgetId}|${budgetSetLsn}|${thresholdBps}`;
}

export function agentDayKey(agentSlug: string, dayUtc: string): string {
  // Pipe separator: agentSlug is constrained by `AgentSlug` brand
  // (lowercase alnum + underscore) and dayUtc is `YYYY-MM-DD`; neither
  // contains `|`, so a key-collision attack via the agent_slug field is
  // structurally impossible.
  return `${agentSlug}|${dayUtc}`;
}

export function splitAgentDayKey(key: string): {
  readonly agentSlug: string;
  readonly dayUtc: string;
} {
  const idx = key.indexOf("|");
  if (idx === -1) {
    throw new Error(`replay-check: malformed agent-day key ${key}`);
  }
  return { agentSlug: key.slice(0, idx), dayUtc: key.slice(idx + 1) };
}

export function arraysEqual(a: readonly number[], b: readonly number[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

export function eventTypeToKind(type: string): AuditEventKind | "other" {
  if (type === "cost.event") return "cost_event";
  if (type === "cost.budget.set") return "budget_set";
  if (type === "cost.budget.threshold.crossed") return "budget_threshold_crossed";
  return "other";
}

export function parseStoredThresholds(raw: string): readonly number[] | { readonly error: string } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    return { error: err instanceof Error ? err.message : String(err) };
  }
  if (!Array.isArray(parsed)) {
    return { error: "thresholds_bps is not an array" };
  }
  // Mirror the protocol's `BudgetSetAuditPayload.thresholdsBps`
  // validation: each entry must be a positive safe-integer ≤ 10_000.
  // Explicit `Number.isSafeInteger` + bounds rather than `typeof
  // === "number"` so values like `Infinity` (which `JSON.parse` can
  // produce from `"[1e999]"`) are rejected here — they would later
  // JSON-serialize as `null` and the corruption signal would be
  // lost by the time it reaches on-call.
  for (const n of parsed) {
    if (typeof n !== "number" || !Number.isSafeInteger(n) || n <= 0 || n > 10_000) {
      return {
        error: `thresholds_bps contains invalid entry ${String(n)} (expected positive safe integer ≤ 10000)`,
      };
    }
  }
  return parsed as readonly number[];
}
