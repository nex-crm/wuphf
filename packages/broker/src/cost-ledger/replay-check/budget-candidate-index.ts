// Budget candidate indexes for the replay-check threshold oracle.
//
// `computeExpectedCrossings` (in replay-check.ts) used to iterate every
// replayed budget per cost event — O(events × budgets) on long-lived
// ledgers with many tombstoned budgets. These indexes keep three
// scope-keyed candidate sets in sync with `replayedBudgets`:
//
//   - globalBudgetIds: all live `scope === "global"` budgets
//   - agentBudgetIds[slug]: live `scope === "agent"` budgets per slug
//   - taskBudgetIds[taskId]: live `scope === "task"` budgets per id
//
// A budget moves between scopes/subjects via re-issued `cost.budget.set`
// events; `replaceBudgetInIndex` owns the remove-then-add lifecycle so
// a budget is never in two scope sets at once. The disjointness
// invariant is what lets the hot path iterate the three matching sets
// without dedupe.
//
// Extracted from replay-check.ts so that file stays under the
// 1500-LOC file-size limit (see scripts/file-size-allowlist.txt).
import type { ReplayedBudget } from "./discrepancy.ts";

export interface BudgetCandidateIndexes {
  readonly globalBudgetIds: Set<string>;
  readonly agentBudgetIds: Map<string, Set<string>>;
  readonly taskBudgetIds: Map<string, Set<string>>;
}

export function createBudgetCandidateIndexes(): BudgetCandidateIndexes {
  return {
    globalBudgetIds: new Set<string>(),
    agentBudgetIds: new Map<string, Set<string>>(),
    taskBudgetIds: new Map<string, Set<string>>(),
  };
}

export function addBudgetToIndex(
  indexes: BudgetCandidateIndexes,
  budgetId: string,
  budget: ReplayedBudget,
): void {
  if (budget.scope === "global") {
    indexes.globalBudgetIds.add(budgetId);
    return;
  }
  if (budget.subjectId === null) return;
  addBudgetToSubjectIndex(
    budget.scope === "agent" ? indexes.agentBudgetIds : indexes.taskBudgetIds,
    budget.subjectId,
    budgetId,
  );
}

export function removeBudgetFromIndex(
  indexes: BudgetCandidateIndexes,
  budgetId: string,
  budget: ReplayedBudget,
): void {
  if (budget.scope === "global") {
    indexes.globalBudgetIds.delete(budgetId);
    return;
  }
  if (budget.subjectId === null) return;
  removeBudgetFromSubjectIndex(
    budget.scope === "agent" ? indexes.agentBudgetIds : indexes.taskBudgetIds,
    budget.subjectId,
    budgetId,
  );
}

// Single-call lifecycle transition. The replay loop calls this on
// every `budget_set` event so a budget moves between scopes/subjects/
// tombstone-states without ever existing in two scope sets at once.
// Tombstones (`next.tombstoned === true`) remove without re-adding;
// the post-condition is "exactly the placement implied by `next`".
export function replaceBudgetInIndex(
  indexes: BudgetCandidateIndexes,
  budgetId: string,
  previous: ReplayedBudget | undefined,
  next: ReplayedBudget,
): void {
  if (previous !== undefined) {
    removeBudgetFromIndex(indexes, budgetId, previous);
  }
  if (next.tombstoned) return;
  addBudgetToIndex(indexes, budgetId, next);
}

function addBudgetToSubjectIndex(
  index: Map<string, Set<string>>,
  subjectId: string,
  budgetId: string,
): void {
  const existing = index.get(subjectId);
  if (existing !== undefined) {
    existing.add(budgetId);
    return;
  }
  index.set(subjectId, new Set<string>([budgetId]));
}

function removeBudgetFromSubjectIndex(
  index: Map<string, Set<string>>,
  subjectId: string,
  budgetId: string,
): void {
  const existing = index.get(subjectId);
  if (existing === undefined) return;
  existing.delete(budgetId);
  if (existing.size === 0) {
    index.delete(subjectId);
  }
}

// Internal test seam. Tests directly assert the candidate-set shape
// after a sequence of `cost.budget.set` events; the existing #842
// regression test only asserted `eventsScanned > 1_000`, which a
// revert to the O(events × budgets) iteration would still satisfy.
// NOT part of `@wuphf/broker/cost-ledger`'s public surface (the index
// re-exports only `ReplayCheckReport`, `ReplayDiscrepancy`, and
// `runReplayCheck`).
export function __createBudgetCandidateIndexesForTesting(): BudgetCandidateIndexes {
  return createBudgetCandidateIndexes();
}
