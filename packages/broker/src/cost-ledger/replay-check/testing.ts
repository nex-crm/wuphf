// Internal test seam for the replay-check modules.
//
// The end-to-end path that exercises `unsafe_lifetime_accumulator`
// requires pushing an oracle accumulator past 2^53 microUsd (≈ $9B
// cumulative spend). Per-event amounts are capped at
// `MAX_COST_EVENT_AMOUNT_MICRO_USD` (1e8) by the protocol validator,
// so reaching the boundary through `runReplayCheck` would need ~9e7
// cost.events — infeasible in a unit test. The aggregate-totals
// `compareAgentDays` / `compareTasks` path has the same boundary
// constraint. And the perf-regression tests need direct access to the
// budget-candidate-index helpers and `computeExpectedCrossings` to
// assert the index lifecycle and the no-universe-iteration invariant
// the orchestrator depends on.
//
// These exports are NOT part of `@wuphf/broker/cost-ledger`'s public
// surface (the package re-exports only `ReplayCheckReport`,
// `ReplayDiscrepancy`, and `runReplayCheck`). Frozen so a test
// cannot mutate the seam and pollute subsequent imports (modules
// are singletons).
import { compareAgentDays, compareTasks } from "./aggregate-comparators.ts";
import {
  addBudgetToIndex,
  removeBudgetFromIndex,
  replaceBudgetInIndex,
} from "./budget-candidate-index.ts";
import { computeExpectedCrossings, crossesThresholdBigInt } from "./threshold-oracle.ts";
import {
  flagUnsafeAccumulator,
  MAX_BUDGET_LIMIT_MICRO_USD_BIG,
  MAX_SAFE_INTEGER_BIG,
} from "./unsafe-lifetime-accumulator.ts";

export type { BudgetCandidateIndexes } from "./budget-candidate-index.ts";
export { __createBudgetCandidateIndexesForTesting } from "./budget-candidate-index.ts";

export const __replayCheckTesting = Object.freeze({
  flagUnsafeAccumulator,
  MAX_SAFE_INTEGER_BIG,
  MAX_BUDGET_LIMIT_MICRO_USD_BIG,
  crossesThresholdBigInt,
  addBudgetToIndex,
  removeBudgetFromIndex,
  replaceBudgetInIndex,
  computeExpectedCrossings,
  compareAgentDays,
  compareTasks,
});
