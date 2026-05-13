// Helpers shared by 2+ modules in this directory.
//
// Strict membership rule: an item belongs here only if at least two
// sibling modules import it. Single-consumer items live next to the
// code that uses them so the grouping stays cohesive instead of
// becoming a junk drawer.

export interface BudgetDbRow {
  readonly budgetId: string;
  readonly scope: string;
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: string;
  readonly setAtLsn: number;
  readonly tombstoned: number;
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
