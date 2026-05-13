// Independent threshold-crossing oracle for a single `cost.event`.
//
// Mirrors the reactor's logic (`processCostEventForCrossings` in
// reactor.ts) but derives `observed` from oracle-maintained lifetime
// trackers rather than reading `cost_by_agent` / `cost_by_task` from
// disk. The crossing math (`crossesThresholdBigInt`) MUST stay
// byte-identical with the reactor's `crossesThreshold`; if they
// diverge, this oracle becomes a tautology.
//
// The orchestrator feeds three scope-keyed candidate indexes
// (`global`, `agent[slug]`, `task[taskId]`) maintained via the
// `budget-candidate-index` module's `replaceBudgetInIndex`, so the
// per-event iteration only visits budgets that could plausibly fire.
// The hot path is asserted by hostile-Proxy tests in
// cost-ledger.spec.ts that block universe iteration and subject-map
// `.entries()` regressions.
import type { ReplayedBudget } from "./discrepancy.ts";
import { crossingKey } from "./internal.ts";

export interface ExpectedCrossing {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  /**
   * Cumulative oracle-observed spend, as `bigint` to preserve precision
   * across the unbounded accumulator range. Emitted to discrepancies
   * via `.toString()` (decimal) to honor the `MicroUsd` brand: cumulative
   * lifetime can exceed `MAX_BUDGET_LIMIT_MICRO_USD` and would forge the
   * brand if cast back to `number`.
   */
  readonly observedMicroUsd: bigint;
  readonly limitMicroUsd: number;
  /** `crossedAt` derived from the triggering cost event, as ms-since-epoch. */
  readonly crossedAtMs: number;
}

export interface ComputeExpectedCrossingsArgs {
  readonly costEventLsn: number;
  readonly costEventOccurredAtMs: number;
  readonly agentSlug: string;
  readonly taskId: string | undefined;
  readonly budgets: ReadonlyMap<string, ReplayedBudget>;
  // Accepts both the live `BudgetCandidateIndexes` (mutable inside the
  // replay loop) and the hostile-Proxy wrappers used by the perf
  // regression tests, which are `ReadonlyMap` / `ReadonlySet` and
  // throw on iterator access to lock in the direct-`.get(slug)` hot
  // path.
  readonly globalBudgetIds: ReadonlySet<string>;
  readonly agentBudgetIds: ReadonlyMap<string, ReadonlySet<string>>;
  readonly taskBudgetIds: ReadonlyMap<string, ReadonlySet<string>>;
  /** BigInt accumulators preserve precision past `Number.MAX_SAFE_INTEGER`. */
  readonly globalLifetime: bigint;
  readonly agentLifetime: ReadonlyMap<string, bigint>;
  readonly taskLifetime: ReadonlyMap<string, bigint>;
  readonly out: Map<string, ExpectedCrossing>;
}

export function computeExpectedCrossings(args: ComputeExpectedCrossingsArgs): void {
  // Visit each scope index directly. The replay loop's
  // `replaceBudgetInIndex` keeps a budget in exactly one scope/subject
  // bucket at a time, so the three iterations below are disjoint and
  // no dedupe is required. This avoids one transient Set allocation
  // and O(events × globalBudgetIds.size) hash inserts per cost event.
  // The disjointness invariant is asserted by the perf-regression test
  // in cost-ledger.spec.ts: a hostile Proxy on `args.budgets` blocks
  // universe iteration, and hostile Proxies on the agent/task scope
  // maps block `.entries()`/`.values()` regressions that would scan
  // subjects rather than `.get(slug)` directly.
  visitCandidates(args, args.globalBudgetIds, evalBudget);
  visitCandidates(args, args.agentBudgetIds.get(args.agentSlug), evalBudget);
  if (args.taskId !== undefined) {
    visitCandidates(args, args.taskBudgetIds.get(args.taskId), evalBudget);
  }
}

function visitCandidates(
  args: ComputeExpectedCrossingsArgs,
  budgetIds: ReadonlySet<string> | undefined,
  visit: (args: ComputeExpectedCrossingsArgs, budgetId: string, budget: ReplayedBudget) => void,
): void {
  if (budgetIds === undefined) return;
  for (const budgetId of budgetIds) {
    const budget = args.budgets.get(budgetId);
    if (budget === undefined) continue;
    visit(args, budgetId, budget);
  }
}

function evalBudget(
  args: ComputeExpectedCrossingsArgs,
  budgetId: string,
  budget: ReplayedBudget,
): void {
  // Defensive guards retained from the original pre-#842 oracle. The
  // index lifecycle (replaceBudgetInIndex) keeps tombstones out of the
  // scope sets and `isOracleApplicable` should always be true on a
  // candidate pulled from its own scope index. Note: these guards only
  // catch STALE/EXTRA entries — they cannot catch the more dangerous
  // failure mode where a live, applicable budget is missing from all
  // candidate indexes (an under-emission, masked here as silent).
  // The lifecycle tests cover that path directly.
  if (budget.tombstoned) return;
  if (!isOracleApplicable(budget, args.agentSlug, args.taskId)) return;
  const observed = oracleObservedFor(budget, args);
  if (observed === 0n && budget.limitMicroUsd === 0) return;
  for (const thresholdBps of budget.thresholdsBps) {
    const key = crossingKey(budgetId, budget.setAtLsn, thresholdBps);
    if (args.out.has(key)) continue;
    if (!crossesThresholdBigInt(observed, budget.limitMicroUsd, thresholdBps)) continue;
    args.out.set(key, {
      budgetId,
      budgetSetLsn: budget.setAtLsn,
      thresholdBps,
      // Keep cumulative `observed` as bigint internally. Discrepancy
      // emission stringifies via `.toString()` to honor the
      // `MicroUsd` brand contract on the wire.
      crossedAtLsn: args.costEventLsn,
      observedMicroUsd: observed,
      limitMicroUsd: budget.limitMicroUsd,
      crossedAtMs: args.costEventOccurredAtMs,
    });
  }
}

function isOracleApplicable(
  budget: ReplayedBudget,
  agentSlug: string,
  taskId: string | undefined,
): boolean {
  if (budget.scope === "global") return true;
  if (budget.scope === "agent") return budget.subjectId === agentSlug;
  if (budget.scope === "task") return taskId !== undefined && budget.subjectId === taskId;
  return false;
}

function oracleObservedFor(budget: ReplayedBudget, args: ComputeExpectedCrossingsArgs): bigint {
  if (budget.scope === "global") return args.globalLifetime;
  if (budget.scope === "agent") return args.agentLifetime.get(args.agentSlug) ?? 0n;
  // task scope — caller already gated on taskId !== undefined.
  if (args.taskId === undefined) return 0n;
  return args.taskLifetime.get(args.taskId) ?? 0n;
}

/**
 * Integer threshold test in BigInt — byte-identical to the reactor's
 * `crossesThreshold` (see reactor.ts:313). The operand bounds make the
 * intermediate product reach 1e16, above Number.MAX_SAFE_INTEGER, so
 * Number-multiplication would silently truncate; BigInt is exact across
 * the full documented range. `observed` is a bigint accumulator (see
 * `unsafe_lifetime_accumulator` for the precision rationale); `limit`
 * is the per-budget cap and stays well within safe-integer range, so
 * widening it inside the function is safe. If you change the reactor's
 * math, change this too — otherwise the oracle becomes a tautology.
 */
export function crossesThresholdBigInt(
  observed: bigint,
  limit: number,
  thresholdBps: number,
): boolean {
  if (limit === 0) return false;
  return observed * 10_000n >= BigInt(limit) * BigInt(thresholdBps);
}
