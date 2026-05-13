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
// mid-check (better-sqlite3's WAL does NOT give per-statement
// snapshots on its own — round-2 review caught a stale claim here).

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

export interface ReplayCheckReport {
  readonly ok: boolean;
  readonly highestLsn: EventLsn;
  readonly eventsScanned: number;
  readonly discrepancies: readonly ReplayDiscrepancy[];
}

export type ReplayDiscrepancy =
  | {
      // Aggregate accumulators (`cost_by_agent.total_micro_usd`,
      // `cost_by_task.total_micro_usd`) are cumulative across the
      // lifetime of the ledger. The `MicroUsd` brand is bounded at
      // `MAX_BUDGET_LIMIT_MICRO_USD` (1e12), but these aggregates can
      // legitimately exceed that bound (a busy agent can spend more
      // than $1M micro-USD = 1e12 µUSD over time), and a hostile
      // projection row can carry arbitrary integer bytes. Emit as
      // decimal strings so the brand contract is preserved on the
      // wire and the diagnostic preserves exact integer values even
      // past `Number.MAX_SAFE_INTEGER`. See PR #845 for the parallel
      // fix on the threshold-oracle path.
      readonly kind: "agent_day_total_mismatch";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayedMicroUsdString: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "task_total_mismatch";
      readonly taskId: string;
      readonly replayedMicroUsdString: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "agent_day_row_missing";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayedMicroUsdString: string;
    }
  | {
      readonly kind: "agent_day_row_ghost";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "task_row_missing";
      readonly taskId: string;
      readonly replayedMicroUsdString: string;
    }
  | {
      readonly kind: "task_row_ghost";
      readonly taskId: string;
      readonly storedMicroUsdString: string;
    }
  | {
      readonly kind: "budget_state_mismatch";
      readonly budgetId: BudgetId;
      readonly field:
        | "scope"
        | "subjectId"
        | "limitMicroUsd"
        | "thresholdsBps"
        | "setAtLsn"
        | "tombstoned";
      readonly replayed: unknown;
      readonly stored: unknown;
    }
  | {
      readonly kind: "budget_row_missing";
      readonly budgetId: BudgetId;
    }
  | {
      readonly kind: "budget_row_ghost";
      readonly budgetId: BudgetId;
    }
  | {
      readonly kind: "threshold_crossing_missing";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
    }
  | {
      readonly kind: "threshold_crossing_ghost";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
    }
  | {
      readonly kind: "threshold_crossing_field_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly field: "crossedAtLsn" | "observedMicroUsd" | "limitMicroUsd";
      readonly replayed: number;
      readonly stored: number;
    }
  | {
      // Oracle: the independent threshold replay says this crossing
      // should have fired, but no `cost.budget.threshold.crossed` event
      // exists for it. Catches reactor under-emission bugs (e.g. a
      // regression in `crossesThreshold`'s integer math) that the
      // event-log-as-oracle replay can't see, because the missing event
      // is missing in BOTH the log and the projection.
      readonly kind: "threshold_crossing_unemitted";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // Decimal-string form of the oracle's bigint cumulative `observed`.
      // Cumulative lifetime spend is unbounded; `MicroUsd` is bounded at
      // `MAX_BUDGET_LIMIT_MICRO_USD` and would forge the brand here.
      // Pair with `unsafe_lifetime_accumulator` for boundary signals.
      readonly observedMicroUsdString: string;
      readonly limitMicroUsd: MicroUsd;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event exists, but the
      // independent threshold replay says it shouldn't fire. Distinct
      // from `threshold_crossing_ghost` — that variant is projection-row
      // drift from event log. This variant is event-log drift from the
      // oracle's view of cost events + budgets.
      //
      // `eventLsn` is the `event_log.lsn` of the offending threshold
      // audit row so on-call can locate and surgically inspect it.
      readonly kind: "threshold_crossing_spurious";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // Decimal-string form of the logged audit's `observedMicroUsd`.
      // The protocol bounds the per-event value at
      // `MAX_BUDGET_LIMIT_MICRO_USD`, but emitting via a string keeps
      // the field shape consistent with `threshold_crossing_unemitted`
      // and avoids brand drift if an adversary forges a value past
      // brand range that the decoder accepts.
      readonly observedMicroUsdString: string;
      readonly limitMicroUsd: MicroUsd;
      readonly crossedAtLsn: EventLsn;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: same (budget, epoch, threshold) exists in both the event
      // log and the oracle, but one of the recorded fields disagrees.
      // Most likely cause: the reactor and oracle diverged on which
      // cost_event was the trigger or what cumulative sum was observed.
      //
      // `crossedAtMs` is the `crossedAt` timestamp converted to ms-since-
      // epoch so all four fields share a `number` shape; on-call who
      // wants the human-readable timestamp can `new Date(value)`.
      readonly kind: "threshold_crossing_oracle_field_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      // `observedMicroUsd` is now handled by the sibling
      // `threshold_crossing_oracle_observed_mismatch` variant so it can
      // carry unbounded bigint values without forging the `MicroUsd`
      // brand. The fields kept here are bounded by their wire shape:
      // crossedAtLsn ≤ event_log.lsn (safe integer in practice),
      // limitMicroUsd ≤ `MAX_BUDGET_LIMIT_MICRO_USD`, crossedAtMs ≤ JS
      // `Date.now()` range.
      readonly field: "crossedAtLsn" | "limitMicroUsd" | "crossedAtMs";
      readonly expected: number;
      readonly logged: number;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: like `threshold_crossing_oracle_field_mismatch` but for
      // `observedMicroUsd`. Cumulative lifetime spend is unbounded, so
      // expected/logged are decimal-string form of the bigint values.
      // Split out so the bounded-field variant can keep `number` and
      // the brand contract on its emitted fields.
      readonly kind: "threshold_crossing_oracle_observed_mismatch";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly expectedMicroUsdString: string;
      readonly loggedMicroUsdString: string;
      readonly eventLsn: EventLsn;
    }
  | {
      // Oracle: more than one `cost.budget.threshold.crossed` event in
      // event_log shares the same `(budgetId, budgetSetLsn, thresholdBps)`
      // key. The projection PK suppresses duplicate rows
      // (`cost_threshold_crossings`) and `replayedCrossings` keys would
      // collapse duplicates into one entry, so the prior comparator
      // couldn't see this. A reactor over-emission bug or a forged
      // duplicate audit event surfaces here.
      readonly kind: "threshold_crossing_duplicate_event";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsns: readonly EventLsn[];
    }
  | {
      // Oracle: the reactor invariant is that a `cost.budget.threshold.crossed`
      // event is appended AFTER its triggering `cost.event` in the same
      // SQLite transaction, so `eventLsn > crossedAtLsn` must hold. A
      // payload claiming a later cost-event LSN than its own
      // event_log row LSN means either a forgery or a reactor regression
      // that violates the §15.A "every commit reproduces state at every
      // LSN" invariant.
      readonly kind: "threshold_crossing_causal_order_violation";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event names a
      // `crossedAtLsn` that has no corresponding `cost.event` in
      // event_log. Catches forged references that name an unrelated
      // LSN (e.g. a `cost.budget.set` row) — the existing causal-order
      // check only validates the numeric ordering, not that the
      // referenced row is actually a cost event.
      readonly kind: "threshold_crossing_dangling_reference";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
    }
  | {
      // Oracle: the reactor's strongest invariant is that threshold
      // events are appended SYNCHRONOUSLY immediately after their
      // triggering `cost.event`, all inside the same SQLite
      // transaction. So for a cost.event at LSN X with K legitimate
      // crossings, the threshold-event LSNs MUST be {X+1, …, X+K}.
      // A forged or delayed event appended much later (after a
      // tombstone, budget reset, or arbitrary gap) would otherwise
      // pass causal-order + reference + field checks. This variant
      // catches the gap.
      readonly kind: "threshold_crossing_delayed_emission";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly crossedAtLsn: EventLsn;
      readonly expectedWindowMinLsn: EventLsn;
      readonly expectedWindowMaxLsn: EventLsn;
    }
  | {
      // Oracle: a `cost.budget.threshold.crossed` event names a
      // `budgetSetLsn` that has no corresponding `cost.budget.set` in
      // event_log. Sibling of `dangling_reference` for the budget
      // side. Catches forgeries that point at an unrelated row OR
      // at a budgetSet for a different budgetId.
      readonly kind: "threshold_crossing_invalid_budget_set_reference";
      readonly budgetId: BudgetId;
      readonly budgetSetLsn: EventLsn;
      readonly thresholdBps: number;
      readonly eventLsn: EventLsn;
      readonly reason: "lsn_not_a_budget_set" | "budget_id_mismatch";
      /** When `reason === "budget_id_mismatch"`, the actual budgetId at the named LSN. */
      readonly actualBudgetId?: BudgetId;
    }
  | {
      // Oracle: one of the lifetime accumulators (`oracleGlobalLifetime`,
      // `oracleAgentLifetime[slug]`, `oracleTaskLifetime[taskId]`) just
      // crossed a representability boundary. Fires once per
      // (scope, subjectId, reason) per replay-check run.
      //
      // Two boundaries fire independently:
      //
      // - `exceeds_micro_usd_brand`: post > `MAX_BUDGET_LIMIT_MICRO_USD`
      //   (1e12). The accumulator has surpassed the documented
      //   `MicroUsd` brand ceiling. Downstream consumers that re-validate
      //   `MicroUsd` payloads should not accept values past this point.
      //   Oracle discrepancies that carry cumulative observed totals
      //   (`threshold_crossing_unemitted`,
      //   `threshold_crossing_spurious`,
      //   `threshold_crossing_oracle_observed_mismatch`) emit decimal
      //   strings instead of branded `MicroUsd` to avoid forging the
      //   brand past this boundary.
      //
      // - `exceeds_safe_integer`: post > `Number.MAX_SAFE_INTEGER`
      //   (≈ 9e15). The accumulator has surpassed the JS-`number`
      //   precision boundary. The internal math is now bigint, but any
      //   `Number(observed)` conversion would lose precision past this
      //   point. The decimal-string emission keeps the wire shape
      //   exact, but on-call should still treat any number-typed
      //   derivative as suspect once this fires.
      readonly kind: "unsafe_lifetime_accumulator";
      readonly reason: "exceeds_micro_usd_brand" | "exceeds_safe_integer";
      readonly scope: "global" | "agent" | "task";
      /** `null` for global; agentSlug or taskId for the other two scopes. */
      readonly subjectId: string | null;
      /** Cost event whose append pushed the accumulator past the boundary. */
      readonly costEventLsn: EventLsn;
      /** Accumulated total as a decimal string. Bigint, JSON-safe. */
      readonly accumulatedMicroUsd: string;
    }
  | {
      // Surface unparseable event-log rows distinctly so on-call sees the
      // failing LSN, event type, and parse reason instead of a bare
      // `internal_error` from the route.
      readonly kind: "event_payload_unparseable";
      readonly lsn: EventLsn;
      readonly type: string;
      readonly reason: string;
    };

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

interface BudgetDbRow {
  readonly budgetId: string;
  readonly scope: string;
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: string;
  readonly setAtLsn: number;
  readonly tombstoned: number;
}

interface HighestLsnRow {
  readonly lsn: number;
}

const BATCH_SIZE = 1_000;

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
  const listCrossingsStmt = db.prepare<[], ThresholdCrossingDbRow>(
    `SELECT budget_id AS budgetId, budget_set_lsn AS budgetSetLsn,
            threshold_bps AS thresholdBps, crossed_at_lsn AS crossedAtLsn,
            observed_micro_usd AS observedMicroUsd, limit_micro_usd AS limitMicroUsd
     FROM cost_threshold_crossings`,
  );

  // Round-2 fix: run all reads inside a single SQLite snapshot. The
  // file header used to claim WAL gave us this for free; it doesn't —
  // better-sqlite3 only snapshots within an explicit transaction.
  // `.deferred()` acquires a SHARED lock on first read and holds it
  // through the rest of the function; concurrent writers can still
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
    // Collect discrepancies surfaced inline during the scan loop
    // (parse failures, oracle-state warnings such as
    // `unsafe_lifetime_accumulator`). Merged into the final
    // `discrepancies` list at the end of the replay.
    const parseFailures: ReplayDiscrepancy[] = [];

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
              parseFailures,
            );
            const agentPost = (oracleAgentLifetime.get(parsed.agentSlug) ?? 0n) + amountBig;
            oracleAgentLifetime.set(parsed.agentSlug, agentPost);
            flagUnsafeAccumulator(
              "agent",
              parsed.agentSlug,
              agentPost,
              row.lsn,
              unsafeAccumulatorFlagged,
              parseFailures,
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
                parseFailures,
              );
            }
            computeExpectedCrossings({
              costEventLsn: row.lsn,
              costEventOccurredAtMs: occurredAtMs,
              agentSlug: parsed.agentSlug,
              taskId: parsed.taskId,
              budgets: replayedBudgets,
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
            replayedBudgets.set(parsed.budgetId, {
              scope: parsed.scope,
              subjectId: parsed.subjectId ?? null,
              limitMicroUsd: parsed.limitMicroUsd as number,
              thresholdsBps: [...parsed.thresholdsBps],
              setAtLsn: row.lsn,
              tombstoned: (parsed.limitMicroUsd as number) === 0,
            });
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
          parseFailures.push({
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

    // Surface per-row parse failures first so on-call sees them at the
    // top of the discrepancies list; downstream comparators may emit
    // sum-mismatch discrepancies for the same rows but the
    // parse-failure is the root cause.
    const discrepancies: ReplayDiscrepancy[] = [...parseFailures];

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

interface ThresholdCrossingDbRow {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  readonly observedMicroUsd: number;
  readonly limitMicroUsd: number;
}

interface ReplayedCrossing {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  readonly observedMicroUsd: number;
  readonly limitMicroUsd: number;
  /** `crossedAt` from the audit payload, as ms-since-epoch. */
  readonly crossedAtMs: number;
  /** The `event_log.lsn` of this threshold-crossed audit row itself. */
  readonly eventLsn: number;
}

function crossingKey(budgetId: string, budgetSetLsn: number, thresholdBps: number): string {
  return `${budgetId}|${budgetSetLsn}|${thresholdBps}`;
}

// `MicroUsd` brand ceiling as a bigint. Cumulative oracle accumulators
// that cross this no longer fit the `MicroUsd` contract; emit a
// decimal-string form in any discrepancy that carries them. Derived
// from the protocol constant so a future change to the brand bound
// can't silently drift the oracle.
const MAX_BUDGET_LIMIT_MICRO_USD_BIG = BigInt(MAX_BUDGET_LIMIT_MICRO_USD);

// 2^53 - 1, the largest exact integer representable as a JS `number`.
// Past this point any `Number(bigint)` cast rounds. The internal math
// is now bigint and the cumulative-observed wire shape is a decimal
// string, so this is purely a "values past here are number-typed-
// suspect" signal for on-call dashboards.
const MAX_SAFE_INTEGER_BIG = BigInt(Number.MAX_SAFE_INTEGER);

// Records the first cost.event LSN at which an accumulator
// (`global`, `agent[slug]`, or `task[id]`) crosses each
// representability boundary. Fires once per (scope, subjectId, reason)
// per replay-check run. The reasons fire independently:
//
// - `exceeds_micro_usd_brand` (`MAX_BUDGET_LIMIT_MICRO_USD`, 1e12):
//   downstream consumers that re-validate `MicroUsd` payloads will
//   reject values past here; oracle discrepancies switch to decimal
//   strings.
// - `exceeds_safe_integer` (`Number.MAX_SAFE_INTEGER`, ≈ 9e15): any
//   number-typed derivative loses precision past here.
function flagUnsafeAccumulator(
  scope: "global" | "agent" | "task",
  subjectId: string | null,
  post: bigint,
  costEventLsn: number,
  flagged: Set<string>,
  out: ReplayDiscrepancy[],
): void {
  if (post > MAX_BUDGET_LIMIT_MICRO_USD_BIG) {
    pushUnsafeIfNew("exceeds_micro_usd_brand", scope, subjectId, post, costEventLsn, flagged, out);
  }
  if (post > MAX_SAFE_INTEGER_BIG) {
    pushUnsafeIfNew("exceeds_safe_integer", scope, subjectId, post, costEventLsn, flagged, out);
  }
}

function pushUnsafeIfNew(
  reason: "exceeds_micro_usd_brand" | "exceeds_safe_integer",
  scope: "global" | "agent" | "task",
  subjectId: string | null,
  post: bigint,
  costEventLsn: number,
  flagged: Set<string>,
  out: ReplayDiscrepancy[],
): void {
  const key = `${scope}|${subjectId ?? ""}|${reason}`;
  if (flagged.has(key)) return;
  flagged.add(key);
  out.push({
    kind: "unsafe_lifetime_accumulator",
    reason,
    scope,
    subjectId,
    costEventLsn: lsnFromV1Number(costEventLsn),
    accumulatedMicroUsd: post.toString(),
  });
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
  // Round-4 exposure (PR #845 round-4 codex api + security 2-of-2):
  // the aggregate compare functions are exposed so tests can drive
  // them with synthetic bigint maps past `MAX_BUDGET_LIMIT_MICRO_USD`
  // and `Number.MAX_SAFE_INTEGER`. End-to-end is blocked by the same
  // protocol per-event cap as the threshold-oracle path.
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
    if (actual.observedMicroUsd !== expected.observedMicroUsd) {
      out.push({
        kind: "threshold_crossing_field_mismatch",
        budgetId: expected.budgetId as BudgetId,
        budgetSetLsn: lsnFromV1Number(expected.budgetSetLsn),
        thresholdBps: expected.thresholdBps,
        field: "observedMicroUsd",
        replayed: expected.observedMicroUsd,
        stored: actual.observedMicroUsd,
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

interface ReplayedBudget {
  readonly scope: "global" | "agent" | "task";
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: readonly number[];
  readonly setAtLsn: number;
  readonly tombstoned: boolean;
}

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

function agentDayKey(agentSlug: string, dayUtc: string): string {
  // Pipe separator: agentSlug is constrained by `AgentSlug` brand
  // (lowercase alnum + underscore) and dayUtc is `YYYY-MM-DD`; neither
  // contains `|`, so a key-collision attack via the agent_slug field is
  // structurally impossible.
  return `${agentSlug}|${dayUtc}`;
}

function splitAgentDayKey(key: string): { readonly agentSlug: string; readonly dayUtc: string } {
  const idx = key.indexOf("|");
  if (idx === -1) {
    throw new Error(`replay-check: malformed agent-day key ${key}`);
  }
  return { agentSlug: key.slice(0, idx), dayUtc: key.slice(idx + 1) };
}

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
  // Mirror the protocol's `BudgetSetAuditPayload.thresholdsBps`
  // validation: each entry must be a positive safe-integer ≤ 10_000.
  // Round-3 hardening: a bare `typeof === "number"` check accepted
  // `Infinity` (from `JSON.parse("[1e999]")`), which would later JSON-
  // serialize as `null` and lose the corruption signal. Round-3 fix:
  // explicit `Number.isSafeInteger` + bounds.
  for (const n of parsed) {
    if (typeof n !== "number" || !Number.isSafeInteger(n) || n <= 0 || n > 10_000) {
      return {
        error: `thresholds_bps contains invalid entry ${String(n)} (expected positive safe integer ≤ 10000)`,
      };
    }
  }
  return parsed as readonly number[];
}

function arraysEqual(a: readonly number[], b: readonly number[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

function eventTypeToKind(type: string): AuditEventKind | "other" {
  if (type === "cost.event") return "cost_event";
  if (type === "cost.budget.set") return "budget_set";
  if (type === "cost.budget.threshold.crossed") return "budget_threshold_crossed";
  return "other";
}

interface ExpectedCrossing {
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

interface ComputeExpectedCrossingsArgs {
  readonly costEventLsn: number;
  readonly costEventOccurredAtMs: number;
  readonly agentSlug: string;
  readonly taskId: string | undefined;
  readonly budgets: ReadonlyMap<string, ReplayedBudget>;
  /** BigInt accumulators preserve precision past `Number.MAX_SAFE_INTEGER`. */
  readonly globalLifetime: bigint;
  readonly agentLifetime: ReadonlyMap<string, bigint>;
  readonly taskLifetime: ReadonlyMap<string, bigint>;
  readonly out: Map<string, ExpectedCrossing>;
}

// Independent threshold-crossing oracle for a single cost_event. Mirrors
// the reactor's logic (`processCostEventForCrossings` in reactor.ts) but
// derives `observed` from oracle-maintained lifetime trackers rather than
// reading `cost_by_agent` / `cost_by_task` from disk. The crossing math
// (`crossesThresholdBigInt`) MUST stay byte-identical with the reactor's
// `crossesThreshold`; if they diverge, this oracle becomes a tautology.
function computeExpectedCrossings(args: ComputeExpectedCrossingsArgs): void {
  for (const [budgetId, budget] of args.budgets.entries()) {
    if (budget.tombstoned) continue;
    if (!isOracleApplicable(budget, args.agentSlug, args.taskId)) continue;
    const observed = oracleObservedFor(budget, args);
    if (observed === 0n && budget.limitMicroUsd === 0) continue;
    for (const thresholdBps of budget.thresholdsBps) {
      const key = crossingKey(budgetId, budget.setAtLsn, thresholdBps);
      if (args.out.has(key)) continue;
      if (!crossesThresholdBigInt(observed, budget.limitMicroUsd, thresholdBps)) continue;
      args.out.set(key, {
        budgetId,
        budgetSetLsn: budget.setAtLsn,
        thresholdBps,
        crossedAtLsn: args.costEventLsn,
        // Keep cumulative `observed` as bigint internally. Discrepancy
        // emission below stringifies via `.toString()` to honor the
        // `MicroUsd` brand contract on the wire.
        observedMicroUsd: observed,
        limitMicroUsd: budget.limitMicroUsd,
        crossedAtMs: args.costEventOccurredAtMs,
      });
    }
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
function crossesThresholdBigInt(observed: bigint, limit: number, thresholdBps: number): boolean {
  if (limit === 0) return false;
  return observed * 10_000n >= BigInt(limit) * BigInt(thresholdBps);
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
