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
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadFromJsonValue,
  lsnFromV1Number,
  parseLsn,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";
import { compareAgentDays, compareTasks } from "./aggregate-comparators.ts";
import { createBudgetCandidateIndexes, replaceBudgetInIndex } from "./budget-candidate-index.ts";
import { compareBudgets } from "./budget-comparator.ts";
import {
  compareCrossings,
  compareExpectedAndLoggedCrossings,
  detectCausalOrderViolations,
  detectDelayedEmissions,
  detectDuplicateLoggedCrossings,
  validateBudgetSetReferences,
  validateLoggedCrossingReferences,
} from "./crossing-comparators.ts";
import type {
  ReplayCheckReport,
  ReplayDiscrepancy,
  ReplayedBudget,
  ReplayedCrossing,
} from "./discrepancy.ts";
import { type BudgetDbRow, crossingKey, type ThresholdCrossingDbRow } from "./internal.ts";
import { computeExpectedCrossings, type ExpectedCrossing } from "./threshold-oracle.ts";
import { flagUnsafeAccumulator } from "./unsafe-lifetime-accumulator.ts";

// Public surface for the `cost-ledger/replay-check` subsystem. The
// package barrel (`cost-ledger/index.ts`) narrows further to just
// these three symbols on the `@wuphf/broker/cost-ledger` subpath.
// Internal state types (`ReplayedBudget`, `ReplayedCrossing`,
// `BudgetCandidateIndexes`, the DB row shapes, the threshold-oracle
// inputs) stay internal; tests reach them via `./testing.ts`.
export type { ReplayCheckReport, ReplayDiscrepancy } from "./discrepancy.ts";

// Orchestrator-private types and helpers. The prepared statements
// below own the DB row shapes; the per-batch read size and the
// agent-day key encoder are only used during the scan loop.

const BATCH_SIZE = 1_000;

interface CostEventBatchRow {
  readonly lsn: number;
  readonly type: string;
  readonly payload: Buffer;
}

interface AgentDayDbRow {
  readonly agentSlug: string;
  readonly dayUtc: string;
  // Aggregate totals are read with `.safeIntegers(true)` so a hostile
  // INTEGER value in the projection (past `Number.MAX_SAFE_INTEGER`)
  // doesn't silently round in the diagnostic — the "diagnostic of last
  // resort" preserves exact bytes from the row.
  readonly totalMicroUsd: bigint;
}

interface TaskDbRow {
  readonly taskId: string;
  readonly totalMicroUsd: bigint;
}

interface HighestLsnRow {
  readonly lsn: number;
}

function agentDayKey(agentSlug: string, dayUtc: string): string {
  // Pipe separator: agentSlug is constrained by `AgentSlug` brand
  // (lowercase alnum + underscore) and dayUtc is `YYYY-MM-DD`; neither
  // contains `|`, so a key-collision attack via the agent_slug field is
  // structurally impossible.
  return `${agentSlug}|${dayUtc}`;
}

function eventTypeToKind(type: string): AuditEventKind | "other" {
  if (type === "cost.event") return "cost_event";
  if (type === "cost.budget.set") return "budget_set";
  if (type === "cost.budget.threshold.crossed") return "budget_threshold_crossed";
  return "other";
}

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

// Compare functions accept and emit bigint cumulative totals. The
// discrepancy wire shape is a decimal-string form to preserve exact
// values past the `MicroUsd` brand bound or the safe-integer boundary.
