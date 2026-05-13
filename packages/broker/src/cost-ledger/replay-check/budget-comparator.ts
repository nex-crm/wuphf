// Comparator for the `cost_budgets` projection vs the in-memory
// `replayedBudgets` state the scan loop builds from `cost.budget.set`
// audit events.
//
// Surfaces `budget_state_mismatch` (per-field), `budget_row_missing`,
// and `budget_row_ghost`. Field-level coverage includes scope,
// subjectId, limitMicroUsd, thresholdsBps, setAtLsn, and tombstoned.
// `parseStoredThresholds` is used here to surface a structured
// discrepancy for an unparseable `thresholds_bps` JSON cell instead
// of throwing out of `runReplayCheck` and blinding the diagnostic.
import type { BudgetId } from "@wuphf/protocol";
import type { ReplayDiscrepancy, ReplayedBudget } from "./discrepancy.ts";
import type { BudgetDbRow } from "./internal.ts";

// Strict equality across two integer arrays. Only the budget
// comparator round-trips `thresholdsBps` against the stored JSON,
// so the helper lives next to its caller.
function arraysEqual(a: readonly number[], b: readonly number[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

// Bounded parser for the `thresholds_bps` projection cell. Mirrors
// the protocol's `BudgetSetAuditPayload.thresholdsBps` validation:
// each entry must be a positive safe-integer ≤ 10_000. Explicit
// `Number.isSafeInteger` + bounds rather than `typeof === "number"`
// so values like `Infinity` (which `JSON.parse` can produce from
// `"[1e999]"`) are rejected here — they would later JSON-serialize
// as `null` and the corruption signal would be lost by the time it
// reaches on-call.
function parseStoredThresholds(raw: string): readonly number[] | { readonly error: string } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    return { error: err instanceof Error ? err.message : String(err) };
  }
  if (!Array.isArray(parsed)) {
    return { error: "thresholds_bps is not an array" };
  }
  for (const n of parsed) {
    if (typeof n !== "number" || !Number.isSafeInteger(n) || n <= 0 || n > 10_000) {
      return {
        error: `thresholds_bps contains invalid entry ${String(n)} (expected positive safe integer ≤ 10000)`,
      };
    }
  }
  return parsed as readonly number[];
}

export function compareBudgets(
  replayed: ReadonlyMap<string, ReplayedBudget>,
  stored: ReadonlyMap<string, BudgetDbRow>,
  out: ReplayDiscrepancy[],
): void {
  for (const [budgetId, replayedRow] of replayed.entries()) {
    const storedRow = stored.get(budgetId);
    if (storedRow === undefined) {
      out.push({ kind: "budget_row_missing", budgetId: budgetId as BudgetId });
      continue;
    }
    if (storedRow.scope !== replayedRow.scope) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "scope",
        replayed: replayedRow.scope,
        stored: storedRow.scope,
      });
    }
    if (storedRow.subjectId !== replayedRow.subjectId) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "subjectId",
        replayed: replayedRow.subjectId,
        stored: storedRow.subjectId,
      });
    }
    if (storedRow.limitMicroUsd !== replayedRow.limitMicroUsd) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "limitMicroUsd",
        replayed: replayedRow.limitMicroUsd,
        stored: storedRow.limitMicroUsd,
      });
    }
    const storedThresholds = parseStoredThresholds(storedRow.thresholdsBps);
    if ("error" in storedThresholds) {
      // Corrupted/unparseable `thresholds_bps` projection cell. A bare
      // `JSON.parse` here used to throw out of `runReplayCheck`,
      // blinding the diagnostic; surface a structured discrepancy so
      // on-call sees the exact budget + reason and the rest of the
      // check still runs.
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "thresholdsBps",
        replayed: replayedRow.thresholdsBps,
        stored: { unparseable: storedThresholds.error, raw: storedRow.thresholdsBps },
      });
    } else if (!arraysEqual(storedThresholds, replayedRow.thresholdsBps)) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "thresholdsBps",
        replayed: replayedRow.thresholdsBps,
        stored: storedThresholds,
      });
    }
    if (storedRow.setAtLsn !== replayedRow.setAtLsn) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "setAtLsn",
        replayed: replayedRow.setAtLsn,
        stored: storedRow.setAtLsn,
      });
    }
    if ((storedRow.tombstoned === 1) !== replayedRow.tombstoned) {
      out.push({
        kind: "budget_state_mismatch",
        budgetId: budgetId as BudgetId,
        field: "tombstoned",
        replayed: replayedRow.tombstoned,
        stored: storedRow.tombstoned === 1,
      });
    }
  }
  for (const [budgetId] of stored.entries()) {
    if (!replayed.has(budgetId)) {
      out.push({ kind: "budget_row_ghost", budgetId: budgetId as BudgetId });
    }
  }
}
