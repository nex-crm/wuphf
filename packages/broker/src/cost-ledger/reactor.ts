// BudgetThresholdReactor.
//
// Runs synchronously inside the same SQLite transaction that appends a
// `cost.event` to event_log and updates `cost_by_agent` / `cost_by_task`.
// Why synchronously?
//
//   - §15.A's sum invariant `sum(cost_events) == sum(by_agent) == sum(by_task)`
//     must hold at every commit. Inlining the threshold check means a single
//     commit always carries the cost_event AND the matching threshold-crossed
//     events (if any), so replay from event_log reproduces identical state at
//     every LSN.
//   - Idempotency on re-run: the (budget_id, budget_set_lsn, threshold_bps)
//     PK on `cost_threshold_crossings` makes the insert a no-op if the
//     threshold already fired under this `budget_set` epoch. Raising a
//     budget mints a new `budget_set` LSN, which re-arms thresholds without
//     changing any PK semantics.
//   - Deterministic: `crossedAtLsn` is the triggering cost_event's LSN,
//     `crossedAt` is the cost_event's `occurredAt`. No wall-clock reads.
//
// Affected budgets per cost_event:
//   - `scope === "global"` budgets: every cost_event matches
//   - `scope === "agent"` budgets where `subjectId === costEvent.agentSlug`
//   - `scope === "task"` budgets where `subjectId === costEvent.taskId`
//
// Per affected budget, observed = total cumulative spend in scope:
//   - global / agent: `cost_by_agent` aggregated across days for the agent
//     (or globally) — for the architecture-proof slice we use lifetime sum
//     across all days; the calendar-day cap reset is enforced separately in
//     the PR B supervisor by querying `cost_by_agent` with the day key.
//   - task: `cost_by_task.total_micro_usd` for the matching taskId.
//
// The crossing fires when `observed * 10_000 >= threshold_bps * limit`. We
// use integer math throughout (multiplication may exceed 2^53 — see the note
// inside `crossesThreshold` for the bound check).

import {
  asAgentSlug,
  asBudgetId,
  asMicroUsd,
  asTaskId,
  type BudgetThresholdCrossedAuditPayload,
  costAuditPayloadToBytes,
  type EventLsn,
  lsnFromV1Number,
  type MicroUsd,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";

export interface ReactorInput {
  readonly costEventLsn: number;
  readonly agentSlug: string;
  readonly taskId: string | undefined;
  readonly occurredAt: Date;
}

export interface NewCrossing {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly observedMicroUsd: MicroUsd;
  readonly limitMicroUsd: MicroUsd;
  readonly crossingEventLsn: number;
}

interface BudgetRow {
  readonly budgetId: string;
  readonly scope: string;
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: string;
  readonly setAtLsn: number;
  readonly tombstoned: number;
}

interface AggregateRow {
  readonly total: number;
}

interface ExistingThresholdRow {
  readonly thresholdBps: number;
}

// Names the `reactor_cursors.reactor_name` for this reactor. Kept distinct
// from the cost-ledger's `cost_event` consumer because future reactors
// (notification fanout, billing export) will scan the same event_log with
// their own cursors.
export const BUDGET_THRESHOLD_REACTOR_NAME = "budget_threshold";

/**
 * Process one cost_event in the current transaction:
 *
 *   1. Load every live, non-tombstoned budget that overlaps the event's scope
 *      (global, agent, task).
 *   2. For each budget × ascending threshold, check whether cumulative
 *      observed spend now crosses the threshold and whether a crossing under
 *      this `budget_set` LSN already exists.
 *   3. For each newly-crossing (budget, threshold) pair, append a
 *      `cost.budget.threshold.crossed` event into event_log and insert the
 *      `cost_threshold_crossings` row.
 *   4. Advance `reactor_cursors.last_processed_lsn` to this cost_event's LSN.
 *
 * Returns the list of new crossings (deterministic order: budget_id asc,
 * threshold_bps asc) so the caller can log/emit them.
 */
export function processCostEventForCrossings(
  db: Database.Database,
  eventLog: EventLog,
  input: ReactorInput,
): readonly NewCrossing[] {
  const selectAffectedBudgets = db.prepare<[string, string | null], BudgetRow>(
    `SELECT budget_id AS budgetId, scope, subject_id AS subjectId,
            limit_micro_usd AS limitMicroUsd, thresholds_bps AS thresholdsBps,
            set_at_lsn AS setAtLsn, tombstoned
     FROM cost_budgets
     WHERE tombstoned = 0
       AND (
         scope = 'global'
         OR (scope = 'agent' AND subject_id = ?)
         OR (scope = 'task'  AND subject_id IS ?)
       )
     ORDER BY budget_id ASC`,
  );
  const agentLifetimeStmt = db.prepare<[string], AggregateRow>(
    "SELECT COALESCE(SUM(total_micro_usd), 0) AS total FROM cost_by_agent WHERE agent_slug = ?",
  );
  const globalLifetimeStmt = db.prepare<[], AggregateRow>(
    "SELECT COALESCE(SUM(total_micro_usd), 0) AS total FROM cost_by_agent",
  );
  const taskLifetimeStmt = db.prepare<[string], AggregateRow>(
    "SELECT COALESCE(total_micro_usd, 0) AS total FROM cost_by_task WHERE task_id = ?",
  );
  const existingThresholdsStmt = db.prepare<[string, number], ExistingThresholdRow>(
    `SELECT threshold_bps AS thresholdBps
     FROM cost_threshold_crossings
     WHERE budget_id = ? AND budget_set_lsn = ?`,
  );
  const insertCrossingStmt = db.prepare<[string, number, number, number, number, number]>(
    `INSERT INTO cost_threshold_crossings
       (budget_id, budget_set_lsn, threshold_bps, crossed_at_lsn, observed_micro_usd, limit_micro_usd)
     VALUES (?, ?, ?, ?, ?, ?)
     ON CONFLICT DO NOTHING`,
  );
  const upsertCursorStmt = db.prepare<[string, number, number]>(
    `INSERT INTO reactor_cursors (reactor_name, last_processed_lsn, updated_at_ms)
     VALUES (?, ?, ?)
     ON CONFLICT (reactor_name) DO UPDATE SET
       last_processed_lsn = excluded.last_processed_lsn,
       updated_at_ms = excluded.updated_at_ms`,
  );

  const taskKey = input.taskId ?? null;
  const candidates = selectAffectedBudgets.all(input.agentSlug, taskKey);
  const results: NewCrossing[] = [];

  for (const budget of candidates) {
    if (!isApplicable(budget, input)) continue;
    const observed = lifetimeFor(
      budget,
      input,
      agentLifetimeStmt,
      globalLifetimeStmt,
      taskLifetimeStmt,
    );
    if (observed === 0 && budget.limitMicroUsd === 0) continue;
    const existing = new Set<number>(
      existingThresholdsStmt.all(budget.budgetId, budget.setAtLsn).map((row) => row.thresholdBps),
    );
    const thresholds = parseThresholds(budget);
    for (const thresholdBps of thresholds) {
      if (existing.has(thresholdBps)) continue;
      if (!crossesThreshold(observed, budget.limitMicroUsd, thresholdBps)) continue;
      const crossingPayload: BudgetThresholdCrossedAuditPayload = {
        budgetId: asBudgetId(budget.budgetId),
        budgetSetLsn: lsnFromV1Number(budget.setAtLsn),
        thresholdBps,
        observedMicroUsd: asMicroUsd(observed),
        limitMicroUsd: asMicroUsd(budget.limitMicroUsd),
        crossedAtLsn: lsnFromV1Number(input.costEventLsn),
        crossedAt: input.occurredAt,
      };
      const bytes = costAuditPayloadToBytes("budget_threshold_crossed", crossingPayload);
      const crossingLsn = eventLog.append({
        type: "cost.budget.threshold.crossed",
        payload: Buffer.from(bytes),
      });
      insertCrossingStmt.run(
        budget.budgetId,
        budget.setAtLsn,
        thresholdBps,
        input.costEventLsn,
        observed,
        budget.limitMicroUsd,
      );
      results.push({
        budgetId: budget.budgetId,
        budgetSetLsn: budget.setAtLsn,
        thresholdBps,
        observedMicroUsd: asMicroUsd(observed),
        limitMicroUsd: asMicroUsd(budget.limitMicroUsd),
        crossingEventLsn: crossingLsn,
      });
    }
  }

  upsertCursorStmt.run(
    BUDGET_THRESHOLD_REACTOR_NAME,
    input.costEventLsn,
    input.occurredAt.getTime(),
  );
  return results;
}

export interface ReactorState {
  readonly lastProcessedLsn: EventLsn;
  readonly updatedAtMs: number;
}

interface ReactorCursorDbRow {
  readonly lastProcessedLsn: number;
  readonly updatedAtMs: number;
}

/**
 * Read the reactor's current cursor. Useful for diagnostics and the
 * `/replay-check` route to confirm the reactor has caught up to the latest
 * cost_event in event_log.
 */
export function readReactorCursor(db: Database.Database): ReactorState | null {
  const stmt = db.prepare<[string], ReactorCursorDbRow>(
    `SELECT last_processed_lsn AS lastProcessedLsn, updated_at_ms AS updatedAtMs
     FROM reactor_cursors WHERE reactor_name = ?`,
  );
  const row = stmt.get(BUDGET_THRESHOLD_REACTOR_NAME);
  if (row === undefined) return null;
  return {
    lastProcessedLsn: lsnFromV1Number(row.lastProcessedLsn),
    updatedAtMs: row.updatedAtMs,
  };
}

function isApplicable(budget: BudgetRow, input: ReactorInput): boolean {
  if (budget.scope === "global") return true;
  if (budget.scope === "agent") {
    return budget.subjectId === input.agentSlug && asAgentSlug(input.agentSlug) !== undefined;
  }
  if (budget.scope === "task") {
    return (
      input.taskId !== undefined &&
      budget.subjectId === input.taskId &&
      asTaskId(input.taskId) !== undefined
    );
  }
  return false;
}

function lifetimeFor(
  budget: BudgetRow,
  input: ReactorInput,
  agentStmt: Database.Statement<[string], AggregateRow>,
  globalStmt: Database.Statement<[], AggregateRow>,
  taskStmt: Database.Statement<[string], AggregateRow>,
): number {
  if (budget.scope === "global") {
    return globalStmt.get()?.total ?? 0;
  }
  if (budget.scope === "agent") {
    return agentStmt.get(input.agentSlug)?.total ?? 0;
  }
  // task
  if (input.taskId === undefined) return 0;
  return taskStmt.get(input.taskId)?.total ?? 0;
}

function parseThresholds(row: BudgetRow): readonly number[] {
  const parsed: unknown = JSON.parse(row.thresholdsBps);
  if (!Array.isArray(parsed)) {
    throw new Error(`reactor: thresholds_bps not an array for budget ${row.budgetId}`);
  }
  for (const v of parsed) {
    if (typeof v !== "number" || !Number.isSafeInteger(v) || v <= 0 || v > 10_000) {
      throw new Error(`reactor: invalid threshold ${String(v)} for budget ${row.budgetId}`);
    }
  }
  // Already ascending+deduped by protocol validators, but re-sort defensively
  // so a tampered DB row (manual SQL edit, downgrade) doesn't skip crossings.
  return [...(parsed as number[])].sort((a, b) => a - b);
}

/**
 * Integer threshold test: `observed >= (limit * threshold_bps) / 10_000`.
 *
 * Implemented as `observed * 10_000 >= limit * threshold_bps` to avoid
 * fractional truncation that would let us under-report a crossing by one
 * micro-USD. Operand bounds (per `packages/protocol/src/budgets.ts`):
 *   observed <= MAX_BUDGET_LIMIT_MICRO_USD = 1e12
 *   threshold_bps <= 10_000
 * Their product fits in 2^53 (Number.MAX_SAFE_INTEGER ≈ 9.007e15), so the
 * multiplication is safe in IEEE 754 doubles.
 */
function crossesThreshold(observed: number, limit: number, thresholdBps: number): boolean {
  if (limit === 0) return false;
  return observed * 10_000 >= limit * thresholdBps;
}
