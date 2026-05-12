// Cost-ledger projection writer.
//
// Every append goes through one SQLite transaction that:
//   1. canonicalizes the typed payload via `costAuditPayloadToBytes` so the
//      bytes in `event_log` match the audit-event chain exactly,
//   2. inserts the event_log row and captures its LSN,
//   3. applies the projection update keyed by that LSN,
//   4. (idempotent variants only) inserts the `command_idempotency` row
//      with the rendered response — same tx, so a crash between the
//      ledger write and the idempotency row is impossible.
//
// The §15.A invariants this slice guarantees, both decidable:
//   I1. sum(cost_events in event_log) == sum(cost_by_agent across all days)
//   I2. sum(task-attributed cost_events) == sum(cost_by_task)
//
// Cost events with no `taskId` skip the task projection entirely (see
// `cost.ts:CostEventAuditPayload.taskId?`); I2 holds only for the
// task-attributed subset. The earlier "grand-sum" wording was unfalsifiable
// because `replay-check` mirrors the same skip. See triangulation finding B2.
//
// Integer math throughout (amounts, projections) keeps both invariants
// decidable across a long-lived ledger.

import {
  asMicroUsd,
  type BudgetId,
  type BudgetScope,
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  canonicalJSON,
  costAuditPayloadToBytes,
  type EventLsn,
  lsnFromV1Number,
  type MicroUsd,
  parseLsn,
} from "@wuphf/protocol";
import type Database from "better-sqlite3";

import type { EventLog } from "../event-log/index.ts";
import type { ParsedIdempotencyKey } from "./idempotency.ts";
import { type NewCrossing, processCostEventForCrossings } from "./reactor.ts";

export interface CostEventAppendResult {
  readonly lsn: EventLsn;
  readonly agentDayTotal: MicroUsd;
  readonly taskTotal: MicroUsd | null;
  readonly newCrossings: readonly NewCrossing[];
}

export interface BudgetSetAppendResult {
  readonly lsn: EventLsn;
  readonly tombstoned: boolean;
}

export interface ThresholdCrossedAppendResult {
  readonly lsn: EventLsn;
  readonly alreadyRecorded: boolean;
}

export interface AgentSpendRow {
  readonly agentSlug: string;
  readonly dayUtc: string;
  readonly totalMicroUsd: MicroUsd;
  readonly lastLsn: EventLsn;
}

export interface TaskSpendRow {
  readonly taskId: string;
  readonly totalMicroUsd: MicroUsd;
  readonly lastLsn: EventLsn;
}

export interface BudgetRow {
  readonly budgetId: BudgetId;
  readonly scope: BudgetScope;
  readonly subjectId: string | null;
  readonly limitMicroUsd: MicroUsd;
  readonly thresholdsBps: readonly number[];
  readonly setAtLsn: EventLsn;
  readonly tombstoned: boolean;
}

export interface ThresholdCrossingRow {
  readonly budgetId: BudgetId;
  readonly budgetSetLsn: EventLsn;
  readonly thresholdBps: number;
  readonly crossedAtLsn: EventLsn;
  readonly observedMicroUsd: MicroUsd;
  readonly limitMicroUsd: MicroUsd;
}

export interface IdempotentAppendResult {
  /** True iff the response was replayed from `command_idempotency`. */
  readonly replayed: boolean;
  readonly statusCode: number;
  /** Pre-rendered response bytes — the route writes these verbatim. */
  readonly payload: Buffer;
}

export interface IdempotentCostEventArgs {
  readonly payload: CostEventAuditPayload;
  readonly idempotency: ParsedIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: CostEventAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface IdempotentBudgetSetArgs {
  readonly payload: BudgetSetAuditPayload;
  readonly idempotency: ParsedIdempotencyKey;
  readonly nowMs: number;
  readonly render: (applied: BudgetSetAppendResult) => {
    readonly statusCode: number;
    readonly payload: Buffer;
  };
}

export interface CostLedger {
  appendCostEvent(payload: CostEventAuditPayload): CostEventAppendResult;
  appendBudgetSet(payload: BudgetSetAuditPayload): BudgetSetAppendResult;
  appendThresholdCrossed(payload: BudgetThresholdCrossedAuditPayload): ThresholdCrossedAppendResult;

  /**
   * Atomic equivalent of `appendCostEvent` with built-in idempotency.
   * Runs the lookup, the append, and the idempotency-row insert in ONE
   * SQLite transaction so a crash between any two steps cannot lose the
   * replay record. The route hands in a `render` function that produces
   * the response bytes given the applied result; those bytes are what
   * subsequent replays return byte-for-byte.
   *
   * Replaces the pre-existing `runIdempotent + appendCostEvent` two-step
   * (see triangulation finding B1).
   */
  appendCostEventIdempotent(args: IdempotentCostEventArgs): IdempotentAppendResult;
  appendBudgetSetIdempotent(args: IdempotentBudgetSetArgs): IdempotentAppendResult;

  getAgentSpend(agentSlug: string, dayUtc: string): AgentSpendRow | null;
  listAgentSpend(filter?: { dayUtc?: string }): readonly AgentSpendRow[];
  getTaskSpend(taskId: string): TaskSpendRow | null;
  getBudget(budgetId: BudgetId): BudgetRow | null;
  listBudgets(): readonly BudgetRow[];
  listThresholdCrossings(budgetId?: BudgetId): readonly ThresholdCrossingRow[];
}

interface AgentSpendDbRow {
  readonly agentSlug: string;
  readonly dayUtc: string;
  readonly totalMicroUsd: number;
  readonly lastLsn: number;
}

interface TaskSpendDbRow {
  readonly taskId: string;
  readonly totalMicroUsd: number;
  readonly lastLsn: number;
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

interface ThresholdCrossingDbRow {
  readonly budgetId: string;
  readonly budgetSetLsn: number;
  readonly thresholdBps: number;
  readonly crossedAtLsn: number;
  readonly observedMicroUsd: number;
  readonly limitMicroUsd: number;
}

export function createCostLedger(db: Database.Database, eventLog: EventLog): CostLedger {
  const upsertAgentSpend = db.prepare<[string, string, number, number]>(
    `INSERT INTO cost_by_agent (agent_slug, day_utc, total_micro_usd, last_lsn)
     VALUES (?, ?, ?, ?)
     ON CONFLICT (agent_slug, day_utc) DO UPDATE SET
       total_micro_usd = total_micro_usd + excluded.total_micro_usd,
       last_lsn = excluded.last_lsn`,
  );
  const selectAgentSpend = db.prepare<[string, string], AgentSpendDbRow>(
    `SELECT agent_slug AS agentSlug, day_utc AS dayUtc,
            total_micro_usd AS totalMicroUsd, last_lsn AS lastLsn
     FROM cost_by_agent WHERE agent_slug = ? AND day_utc = ?`,
  );
  const upsertTaskSpend = db.prepare<[string, number, number]>(
    `INSERT INTO cost_by_task (task_id, total_micro_usd, last_lsn)
     VALUES (?, ?, ?)
     ON CONFLICT (task_id) DO UPDATE SET
       total_micro_usd = total_micro_usd + excluded.total_micro_usd,
       last_lsn = excluded.last_lsn`,
  );
  const selectTaskSpend = db.prepare<[string], TaskSpendDbRow>(
    `SELECT task_id AS taskId, total_micro_usd AS totalMicroUsd, last_lsn AS lastLsn
     FROM cost_by_task WHERE task_id = ?`,
  );
  const upsertBudget = db.prepare<[string, string, string | null, number, string, number, number]>(
    `INSERT INTO cost_budgets (budget_id, scope, subject_id, limit_micro_usd, thresholds_bps, set_at_lsn, tombstoned)
     VALUES (?, ?, ?, ?, ?, ?, ?)
     ON CONFLICT (budget_id) DO UPDATE SET
       scope = excluded.scope,
       subject_id = excluded.subject_id,
       limit_micro_usd = excluded.limit_micro_usd,
       thresholds_bps = excluded.thresholds_bps,
       set_at_lsn = excluded.set_at_lsn,
       tombstoned = excluded.tombstoned`,
  );
  const selectBudget = db.prepare<[string], BudgetDbRow>(
    `SELECT budget_id AS budgetId, scope, subject_id AS subjectId,
            limit_micro_usd AS limitMicroUsd, thresholds_bps AS thresholdsBps,
            set_at_lsn AS setAtLsn, tombstoned
     FROM cost_budgets WHERE budget_id = ?`,
  );
  const listBudgetsStmt = db.prepare<[], BudgetDbRow>(
    `SELECT budget_id AS budgetId, scope, subject_id AS subjectId,
            limit_micro_usd AS limitMicroUsd, thresholds_bps AS thresholdsBps,
            set_at_lsn AS setAtLsn, tombstoned
     FROM cost_budgets ORDER BY budget_id ASC`,
  );
  const listAgentSpendAllStmt = db.prepare<[], AgentSpendDbRow>(
    `SELECT agent_slug AS agentSlug, day_utc AS dayUtc,
            total_micro_usd AS totalMicroUsd, last_lsn AS lastLsn
     FROM cost_by_agent ORDER BY day_utc DESC, agent_slug ASC`,
  );
  const listAgentSpendDayStmt = db.prepare<[string], AgentSpendDbRow>(
    `SELECT agent_slug AS agentSlug, day_utc AS dayUtc,
            total_micro_usd AS totalMicroUsd, last_lsn AS lastLsn
     FROM cost_by_agent WHERE day_utc = ? ORDER BY agent_slug ASC`,
  );
  const insertCrossingStmt = db.prepare<[string, number, number, number, number, number]>(
    `INSERT INTO cost_threshold_crossings
       (budget_id, budget_set_lsn, threshold_bps, crossed_at_lsn, observed_micro_usd, limit_micro_usd)
     VALUES (?, ?, ?, ?, ?, ?)
     ON CONFLICT (budget_id, budget_set_lsn, threshold_bps) DO NOTHING`,
  );
  const listCrossingsAllStmt = db.prepare<[], ThresholdCrossingDbRow>(
    `SELECT budget_id AS budgetId, budget_set_lsn AS budgetSetLsn,
            threshold_bps AS thresholdBps, crossed_at_lsn AS crossedAtLsn,
            observed_micro_usd AS observedMicroUsd, limit_micro_usd AS limitMicroUsd
     FROM cost_threshold_crossings
     ORDER BY budget_id ASC, budget_set_lsn ASC, threshold_bps ASC`,
  );
  const listCrossingsForBudgetStmt = db.prepare<[string], ThresholdCrossingDbRow>(
    `SELECT budget_id AS budgetId, budget_set_lsn AS budgetSetLsn,
            threshold_bps AS thresholdBps, crossed_at_lsn AS crossedAtLsn,
            observed_micro_usd AS observedMicroUsd, limit_micro_usd AS limitMicroUsd
     FROM cost_threshold_crossings WHERE budget_id = ?
     ORDER BY budget_set_lsn ASC, threshold_bps ASC`,
  );

  // Idempotency-row statements (B1 fix). Lookup + insert run inside the
  // SAME transaction as the append so a crash between the two is
  // impossible — a retry sees either both committed or both rolled back.
  const idempotencyLookupStmt = db.prepare<
    [string, string],
    {
      readonly statusCode: number;
      readonly responsePayload: Buffer;
    }
  >(
    `SELECT status_code AS statusCode, response_payload AS responsePayload
     FROM command_idempotency
     WHERE idempotency_key = ? AND command = ?`,
  );
  const idempotencyInsertStmt = db.prepare<[string, string, number, Buffer, number | null, number]>(
    `INSERT INTO command_idempotency
       (idempotency_key, command, status_code, response_payload, created_at_lsn, created_at_ms)
     VALUES (?, ?, ?, ?, ?, ?)`,
  );

  // Inner (non-transactional) append helpers. The public `appendX`
  // methods wrap these in their own transactions. The `appendXIdempotent`
  // methods wrap them along with the idempotency-row insert in one
  // bigger transaction.
  const appendCostEventInner = (payload: CostEventAuditPayload): CostEventAppendResult => {
    const bytes = costAuditPayloadToBytes("cost_event", payload);
    const lsn = eventLog.append({ type: "cost.event", payload: Buffer.from(bytes) });
    const dayUtc = isoDateUtc(payload.occurredAt);
    upsertAgentSpend.run(payload.agentSlug, dayUtc, payload.amountMicroUsd as number, lsn);
    const agentRow = selectAgentSpend.get(payload.agentSlug, dayUtc);
    if (agentRow === undefined) {
      throw new Error("cost_by_agent upsert produced no row (concurrent delete?)");
    }
    let taskTotal: MicroUsd | null = null;
    if (payload.taskId !== undefined) {
      upsertTaskSpend.run(payload.taskId, payload.amountMicroUsd as number, lsn);
      const taskRow = selectTaskSpend.get(payload.taskId);
      if (taskRow === undefined) {
        throw new Error("cost_by_task upsert produced no row (concurrent delete?)");
      }
      taskTotal = asMicroUsd(taskRow.totalMicroUsd);
    }
    // Threshold reactor runs in the same transaction so the §15.A
    // invariants hold at every commit: a cost_event and its derivative
    // threshold crossings either both land or both roll back.
    const newCrossings = processCostEventForCrossings(db, eventLog, {
      costEventLsn: lsn,
      agentSlug: payload.agentSlug,
      taskId: payload.taskId,
      occurredAt: payload.occurredAt,
    });
    return {
      lsn: lsnFromV1Number(lsn),
      agentDayTotal: asMicroUsd(agentRow.totalMicroUsd),
      taskTotal,
      newCrossings,
    };
  };

  const appendBudgetSetInner = (payload: BudgetSetAuditPayload): BudgetSetAppendResult => {
    const bytes = costAuditPayloadToBytes("budget_set", payload);
    const lsn = eventLog.append({ type: "cost.budget.set", payload: Buffer.from(bytes) });
    const tombstoned = (payload.limitMicroUsd as number) === 0;
    upsertBudget.run(
      payload.budgetId,
      payload.scope,
      payload.subjectId ?? null,
      payload.limitMicroUsd as number,
      canonicalJSON(payload.thresholdsBps),
      lsn,
      tombstoned ? 1 : 0,
    );
    return { lsn: lsnFromV1Number(lsn), tombstoned };
  };

  const appendCostEventTransaction = db.transaction(appendCostEventInner);
  const appendBudgetSetTransaction = db.transaction(appendBudgetSetInner);

  const appendCostEventIdempotentTransaction = db.transaction(
    (args: IdempotentCostEventArgs): IdempotentAppendResult => {
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
        return {
          replayed: true,
          statusCode: cached.statusCode,
          payload: Buffer.from(cached.responsePayload),
        };
      }
      const applied = appendCostEventInner(args.payload);
      const rendered = args.render(applied);
      idempotencyInsertStmt.run(
        args.idempotency.raw,
        args.idempotency.command,
        rendered.statusCode,
        rendered.payload,
        // applied.lsn is "v1:N" — extract the integer for the column.
        parseLsn(applied.lsn).localLsn,
        args.nowMs,
      );
      return {
        replayed: false,
        statusCode: rendered.statusCode,
        payload: rendered.payload,
      };
    },
  );

  const appendBudgetSetIdempotentTransaction = db.transaction(
    (args: IdempotentBudgetSetArgs): IdempotentAppendResult => {
      const cached = idempotencyLookupStmt.get(args.idempotency.raw, args.idempotency.command);
      if (cached !== undefined) {
        return {
          replayed: true,
          statusCode: cached.statusCode,
          payload: Buffer.from(cached.responsePayload),
        };
      }
      const applied = appendBudgetSetInner(args.payload);
      const rendered = args.render(applied);
      idempotencyInsertStmt.run(
        args.idempotency.raw,
        args.idempotency.command,
        rendered.statusCode,
        rendered.payload,
        parseLsn(applied.lsn).localLsn,
        args.nowMs,
      );
      return {
        replayed: false,
        statusCode: rendered.statusCode,
        payload: rendered.payload,
      };
    },
  );

  const appendThresholdCrossedTransaction = db.transaction(
    (payload: BudgetThresholdCrossedAuditPayload): ThresholdCrossedAppendResult => {
      const bytes = costAuditPayloadToBytes("budget_threshold_crossed", payload);
      const lsn = eventLog.append({
        type: "cost.budget.threshold.crossed",
        payload: Buffer.from(bytes),
      });
      const budgetSetLsn = parseLsn(payload.budgetSetLsn).localLsn;
      const crossedAtLsn = parseLsn(payload.crossedAtLsn).localLsn;
      const result = insertCrossingStmt.run(
        payload.budgetId,
        budgetSetLsn,
        payload.thresholdBps,
        crossedAtLsn,
        payload.observedMicroUsd as number,
        payload.limitMicroUsd as number,
      );
      return { lsn: lsnFromV1Number(lsn), alreadyRecorded: result.changes === 0 };
    },
  );

  return {
    appendCostEvent(payload: CostEventAuditPayload): CostEventAppendResult {
      return appendCostEventTransaction.immediate(payload);
    },
    appendBudgetSet(payload: BudgetSetAuditPayload): BudgetSetAppendResult {
      return appendBudgetSetTransaction.immediate(payload);
    },
    appendThresholdCrossed(
      payload: BudgetThresholdCrossedAuditPayload,
    ): ThresholdCrossedAppendResult {
      return appendThresholdCrossedTransaction.immediate(payload);
    },
    appendCostEventIdempotent(args: IdempotentCostEventArgs): IdempotentAppendResult {
      return appendCostEventIdempotentTransaction.immediate(args);
    },
    appendBudgetSetIdempotent(args: IdempotentBudgetSetArgs): IdempotentAppendResult {
      return appendBudgetSetIdempotentTransaction.immediate(args);
    },
    getAgentSpend(agentSlug: string, dayUtc: string): AgentSpendRow | null {
      const row = selectAgentSpend.get(agentSlug, dayUtc);
      return row === undefined ? null : toAgentSpendRow(row);
    },
    listAgentSpend(filter?: { dayUtc?: string }): readonly AgentSpendRow[] {
      const rows =
        filter?.dayUtc !== undefined
          ? listAgentSpendDayStmt.all(filter.dayUtc)
          : listAgentSpendAllStmt.all();
      return rows.map(toAgentSpendRow);
    },
    getTaskSpend(taskId: string): TaskSpendRow | null {
      const row = selectTaskSpend.get(taskId);
      return row === undefined ? null : toTaskSpendRow(row);
    },
    getBudget(budgetId: BudgetId): BudgetRow | null {
      const row = selectBudget.get(budgetId);
      return row === undefined ? null : toBudgetRow(row);
    },
    listBudgets(): readonly BudgetRow[] {
      return listBudgetsStmt.all().map(toBudgetRow);
    },
    listThresholdCrossings(budgetId?: BudgetId): readonly ThresholdCrossingRow[] {
      const rows =
        budgetId === undefined
          ? listCrossingsAllStmt.all()
          : listCrossingsForBudgetStmt.all(budgetId);
      return rows.map(toThresholdCrossingRow);
    },
  };
}

function toAgentSpendRow(row: AgentSpendDbRow): AgentSpendRow {
  return {
    agentSlug: row.agentSlug,
    dayUtc: row.dayUtc,
    totalMicroUsd: asMicroUsd(row.totalMicroUsd),
    lastLsn: lsnFromV1Number(row.lastLsn),
  };
}

function toTaskSpendRow(row: TaskSpendDbRow): TaskSpendRow {
  return {
    taskId: row.taskId,
    totalMicroUsd: asMicroUsd(row.totalMicroUsd),
    lastLsn: lsnFromV1Number(row.lastLsn),
  };
}

function toBudgetRow(row: BudgetDbRow): BudgetRow {
  const parsedThresholds: unknown = JSON.parse(row.thresholdsBps);
  if (!Array.isArray(parsedThresholds) || parsedThresholds.some((n) => typeof n !== "number")) {
    throw new Error(`cost_budgets.thresholds_bps is not a number array for budget ${row.budgetId}`);
  }
  return {
    budgetId: row.budgetId as BudgetId,
    scope: row.scope as BudgetScope,
    subjectId: row.subjectId,
    limitMicroUsd: asMicroUsd(row.limitMicroUsd),
    thresholdsBps: parsedThresholds as readonly number[],
    setAtLsn: lsnFromV1Number(row.setAtLsn),
    tombstoned: row.tombstoned === 1,
  };
}

function toThresholdCrossingRow(row: ThresholdCrossingDbRow): ThresholdCrossingRow {
  return {
    budgetId: row.budgetId as BudgetId,
    budgetSetLsn: lsnFromV1Number(row.budgetSetLsn),
    thresholdBps: row.thresholdBps,
    crossedAtLsn: lsnFromV1Number(row.crossedAtLsn),
    observedMicroUsd: asMicroUsd(row.observedMicroUsd),
    limitMicroUsd: asMicroUsd(row.limitMicroUsd),
  };
}

// Calendar-day key for `cost_by_agent`. UTC date string `YYYY-MM-DD`.
// Calendar-day reset (not rolling 24h) is the locked product decision.
function isoDateUtc(d: Date): string {
  return d.toISOString().slice(0, 10);
}
