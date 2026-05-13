// Replay-check drift detector.
//
// Streams every cost.* event out of `event_log` from LSN 0, re-projects
// into in-memory aggregates, then compares against the live projection
// rows in `cost_by_agent`, `cost_by_task`, `cost_budgets`, and
// `cost_threshold_crossings`. If the projection has drifted from the
// event log (replayed sum != stored sum, missing budget, ghost crossing,
// etc.), the report enumerates each discrepancy.
//
// §15.A's two decidable invariants are verifiable in production by this
// single read-only call:
//   I1. sum(cost_events) == sum(cost_by_agent across all days)
//   I2. sum(task-attributed cost_events) == sum(cost_by_task)
// The HTTP surface wires it as GET /api/v1/cost/replay-check; taskless
// cost events skip the task projection, so I2 is scoped to the
// task-attributed subset.
//
// Read-only: this function never writes. It is safe to call on a live
// broker; the entire check runs inside a single `BEGIN DEFERRED`
// transaction so concurrent writers can commit but won't be observed
// mid-check. better-sqlite3's WAL does NOT give per-statement
// snapshots on its own — the explicit transaction is what produces
// the consistent read view.

import {
  type AuditEventKind,
  type BudgetId,
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadFromJsonValue,
  type EventLsn,
  lsnFromV1Number,
  MAX_BUDGET_LIMIT_MICRO_USD,
  type MicroUsd,
  parseLsn,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import {
  addBudgetToIndex,
  type BudgetCandidateIndexes,
  createBudgetCandidateIndexes,
  removeBudgetFromIndex,
  replaceBudgetInIndex,
} from "./replay-check/budget-candidate-index.ts";
import type {
  ReplayCheckReport,
  ReplayDiscrepancy,
  ReplayedBudget,
  ReplayedCrossing,
} from "./replay-check/discrepancy.ts";
import {
  type AgentDayDbRow,
  agentDayKey,
  arraysEqual,
  BATCH_SIZE,
  type BudgetDbRow,
  type CostEventBatchRow,
  crossingKey,
  eventTypeToKind,
  type HighestLsnRow,
  parseStoredThresholds,
  splitAgentDayKey,
  type TaskDbRow,
  type ThresholdCrossingDbRow,
} from "./replay-check/internal.ts";
import {
  type ComputeExpectedCrossingsArgs,
  computeExpectedCrossings,
  crossesThresholdBigInt,
  type ExpectedCrossing,
} from "./replay-check/threshold-oracle.ts";
import {
  flagUnsafeAccumulator,
  MAX_BUDGET_LIMIT_MICRO_USD_BIG,
  MAX_SAFE_INTEGER_BIG,
} from "./replay-check/unsafe-lifetime-accumulator.ts";

export type {
  ReplayCheckReport,
  ReplayDiscrepancy,
  ReplayedBudget,
} from "./replay-check/discrepancy.ts";

export function runReplayCheck(db: Database.Database): ReplayCheckReport {
  const readBatchStmt = db.prepare<[number, number], CostEventBatchRow>(
    `SELECT lsn, type, payload FROM event_log
     WHERE lsn > ? AND type IN ('cost.event', 'cost.budget.set', 'cost.budget.threshold.crossed')
     ORDER BY lsn ASC
     LIMIT ?`,
  );
  const highestLsnStmt = db.prepare<[], HighestLsnRow>(
    "SELECT COALESCE(MAX(lsn), 0) AS lsn FROM event_log",
  );
  // `safeIntegers(true)` makes better-sqlite3 return INTEGER columns
  // as bigint instead of JS number. Required for the aggregate-totals
  // path: cumulative spend past `Number.MAX_SAFE_INTEGER` (or a hostile
  // projection row past that boundary) would silently round and the
  // diagnostic would report the rounded value instead of the bytes
  // actually in the row.
  const listAgentDaysStmt = db
    .prepare<[], AgentDayDbRow>(
      `SELECT agent_slug AS agentSlug, day_utc AS dayUtc, total_micro_usd AS totalMicroUsd
     FROM cost_by_agent`,
    )
    .safeIntegers(true);
  const listTasksStmt = db
    .prepare<[], TaskDbRow>(
      `SELECT task_id AS taskId, total_micro_usd AS totalMicroUsd FROM cost_by_task`,
    )
    .safeIntegers(true);
  const listBudgetsStmt = db.prepare<[], BudgetDbRow>(
    `SELECT budget_id AS budgetId, scope, subject_id AS subjectId,
            limit_micro_usd AS limitMicroUsd, thresholds_bps AS thresholdsBps,
            set_at_lsn AS setAtLsn, tombstoned
     FROM cost_budgets`,
  );
  // `observed_micro_usd` is CAST to TEXT so a hostile projection row
  // past `Number.MAX_SAFE_INTEGER` flows through as exact decimal
  // bytes instead of silently rounding through JS `number`. The other
  // INTEGER columns are bounded (LSNs, thresholdBps, limitMicroUsd ≤
  // MicroUsd brand) and stay as `number` for ergonomic compares.
  const listCrossingsStmt = db.prepare<[], ThresholdCrossingDbRow>(
    `SELECT budget_id AS budgetId, budget_set_lsn AS budgetSetLsn,
            threshold_bps AS thresholdBps, crossed_at_lsn AS crossedAtLsn,
            CAST(observed_micro_usd AS TEXT) AS observedMicroUsd,
            limit_micro_usd AS limitMicroUsd
     FROM cost_threshold_crossings`,
  );

  // Run all reads inside a single SQLite snapshot so concurrent
  // writers can't shift state mid-check. better-sqlite3's WAL does
  // not give per-statement snapshots; only an explicit transaction
  // does. `.deferred()` acquires a SHARED lock on first read and
  // holds it through the rest of the function; concurrent writers
  // can still
  // commit but this function won't observe them, so replay-vs-stored
  // can't mix snapshots.
  const txn = db.transaction((): ReplayCheckReport => {
    // Aggregate accumulators run in bigint to mirror the lifetime-oracle
    // fix. `total_micro_usd` is cumulative; past `Number.MAX_SAFE_INTEGER`
    // a `number` accumulator silently rounds, and the diagnostic (which
    // emits these totals on the wire) would carry the rounded value.
    const replayedAgentDays = new Map<string, bigint>();
    const replayedTasks = new Map<string, bigint>();
    const replayedBudgets = new Map<string, ReplayedBudget>();
    const budgetIndexes = createBudgetCandidateIndexes();
    // Logged crossings: one entry per (budgetId, budgetSetLsn,
    // thresholdBps) key, BUT each entry carries every event_log row
    // that claimed the key. The projection PK suppresses duplicate
    // rows, so a duplicate threshold-crossed event would silently
    // collapse — without this we'd miss reactor over-emission and
    // forged duplicates. `compareCrossings` continues to compare on
    // the first entry per key for log-vs-projection drift; the new
    // duplicate check fires when `length > 1`.
    const replayedCrossings = new Map<string, ReplayedCrossing[]>();
    // Cost-event triggering timestamps, keyed by cost-event LSN, so
    // `crossedAt` can be validated against the actual triggering event
    // (the reactor sets it from `input.occurredAt`). Stored as ms-
    // since-epoch for cheap comparison.
    const costEventOccurredAtMs = new Map<number, number>();
    // Budget-set LSN → budgetId map for validating logged threshold
    // events' `budgetSetLsn` references — sibling to the
    // `costEventOccurredAtMs` check for `crossedAtLsn`.
    const budgetSetLsnToBudgetId = new Map<number, string>();
    // Oracle: independent threshold replay. Tracks the same lifetime
    // sums the reactor reads (`lifetimeFor` in reactor.ts), then
    // re-runs the same integer/BigInt crossing math against current
    // `replayedBudgets` to derive an expected crossing set without
    // consulting the event log. Discrepancies between
    // `expectedCrossings` and `replayedCrossings` surface reactor bugs
    // (under-emission, spurious, field drift, duplicate, causal order)
    // that the event-log-as-oracle replay can't detect.
    //
    // Lifetime accumulators are `bigint` so the cumulative integer sum
    // stays exact past `Number.MAX_SAFE_INTEGER` (≈ 9e15 microUsd ≈
    // $9B). With `number` accumulators the post-update value rounds at
    // the safe-integer boundary; `BigInt(number)` then carries the
    // rounded value forward, so the BigInt threshold math agrees with
    // a corrupted observed total. See discrepancy
    // `unsafe_lifetime_accumulator` for the structured diagnostic.
    let oracleGlobalLifetime = 0n;
    const oracleAgentLifetime = new Map<string, bigint>();
    const oracleTaskLifetime = new Map<string, bigint>();
    // Track which (scope, subjectId) accumulator keys have already been
    // flagged unsafe so the discrepancy fires once per key per run.
    const unsafeAccumulatorFlagged = new Set<string>();
    const expectedCrossings = new Map<string, ExpectedCrossing>();
    // Discrepancies surfaced inline as the scan loop walks event_log:
    // per-row parse failures and oracle-state boundary signals
    // (`unsafe_lifetime_accumulator`). Merged into the final
    // `discrepancies` list before the downstream comparators run so
    // on-call sees root-cause inline failures ahead of any
    // sum-mismatch discrepancies they may transitively trigger.
    const inlineDiscrepancies: ReplayDiscrepancy[] = [];

    let cursor = 0;
    let scanned = 0;
    for (;;) {
      const rows = readBatchStmt.all(cursor, BATCH_SIZE);
      if (rows.length === 0) break;
      for (const row of rows) {
        const kind = eventTypeToKind(row.type);
        try {
          if (kind === "cost_event") {
            const parsed = costAuditPayloadFromJsonValue(
              kind,
              JSON.parse(row.payload.toString("utf8")),
            ) as CostEventAuditPayload;
            const occurredAtMs = parsed.occurredAt.getTime();
            costEventOccurredAtMs.set(row.lsn, occurredAtMs);
            const dayUtc = parsed.occurredAt.toISOString().slice(0, 10);
            const agentKey = agentDayKey(parsed.agentSlug, dayUtc);
            const amount = parsed.amountMicroUsd as number;
            const amountBig = BigInt(amount);
            replayedAgentDays.set(agentKey, (replayedAgentDays.get(agentKey) ?? 0n) + amountBig);
            if (parsed.taskId !== undefined) {
              replayedTasks.set(
                parsed.taskId,
                (replayedTasks.get(parsed.taskId) ?? 0n) + amountBig,
              );
            }
            // Oracle update: mirror the reactor's post-update lifetime
            // read. The reactor runs after `cost_by_agent` /
            // `cost_by_task` have been updated for the current event,
            // so `observed` includes the event's own amount. Increments
            // run in BigInt so cumulative spend stays exact past
            // `Number.MAX_SAFE_INTEGER` (`amountBig` declared above for
            // the aggregate accumulators is reused here).
            oracleGlobalLifetime += amountBig;
            flagUnsafeAccumulator(
              "global",
              null,
              oracleGlobalLifetime,
              row.lsn,
              unsafeAccumulatorFlagged,
              inlineDiscrepancies,
            );
            const agentPost = (oracleAgentLifetime.get(parsed.agentSlug) ?? 0n) + amountBig;
            oracleAgentLifetime.set(parsed.agentSlug, agentPost);
            flagUnsafeAccumulator(
              "agent",
              parsed.agentSlug,
              agentPost,
              row.lsn,
              unsafeAccumulatorFlagged,
              inlineDiscrepancies,
            );
            if (parsed.taskId !== undefined) {
              const taskPost = (oracleTaskLifetime.get(parsed.taskId) ?? 0n) + amountBig;
              oracleTaskLifetime.set(parsed.taskId, taskPost);
              flagUnsafeAccumulator(
                "task",
                parsed.taskId,
                taskPost,
                row.lsn,
                unsafeAccumulatorFlagged,
                inlineDiscrepancies,
              );
            }
            computeExpectedCrossings({
              costEventLsn: row.lsn,
              costEventOccurredAtMs: occurredAtMs,
              agentSlug: parsed.agentSlug,
              taskId: parsed.taskId,
              budgets: replayedBudgets,
              globalBudgetIds: budgetIndexes.globalBudgetIds,
              agentBudgetIds: budgetIndexes.agentBudgetIds,
              taskBudgetIds: budgetIndexes.taskBudgetIds,
              globalLifetime: oracleGlobalLifetime,
              agentLifetime: oracleAgentLifetime,
              taskLifetime: oracleTaskLifetime,
              out: expectedCrossings,
            });
          } else if (kind === "budget_set") {
            const parsed = costAuditPayloadFromJsonValue(
              kind,
              JSON.parse(row.payload.toString("utf8")),
            ) as BudgetSetAuditPayload;
            const previous = replayedBudgets.get(parsed.budgetId);
            const replayedBudget: ReplayedBudget = {
              scope: parsed.scope,
              subjectId: parsed.subjectId ?? null,
              limitMicroUsd: parsed.limitMicroUsd as number,
              thresholdsBps: [...parsed.thresholdsBps],
              setAtLsn: row.lsn,
              tombstoned: (parsed.limitMicroUsd as number) === 0,
            };
            replayedBudgets.set(parsed.budgetId, replayedBudget);
            replaceBudgetInIndex(budgetIndexes, parsed.budgetId, previous, replayedBudget);
            budgetSetLsnToBudgetId.set(row.lsn, parsed.budgetId);
          } else if (kind === "budget_threshold_crossed") {
            const parsed = costAuditPayloadFromJsonValue(
              kind,
              JSON.parse(row.payload.toString("utf8")),
            ) as BudgetThresholdCrossedAuditPayload;
            const budgetSetLsnInt = parseLsn(parsed.budgetSetLsn).localLsn;
            const crossedAtLsnInt = parseLsn(parsed.crossedAtLsn).localLsn;
            const key = crossingKey(parsed.budgetId, budgetSetLsnInt, parsed.thresholdBps);
            const entry: ReplayedCrossing = {
              budgetId: parsed.budgetId,
              budgetSetLsn: budgetSetLsnInt,
              thresholdBps: parsed.thresholdBps,
              crossedAtLsn: crossedAtLsnInt,
              observedMicroUsd: parsed.observedMicroUsd as number,
              limitMicroUsd: parsed.limitMicroUsd as number,
              crossedAtMs: parsed.crossedAt.getTime(),
              eventLsn: row.lsn,
            };
            const existing = replayedCrossings.get(key);
            if (existing === undefined) {
              replayedCrossings.set(key, [entry]);
            } else {
              existing.push(entry);
            }
          }
        } catch (err) {
          inlineDiscrepancies.push({
            kind: "event_payload_unparseable",
            lsn: lsnFromV1Number(row.lsn),
            type: row.type,
            reason: err instanceof Error ? err.message : String(err),
          });
        }
        scanned += 1;
      }
      cursor = rows[rows.length - 1]?.lsn ?? cursor;
    }

    // Inline scan-loop discrepancies (parse failures, accumulator
    // boundary signals) are surfaced first so on-call sees root-cause
    // failures at the top of the list, ahead of any downstream
    // sum-mismatch comparators that may have been triggered by the
    // same underlying corruption.
    const discrepancies: ReplayDiscrepancy[] = [...inlineDiscrepancies];

    const storedAgentDays = new Map<string, bigint>();
    for (const row of listAgentDaysStmt.all()) {
      storedAgentDays.set(agentDayKey(row.agentSlug, row.dayUtc), row.totalMicroUsd);
    }
    compareAgentDays(replayedAgentDays, storedAgentDays, discrepancies);

    const storedTasks = new Map<string, bigint>();
    for (const row of listTasksStmt.all()) {
      storedTasks.set(row.taskId, row.totalMicroUsd);
    }
    compareTasks(replayedTasks, storedTasks, discrepancies);

    const storedBudgets = new Map<string, BudgetDbRow>();
    for (const row of listBudgetsStmt.all()) {
      storedBudgets.set(row.budgetId, row);
    }
    compareBudgets(replayedBudgets, storedBudgets, discrepancies);

    const storedCrossings = new Map<string, ThresholdCrossingDbRow>();
    for (const row of listCrossingsStmt.all()) {
      storedCrossings.set(crossingKey(row.budgetId, row.budgetSetLsn, row.thresholdBps), row);
    }
    compareCrossings(replayedCrossings, storedCrossings, discrepancies);
    // Oracle checks: duplicates, causal-ordering, then the
    // expected-vs-logged comparison. Order matters for on-call: a
    // duplicate or causal-order violation is a more pointed signal
    // than an oracle mismatch, so surfacing those first keeps the
    // most actionable findings at the top of the list.
    detectDuplicateLoggedCrossings(replayedCrossings, discrepancies);
    detectCausalOrderViolations(replayedCrossings, discrepancies);
    // Per-entry reference validation: every logged threshold event
    // names a `crossedAtLsn`; this checks each one against the actual
    // cost-event LSN→occurredAt map. Catches forged dangling refs
    // (LSN not a cost.event) and per-duplicate `crossedAt` drift that
    // the first-entry-only oracle comparator can't see.
    validateLoggedCrossingReferences(replayedCrossings, costEventOccurredAtMs, discrepancies);
    validateBudgetSetReferences(replayedCrossings, budgetSetLsnToBudgetId, discrepancies);
    // Contiguous-emission window check: enforces the reactor's
    // same-transaction invariant. Catches forgeries that pass every
    // other check by being appended much later than the cost.event.
    detectDelayedEmissions(replayedCrossings, expectedCrossings, discrepancies);
    compareExpectedAndLoggedCrossings(expectedCrossings, replayedCrossings, discrepancies);

    const highest = highestLsnStmt.get()?.lsn ?? 0;
    return {
      ok: discrepancies.length === 0,
      highestLsn: lsnFromV1Number(highest),
      eventsScanned: scanned,
      discrepancies,
    };
  });
  return txn.deferred();
}

// Internal test seam. The end-to-end path that exercises
// `unsafe_lifetime_accumulator` requires pushing an oracle accumulator
// past 2^53 microUsd (≈ $9B cumulative spend). Per-event amounts are
// capped at `MAX_COST_EVENT_AMOUNT_MICRO_USD` (1e8) by the protocol
// validator, so reaching the boundary through `runReplayCheck` would
// need ~9e7 cost.events — infeasible in a unit test. These exports
// let tests assert the helper's emission logic directly. NOT part of
// `@wuphf/broker/cost-ledger`'s public surface (the index re-exports
// only `ReplayCheckReport`, `ReplayDiscrepancy`, and `runReplayCheck`).
// Frozen so a test cannot mutate the seam and pollute subsequent
// imports (modules are singletons).
export const __replayCheckTesting = Object.freeze({
  flagUnsafeAccumulator,
  MAX_SAFE_INTEGER_BIG,
  MAX_BUDGET_LIMIT_MICRO_USD_BIG,
  crossesThresholdBigInt,
  // Budget-candidate-index helpers exposed for direct assertion:
  // the existing `eventsScanned > 1_000` regression test would still
  // satisfy a revert to the O(events × budgets) iteration, so tests
  // need to inspect the index shape directly. `computeExpectedCrossings`
  // is exposed so the hot path can be tested with a hostile Proxy
  // that blocks universe iteration. `replaceBudgetInIndex` is
  // exposed so the lifecycle-invariant tests can exercise the
  // remove-then-add semantics directly without going through the
  // full replay loop.
  addBudgetToIndex,
  removeBudgetFromIndex,
  replaceBudgetInIndex,
  computeExpectedCrossings,
  // Aggregate compare functions exposed so tests can drive them with
  // synthetic bigint maps past `MAX_BUDGET_LIMIT_MICRO_USD` and
  // `Number.MAX_SAFE_INTEGER`. End-to-end coverage past 2^53 is
  // blocked by the protocol per-event cap
  // (`MAX_COST_EVENT_AMOUNT_MICRO_USD = 1e8`), so a direct seam is
  // the only way to assert the boundary emission shape.
  compareAgentDays,
  compareTasks,
});

function compareCrossings(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  stored: ReadonlyMap<string, ThresholdCrossingDbRow>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, entries] of replayed.entries()) {
    // The projection PK collapses duplicate audit events into one row,
    // so log-vs-projection comparison uses the first event seen. The
    // duplicate case is surfaced separately by
    // `detectDuplicateLoggedCrossings`.
    const expected = entries[0];
    if (expected === undefined) continue; // unreachable: map only holds non-empty arrays
    const actual = stored.get(key);
    if (actual === undefined) {
      out.push({
        kind: "threshold_crossing_missing",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
      });
      continue;
    }
    if (actual.crossedAtLsn !== expected.crossedAtLsn) {
      out.push({
        kind: "threshold_crossing_field_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        field: "crossedAtLsn",
        replayed: expected.crossedAtLsn,
        stored: actual.crossedAtLsn,
      });
    }
    // Stored side is a decimal string from `CAST(...)`; widen the
    // protocol-bounded replayed side via `String(...)` for byte-exact
    // comparison. Equal-on-bytes implies equal-as-integers.
    const expectedObservedString = String(expected.observedMicroUsd);
    if (actual.observedMicroUsd !== expectedObservedString) {
      out.push({
        kind: "threshold_crossing_observed_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        replayedMicroUsdString: expectedObservedString,
        storedMicroUsdString: actual.observedMicroUsd,
      });
    }
    if (actual.limitMicroUsd !== expected.limitMicroUsd) {
      out.push({
        kind: "threshold_crossing_field_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        field: "limitMicroUsd",
        replayed: expected.limitMicroUsd,
        stored: actual.limitMicroUsd,
      });
    }
  }
  for (const [key, actual] of stored.entries()) {
    if (!replayed.has(key)) {
      out.push({
        kind: "threshold_crossing_ghost",
        budgetId: actual.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(actual.budgetSetLsn),
        thresholdBps: actual.thresholdBps,
      });
    }
  }
}

function detectDuplicateLoggedCrossings(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    if (entries.length <= 1) continue;
    const first = entries[0];
    if (first === undefined) continue; // unreachable
    out.push({
      kind: "threshold_crossing_duplicate_event",
      budgetId: first.budgetId as BudgetId,
      budgetSetLsn: lsnFromV1Number(first.budgetSetLsn),
      thresholdBps: first.thresholdBps,
      eventLsns: entries.map((e) => lsnFromV1Number(e.eventLsn)),
    });
  }
}

function detectCausalOrderViolations(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      // The reactor appends `cost.budget.threshold.crossed` AFTER its
      // triggering `cost.event` in the same SQLite transaction, so the
      // threshold event's own LSN must strictly exceed the LSN it
      // names as the trigger.
      if (entry.eventLsn <= entry.crossedAtLsn) {
        out.push({
          kind: "threshold_crossing_causal_order_violation",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
        });
      }
    }
  }
}

function validateBudgetSetReferences(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  budgetSetLsnToBudgetId: ReadonlyMap<number, string>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      const actualBudgetId = budgetSetLsnToBudgetId.get(entry.budgetSetLsn);
      if (actualBudgetId === undefined) {
        out.push({
          kind: "threshold_crossing_invalid_budget_set_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          reason: "lsn_not_a_budget_set",
        });
        continue;
      }
      if (actualBudgetId !== entry.budgetId) {
        out.push({
          kind: "threshold_crossing_invalid_budget_set_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          reason: "budget_id_mismatch",
          actualBudgetId: actualBudgetId as BudgetId,
        });
      }
    }
  }
}

function detectDelayedEmissions(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  expected: ReadonlyMap<string, ExpectedCrossing>,
  out: ReplayDiscrepancy[],
): void {
  // Count expected crossings per triggering cost-event LSN. That count
  // IS the size of the contiguous emission window the reactor would
  // produce in the same SQLite transaction as the cost.event.
  const expectedCountByCostEventLsn = new Map<number, number>();
  for (const exp of expected.values()) {
    expectedCountByCostEventLsn.set(
      exp.crossedAtLsn,
      (expectedCountByCostEventLsn.get(exp.crossedAtLsn) ?? 0) + 1,
    );
  }
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      // Spurious entries (no expected crossing for this key) are
      // surfaced by `compareExpectedAndLoggedCrossings`; skip them
      // here to avoid double-noise.
      const key = crossingKey(entry.budgetId, entry.budgetSetLsn, entry.thresholdBps);
      if (!expected.has(key)) continue;
      const expectedCount = expectedCountByCostEventLsn.get(entry.crossedAtLsn) ?? 0;
      if (expectedCount === 0) continue; // unreachable: expected.has(key) implies the count is ≥1
      const minLsn = entry.crossedAtLsn + 1;
      const maxLsn = entry.crossedAtLsn + expectedCount;
      if (entry.eventLsn < minLsn || entry.eventLsn > maxLsn) {
        out.push({
          kind: "threshold_crossing_delayed_emission",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
          expectedWindowMinLsn: lsnFromV1Number(minLsn),
          expectedWindowMaxLsn: lsnFromV1Number(maxLsn),
        });
      }
    }
  }
}

function validateLoggedCrossingReferences(
  replayed: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  costEventOccurredAtMs: ReadonlyMap<number, number>,
  out: ReplayDiscrepancy[],
): void {
  for (const entries of replayed.values()) {
    for (const entry of entries) {
      const referencedMs = costEventOccurredAtMs.get(entry.crossedAtLsn);
      if (referencedMs === undefined) {
        // The named trigger LSN doesn't correspond to any cost.event
        // we replayed. Either the threshold event names a non-cost
        // row (e.g. a `cost.budget.set` LSN) or an LSN that doesn't
        // exist in event_log.
        out.push({
          kind: "threshold_crossing_dangling_reference",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          eventLsn: lsnFromV1Number(entry.eventLsn),
          crossedAtLsn: lsnFromV1Number(entry.crossedAtLsn),
        });
        continue;
      }
      if (referencedMs !== entry.crossedAtMs) {
        // The threshold event's payload `crossedAt` doesn't match the
        // cost.event it names as trigger. Catches forged timestamps
        // (key matches but timestamp drifts) AND per-duplicate
        // divergence the first-entry-only oracle comparator misses.
        out.push({
          kind: "threshold_crossing_oracle_field_mismatch",
          budgetId: entry.budgetId as BudgetId,
          budgetSetLsn: lsnFromV1Number(entry.budgetSetLsn),
          thresholdBps: entry.thresholdBps,
          field: "crossedAtMs",
          expected: referencedMs,
          logged: entry.crossedAtMs,
          eventLsn: lsnFromV1Number(entry.eventLsn),
        });
      }
    }
  }
}

// BudgetCandidateIndexes + helpers live in their own module so this
// file stays under the 1500-LOC limit. The test-only constructor is
// re-exported via that module; the rest of the helpers are used here.
export {
  __createBudgetCandidateIndexesForTesting,
  type BudgetCandidateIndexes,
} from "./replay-check/budget-candidate-index.ts";

// Compare functions accept and emit bigint cumulative totals. The
// discrepancy wire shape is a decimal-string form to preserve exact
// values past the `MicroUsd` brand bound or the safe-integer boundary.
function compareAgentDays(
  replayed: ReadonlyMap<string, bigint>,
  stored: ReadonlyMap<string, bigint>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, replayedTotal] of replayed.entries()) {
    const storedTotal = stored.get(key);
    const { agentSlug, dayUtc } = splitAgentDayKey(key);
    if (storedTotal === undefined) {
      out.push({
        kind: "agent_day_row_missing",
        agentSlug,
        dayUtc,
        replayedMicroUsdString: replayedTotal.toString(),
      });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "agent_day_total_mismatch",
        agentSlug,
        dayUtc,
        replayedMicroUsdString: replayedTotal.toString(),
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
  for (const [key, storedTotal] of stored.entries()) {
    if (!replayed.has(key)) {
      const { agentSlug, dayUtc } = splitAgentDayKey(key);
      out.push({
        kind: "agent_day_row_ghost",
        agentSlug,
        dayUtc,
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
}

function compareTasks(
  replayed: ReadonlyMap<string, bigint>,
  stored: ReadonlyMap<string, bigint>,
  out: ReplayDiscrepancy[],
): void {
  for (const [taskId, replayedTotal] of replayed.entries()) {
    const storedTotal = stored.get(taskId);
    if (storedTotal === undefined) {
      out.push({
        kind: "task_row_missing",
        taskId,
        replayedMicroUsdString: replayedTotal.toString(),
      });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "task_total_mismatch",
        taskId,
        replayedMicroUsdString: replayedTotal.toString(),
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
  for (const [taskId, storedTotal] of stored.entries()) {
    if (!replayed.has(taskId)) {
      out.push({
        kind: "task_row_ghost",
        taskId,
        storedMicroUsdString: storedTotal.toString(),
      });
    }
  }
}

function compareBudgets(
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

function compareExpectedAndLoggedCrossings(
  expected: ReadonlyMap<string, ExpectedCrossing>,
  logged: ReadonlyMap<string, readonly ReplayedCrossing[]>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, exp] of expected.entries()) {
    const entries = logged.get(key);
    // Compare against the first logged event per key. Duplicates are
    // surfaced by `detectDuplicateLoggedCrossings` so the
    // oracle-vs-log comparator focuses on shape drift.
    const log = entries?.[0];
    if (log === undefined) {
      out.push({
        kind: "threshold_crossing_unemitted",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        observedMicroUsdString: exp.observedMicroUsd.toString(),
        limitMicroUsd: exp.limitMicroUsd as MicroUsd,
        crossedAtLsn: lsnFromV1Number(exp.crossedAtLsn),
      });
      continue;
    }
    if (log.crossedAtLsn !== exp.crossedAtLsn) {
      out.push({
        kind: "threshold_crossing_oracle_field_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        field: "crossedAtLsn",
        expected: exp.crossedAtLsn,
        logged: log.crossedAtLsn,
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    // Compare cumulative observed in bigint to preserve precision past
    // the safe-integer boundary. Logged is the protocol-validated audit
    // value (number, ≤ MAX_BUDGET_LIMIT_MICRO_USD); widen to bigint for
    // an exact compare against the oracle's unbounded accumulator.
    const loggedObservedBig = BigInt(log.observedMicroUsd);
    if (loggedObservedBig !== exp.observedMicroUsd) {
      out.push({
        kind: "threshold_crossing_oracle_observed_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        expectedMicroUsdString: exp.observedMicroUsd.toString(),
        loggedMicroUsdString: loggedObservedBig.toString(),
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    if (log.limitMicroUsd !== exp.limitMicroUsd) {
      out.push({
        kind: "threshold_crossing_oracle_field_mismatch",
        budgetId: exp.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(exp.budgetSetLsn),
        thresholdBps: exp.thresholdBps,
        field: "limitMicroUsd",
        expected: exp.limitMicroUsd,
        logged: log.limitMicroUsd,
        eventLsn: lsnFromV1Number(log.eventLsn),
      });
    }
    // `crossedAtMs` is validated per-entry against the cost-event LSN
    // map in `validateLoggedCrossingReferences`, which also covers
    // duplicates and spurious entries that this comparator skips.
  }
  for (const [key, entries] of logged.entries()) {
    if (expected.has(key)) continue;
    const log = entries[0];
    if (log === undefined) continue; // unreachable
    out.push({
      kind: "threshold_crossing_spurious",
      budgetId: log.budgetId as BudgetId,
      budgetSetLsn: lsnFromV1Number(log.budgetSetLsn),
      thresholdBps: log.thresholdBps,
      observedMicroUsdString: log.observedMicroUsd.toString(),
      limitMicroUsd: log.limitMicroUsd as MicroUsd,
      crossedAtLsn: lsnFromV1Number(log.crossedAtLsn),
      eventLsn: lsnFromV1Number(log.eventLsn),
    });
  }
}
