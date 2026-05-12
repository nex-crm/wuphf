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
// PR B wires it as a GET /api/v1/cost/replay-check and a daily SRE alert.
// Taskless cost events skip the task projection (see triangulation B2);
// I2 is scoped to the task-attributed subset.
//
// Read-only: this function never writes. It is safe to call on a live
// broker; SQLite's WAL gives it a consistent snapshot.

import {
  type AuditEventKind,
  type BudgetId,
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadFromJsonValue,
  type EventLsn,
  lsnFromV1Number,
  type MicroUsd,
  parseLsn,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLogRecord } from "../event-log/index.ts";

export interface ReplayCheckReport {
  readonly ok: boolean;
  readonly highestLsn: EventLsn;
  readonly eventsScanned: number;
  readonly discrepancies: readonly ReplayDiscrepancy[];
}

export type ReplayDiscrepancy =
  | {
      readonly kind: "agent_day_total_mismatch";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayed: MicroUsd;
      readonly stored: MicroUsd;
    }
  | {
      readonly kind: "task_total_mismatch";
      readonly taskId: string;
      readonly replayed: MicroUsd;
      readonly stored: MicroUsd;
    }
  | {
      readonly kind: "agent_day_row_missing";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly replayed: MicroUsd;
    }
  | {
      readonly kind: "agent_day_row_ghost";
      readonly agentSlug: string;
      readonly dayUtc: string;
      readonly stored: MicroUsd;
    }
  | {
      readonly kind: "task_row_missing";
      readonly taskId: string;
      readonly replayed: MicroUsd;
    }
  | {
      readonly kind: "task_row_ghost";
      readonly taskId: string;
      readonly stored: MicroUsd;
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
      // #822: surface unparseable event-log rows distinctly so on-call
      // sees the failing LSN, event type, and parse reason instead of
      // a bare `internal_error` from the route. The route returns
      // 200 with `ok: false` so this is observable in the same shape
      // as any other drift discrepancy.
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
  readonly totalMicroUsd: number;
}

interface TaskDbRow {
  readonly taskId: string;
  readonly totalMicroUsd: number;
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

const _COST_EVENT_TYPES = new Set<string>([
  "cost.event",
  "cost.budget.set",
  "cost.budget.threshold.crossed",
]);

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
  const listAgentDaysStmt = db.prepare<[], AgentDayDbRow>(
    `SELECT agent_slug AS agentSlug, day_utc AS dayUtc, total_micro_usd AS totalMicroUsd
     FROM cost_by_agent`,
  );
  const listTasksStmt = db.prepare<[], TaskDbRow>(
    `SELECT task_id AS taskId, total_micro_usd AS totalMicroUsd FROM cost_by_task`,
  );
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

  const replayedAgentDays = new Map<string, number>();
  const replayedTasks = new Map<string, number>();
  const replayedBudgets = new Map<string, ReplayedBudget>();
  const replayedCrossings = new Map<string, ReplayedCrossing>();
  // #822: collect per-row parse failures as structured discrepancies
  // instead of throwing out of the loop. On-call sees the failing LSN
  // + event type + reason without grepping the listener log.
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
          const dayUtc = parsed.occurredAt.toISOString().slice(0, 10);
          const agentKey = agentDayKey(parsed.agentSlug, dayUtc);
          replayedAgentDays.set(
            agentKey,
            (replayedAgentDays.get(agentKey) ?? 0) + (parsed.amountMicroUsd as number),
          );
          if (parsed.taskId !== undefined) {
            replayedTasks.set(
              parsed.taskId,
              (replayedTasks.get(parsed.taskId) ?? 0) + (parsed.amountMicroUsd as number),
            );
          }
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
        } else if (kind === "budget_threshold_crossed") {
          // H1 fix: replay every threshold-crossed event into an expected
          // (budget_id, budget_set_lsn, threshold_bps) → row map. The
          // comparison below catches missing, ghost, and field-mismatch
          // drift against cost_threshold_crossings.
          const parsed = costAuditPayloadFromJsonValue(
            kind,
            JSON.parse(row.payload.toString("utf8")),
          ) as BudgetThresholdCrossedAuditPayload;
          const budgetSetLsnInt = parseLsn(parsed.budgetSetLsn).localLsn;
          const crossedAtLsnInt = parseLsn(parsed.crossedAtLsn).localLsn;
          const key = crossingKey(parsed.budgetId, budgetSetLsnInt, parsed.thresholdBps);
          replayedCrossings.set(key, {
            budgetId: parsed.budgetId,
            budgetSetLsn: budgetSetLsnInt,
            thresholdBps: parsed.thresholdBps,
            crossedAtLsn: crossedAtLsnInt,
            observedMicroUsd: parsed.observedMicroUsd as number,
            limitMicroUsd: parsed.limitMicroUsd as number,
          });
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

  // Surface per-row parse failures first so on-call sees them at the top
  // of the discrepancies list; downstream comparators may emit
  // sum-mismatch discrepancies for the same rows but the parse-failure
  // is the root cause.
  const discrepancies: ReplayDiscrepancy[] = [...parseFailures];

  const storedAgentDays = new Map<string, number>();
  for (const row of listAgentDaysStmt.all()) {
    storedAgentDays.set(agentDayKey(row.agentSlug, row.dayUtc), row.totalMicroUsd);
  }
  compareAgentDays(replayedAgentDays, storedAgentDays, discrepancies);

  const storedTasks = new Map<string, number>();
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

  const highest = highestLsnStmt.get()?.lsn ?? 0;
  return {
    ok: discrepancies.length === 0,
    highestLsn: lsnFromV1Number(highest),
    eventsScanned: scanned,
    discrepancies,
  };
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
}

function crossingKey(budgetId: string, budgetSetLsn: number, thresholdBps: number): string {
  return `${budgetId}|${budgetSetLsn}|${thresholdBps}`;
}

function compareCrossings(
  replayed: ReadonlyMap<string, ReplayedCrossing>,
  stored: ReadonlyMap<string, ThresholdCrossingDbRow>,
  out: ReplayDiscrepancy[],
): void {
  for (const [key, expected] of replayed.entries()) {
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

interface ReplayedBudget {
  readonly scope: "global" | "agent" | "task";
  readonly subjectId: string | null;
  readonly limitMicroUsd: number;
  readonly thresholdsBps: readonly number[];
  readonly setAtLsn: number;
  readonly tombstoned: boolean;
}

function compareAgentDays(
  replayed: ReadonlyMap<string, number>,
  stored: ReadonlyMap<string, number>,
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
        replayed: replayedTotal as MicroUsd,
      });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "agent_day_total_mismatch",
        agentSlug,
        dayUtc,
        replayed: replayedTotal as MicroUsd,
        stored: storedTotal as MicroUsd,
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
        stored: storedTotal as MicroUsd,
      });
    }
  }
}

function compareTasks(
  replayed: ReadonlyMap<string, number>,
  stored: ReadonlyMap<string, number>,
  out: ReplayDiscrepancy[],
): void {
  for (const [taskId, replayedTotal] of replayed.entries()) {
    const storedTotal = stored.get(taskId);
    if (storedTotal === undefined) {
      out.push({ kind: "task_row_missing", taskId, replayed: replayedTotal as MicroUsd });
      continue;
    }
    if (storedTotal !== replayedTotal) {
      out.push({
        kind: "task_total_mismatch",
        taskId,
        replayed: replayedTotal as MicroUsd,
        stored: storedTotal as MicroUsd,
      });
    }
  }
  for (const [taskId, storedTotal] of stored.entries()) {
    if (!replayed.has(taskId)) {
      out.push({ kind: "task_row_ghost", taskId, stored: storedTotal as MicroUsd });
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
    if (!arraysEqual(storedThresholds, replayedRow.thresholdsBps)) {
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

function parseStoredThresholds(raw: string): readonly number[] {
  const parsed: unknown = JSON.parse(raw);
  if (!Array.isArray(parsed) || parsed.some((n) => typeof n !== "number")) {
    return [];
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

// Used only inside the batch loop; importing the type without using it
// trips the lint. Re-exporting keeps the surface coherent for callers
// that want to depend on the batch iterator.
export type { EventLogRecord };
