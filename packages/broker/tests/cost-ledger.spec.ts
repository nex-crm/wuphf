import {
  asAgentSlug,
  asBudgetId,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadToBytes,
  lsnFromV1Number,
  parseLsn,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";
import {
  createCostLedger,
  parseIdempotencyKey,
  type ReplayDiscrepancy,
  runReplayCheck,
} from "../src/cost-ledger/index.ts";
import { __replayCheckTesting } from "../src/cost-ledger/replay-check.ts";
import { createEventLog, openDatabase, runMigrations } from "../src/event-log/index.ts";

function setup() {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const ledger = createCostLedger(db, eventLog);
  return { db, eventLog, ledger };
}

function buildCostEvent(opts: {
  amountMicroUsd: number;
  agentSlug?: string;
  taskId?: string;
  occurredAt?: Date;
}): CostEventAuditPayload {
  return {
    receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
    agentSlug: asAgentSlug(opts.agentSlug ?? "primary"),
    ...(opts.taskId !== undefined ? { taskId: asTaskId(opts.taskId) } : {}),
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    amountMicroUsd: asMicroUsd(opts.amountMicroUsd),
    units: { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0 },
    occurredAt: opts.occurredAt ?? new Date("2026-05-08T10:00:00.000Z"),
  };
}

function buildBudgetSet(opts: {
  budgetId?: string;
  scope?: "global" | "agent" | "task";
  subjectId?: string;
  limitMicroUsd: number;
  thresholdsBps?: readonly number[];
}): BudgetSetAuditPayload {
  return {
    budgetId: asBudgetId(opts.budgetId ?? "01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
    scope: opts.scope ?? "global",
    ...(opts.subjectId !== undefined ? { subjectId: opts.subjectId } : {}),
    limitMicroUsd: asMicroUsd(opts.limitMicroUsd),
    thresholdsBps: opts.thresholdsBps ?? [5_000, 8_000, 10_000],
    setBy: asSignerIdentity("operator@example.com"),
    setAt: new Date("2026-05-08T09:00:00.000Z"),
  };
}

describe("cost ledger projections", () => {
  it("appendCostEvent inserts event_log row and projection rows atomically", () => {
    const { db, ledger } = setup();
    const result = ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 2_500_000, taskId: "01BRZ3NDEKTSV4RRFFQ69G5FA0" }),
    );
    expect(result.lsn).toBe(lsnFromV1Number(1));
    expect(result.agentDayTotal as number).toBe(2_500_000);
    expect(result.taskTotal as number).toBe(2_500_000);
    expect(result.newCrossings).toEqual([]);

    const eventCount = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(eventCount?.n).toBe(1);

    const agentRow = ledger.getAgentSpend("primary", "2026-05-08");
    expect(agentRow?.totalMicroUsd as number).toBe(2_500_000);
    const taskRow = ledger.getTaskSpend("01BRZ3NDEKTSV4RRFFQ69G5FA0");
    expect(taskRow?.totalMicroUsd as number).toBe(2_500_000);
  });

  it("calendar-day reset: same agent, two different UTC days, two rows", () => {
    const { ledger } = setup();
    ledger.appendCostEvent(
      buildCostEvent({
        amountMicroUsd: 1_000_000,
        occurredAt: new Date("2026-05-08T22:00:00.000Z"),
      }),
    );
    ledger.appendCostEvent(
      buildCostEvent({
        amountMicroUsd: 1_500_000,
        occurredAt: new Date("2026-05-09T02:00:00.000Z"),
      }),
    );
    expect(ledger.getAgentSpend("primary", "2026-05-08")?.totalMicroUsd as number).toBe(1_000_000);
    expect(ledger.getAgentSpend("primary", "2026-05-09")?.totalMicroUsd as number).toBe(1_500_000);
  });

  it("§15.A I1 + I2 hold when every event is task-attributed (baseline)", () => {
    const { db, ledger } = setup();
    for (let i = 0; i < 20; i += 1) {
      const opts: { amountMicroUsd: number; taskId?: string } = {
        amountMicroUsd: 100_000 + i,
      };
      if (i % 2 === 0) opts.taskId = "01BRZ3NDEKTSV4RRFFQ69G5FA0";
      ledger.appendCostEvent(buildCostEvent(opts));
    }
    const eventSum = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(eventSum?.n).toBe(20);
    const agentSum = ledger
      .listAgentSpend()
      .reduce((acc, row) => acc + (row.totalMicroUsd as number), 0);
    const taskSum =
      db
        .prepare<[], { readonly s: number }>(
          "SELECT COALESCE(SUM(total_micro_usd), 0) AS s FROM cost_by_task",
        )
        .get()?.s ?? 0;
    // Expected: sum of all 20 amounts = 20*100_000 + (0+1+...+19) = 2_000_190
    const expected = 20 * 100_000 + (19 * 20) / 2;
    expect(agentSum).toBe(expected);
    // Tasks only get even-indexed events: 10 events, amounts 100_000..100_018 step 2
    // = 10*100_000 + (0+2+4+...+18) = 1_000_090
    expect(taskSum).toBe(10 * 100_000 + 2 * ((9 * 10) / 2));
  });
});

describe("cost ledger budget projection", () => {
  it("appendBudgetSet upserts the projection row", () => {
    const { ledger } = setup();
    const r1 = ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));
    expect(r1.tombstoned).toBe(false);

    const row = ledger.getBudget(asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"));
    expect(row?.limitMicroUsd as number).toBe(5_000_000);
    expect(row?.tombstoned).toBe(false);
    expect(row?.thresholdsBps).toEqual([5_000, 8_000, 10_000]);
  });

  it("tombstone (limit=0) flips tombstoned flag; event row stays", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 0 }));
    const row = ledger.getBudget(asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"));
    expect(row?.tombstoned).toBe(true);
    expect(row?.limitMicroUsd as number).toBe(0);
    const eventCount = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.budget.set'",
      )
      .get();
    expect(eventCount?.n).toBe(2);
  });

  it("raising a budget mints a new set_at_lsn (re-arms thresholds)", () => {
    const { ledger } = setup();
    const first = ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));
    const second = ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 10_000_000 }));
    expect(second.lsn).not.toBe(first.lsn);
    const row = ledger.getBudget(asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"));
    expect(row?.setAtLsn).toBe(second.lsn);
    expect(row?.limitMicroUsd as number).toBe(10_000_000);
  });
});

describe("BudgetThresholdReactor", () => {
  it("global budget crosses thresholds when cumulative spend hits each bps level", () => {
    const { ledger } = setup();
    // Budget: $5 cap with 50%, 80%, 100% thresholds.
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));

    // First event: $2.50 — crosses 50%.
    const first = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    expect(first.newCrossings.length).toBe(1);
    expect(first.newCrossings[0]?.thresholdBps).toBe(5_000);
    expect(first.newCrossings[0]?.observedMicroUsd as number).toBe(2_500_000);

    // Second event: another $2.00 → cumulative $4.50 — crosses 80%.
    const second = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_000_000 }));
    expect(second.newCrossings.length).toBe(1);
    expect(second.newCrossings[0]?.thresholdBps).toBe(8_000);

    // Third event: another $0.60 → cumulative $5.10 — crosses 100%.
    const third = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 600_000 }));
    expect(third.newCrossings.length).toBe(1);
    expect(third.newCrossings[0]?.thresholdBps).toBe(10_000);

    // Fourth event: same threshold doesn't re-fire under same budget_set LSN.
    const fourth = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 }));
    expect(fourth.newCrossings).toEqual([]);
  });

  it("raising a budget re-arms thresholds (new budget_set LSN, new crossing rows)", () => {
    const { ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 1_000_000, thresholdsBps: [5_000] }));
    const e1 = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 500_000 }));
    expect(e1.newCrossings.length).toBe(1); // crossed 50% of $1

    // Raise budget to $5. Same threshold (50%) under new epoch should be
    // re-armed but observed total ($0.50) is below new threshold ($2.50).
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    const e2 = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 }));
    expect(e2.newCrossings).toEqual([]);

    // Now spend enough to cross 50% of $5 = $2.50. Cumulative is $0.60, need $1.90 more.
    const e3 = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 1_900_000 }));
    expect(e3.newCrossings.length).toBe(1);
    expect(e3.newCrossings[0]?.thresholdBps).toBe(5_000);
    // Crossings table now has two rows for (budget_id, threshold=5000): one
    // per budget_set epoch.
    const crossings = ledger.listThresholdCrossings();
    expect(crossings.length).toBe(2);
  });

  it("tombstoned budgets never fire crossings", () => {
    const { ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 1_000_000 }));
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 0 })); // tombstone
    const result = ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 5_000_000 }));
    expect(result.newCrossings).toEqual([]);
  });

  it("per-task budget only fires on matching taskId", () => {
    const { ledger } = setup();
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01BRZ3NDEKTSV4RRFFQ69G5FB0",
        scope: "task",
        subjectId: "01CRZ3NDEKTSV4RRFFQ69G5FC0",
        limitMicroUsd: 1_000_000,
        thresholdsBps: [5_000],
      }),
    );
    // Event with a DIFFERENT taskId: must not affect the task budget.
    const noMatch = ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 800_000, taskId: "01CRZ3NDEKTSV4RRFFQ69G5FZZ" }),
    );
    expect(noMatch.newCrossings).toEqual([]);

    // Event with the matching taskId: should cross.
    const match = ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 600_000, taskId: "01CRZ3NDEKTSV4RRFFQ69G5FC0" }),
    );
    expect(match.newCrossings.length).toBe(1);
    expect(match.newCrossings[0]?.thresholdBps).toBe(5_000);
  });
});

describe("idempotency", () => {
  it("parses cmd_<command>_<ULID> shape and rejects others", () => {
    const ok = parseIdempotencyKey("cmd_cost.event_01ARZ3NDEKTSV4RRFFQ69G5FAV", "cost.event");
    expect(ok.ok).toBe(true);
    const wrongCommand = parseIdempotencyKey(
      "cmd_cost.event_01ARZ3NDEKTSV4RRFFQ69G5FAV",
      "cost.budget.set",
    );
    expect(wrongCommand.ok).toBe(false);
    if (!wrongCommand.ok) {
      expect(wrongCommand.error.code).toBe("command_mismatch");
    }
    expect(parseIdempotencyKey("not_a_command_key", "cost.event").ok).toBe(false);
    expect(parseIdempotencyKey(undefined, "cost.event").ok).toBe(false);
    expect(parseIdempotencyKey("cmd_unknown_01ARZ3NDEKTSV4RRFFQ69G5FAV", "cost.event").ok).toBe(
      false,
    );
  });

  it("appendCostEventIdempotent: lookup+append+store atomic (B1)", () => {
    const { db, ledger } = setup();
    const parsed = parseIdempotencyKey("cmd_cost.event_01ARZ3NDEKTSV4RRFFQ69G5FAV", "cost.event");
    expect(parsed.ok).toBe(true);
    if (!parsed.ok) return;

    const r1 = ledger.appendCostEventIdempotent({
      payload: buildCostEvent({ amountMicroUsd: 1_000_000 }),
      idempotency: parsed.key,
      nowMs: 1700000000000,
      render: (applied) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify({ lsn: applied.lsn }), "utf8"),
      }),
    });
    expect(r1.replayed).toBe(false);
    expect(r1.statusCode).toBe(201);

    // Same key: must replay byte-for-byte WITHOUT appending another event.
    const r2 = ledger.appendCostEventIdempotent({
      payload: buildCostEvent({ amountMicroUsd: 9_999_999 }), // payload ignored on replay
      idempotency: parsed.key,
      nowMs: 1700000000999,
      render: () => {
        throw new Error("render must not be called on replay");
      },
    });
    expect(r2.replayed).toBe(true);
    expect(r2.payload.toString()).toBe(r1.payload.toString());
    expect(r2.statusCode).toBe(r1.statusCode);

    // Only one event_log row.
    const count = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(count?.n).toBe(1);

    // Idempotency row exists with the response bytes.
    const idemRow = db
      .prepare<[string], { readonly statusCode: number; readonly responsePayload: Buffer }>(
        "SELECT status_code AS statusCode, response_payload AS responsePayload FROM command_idempotency WHERE idempotency_key = ?",
      )
      .get(parsed.key.raw);
    expect(idemRow?.statusCode).toBe(201);
    expect(Buffer.from(idemRow?.responsePayload ?? Buffer.alloc(0)).toString()).toBe(
      r1.payload.toString(),
    );
  });

  it("pruneIdempotencyOlderThan removes only expired replay rows", () => {
    const { db, ledger } = setup();
    const oldKey = parseIdempotencyKey("cmd_cost.event_01ARZ3NDEKTSV4RRFFQ69G5FAV", "cost.event");
    const freshKey = parseIdempotencyKey("cmd_cost.event_01ARZ3NDEKTSV4RRFFQ69G5FAW", "cost.event");
    expect(oldKey.ok).toBe(true);
    expect(freshKey.ok).toBe(true);
    if (!oldKey.ok || !freshKey.ok) return;

    ledger.appendCostEventIdempotent({
      payload: buildCostEvent({ amountMicroUsd: 1_000_000 }),
      idempotency: oldKey.key,
      nowMs: 1_000,
      render: (applied) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify({ lsn: applied.lsn }), "utf8"),
      }),
    });
    ledger.appendCostEventIdempotent({
      payload: buildCostEvent({ amountMicroUsd: 2_000_000 }),
      idempotency: freshKey.key,
      nowMs: 5_000,
      render: (applied) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify({ lsn: applied.lsn }), "utf8"),
      }),
    });

    expect(ledger.pruneIdempotencyOlderThan(3_000)).toBe(1);

    const idempotencyRows = db
      .prepare<[], { readonly idempotencyKey: string }>(
        "SELECT idempotency_key AS idempotencyKey FROM command_idempotency ORDER BY idempotency_key ASC",
      )
      .all();
    expect(idempotencyRows).toEqual([{ idempotencyKey: freshKey.key.raw }]);

    const eventCountAfterPrune = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(eventCountAfterPrune?.n).toBe(2);
    expect(ledger.getAgentSpend("primary", "2026-05-08")?.totalMicroUsd as number).toBe(3_000_000);

    const reapplied = ledger.appendCostEventIdempotent({
      payload: buildCostEvent({ amountMicroUsd: 4_000_000 }),
      idempotency: oldKey.key,
      nowMs: 6_000,
      render: (applied) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify({ lsn: applied.lsn }), "utf8"),
      }),
    });
    expect(reapplied.replayed).toBe(false);
    const eventCountAfterReapply = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(eventCountAfterReapply?.n).toBe(3);
  });
});

describe("replay-check", () => {
  it("reports ok when projection matches event_log", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));
    for (let i = 0; i < 5; i += 1) {
      ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 + i }));
    }
    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(report.discrepancies).toEqual([]);
  });

  it("detects projection drift when a row is silently tampered", () => {
    const { db, ledger } = setup();
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 1_000_000 }));
    // Manual tamper: drop the cost_by_agent row so replay finds a missing
    // aggregate that the event_log says should exist.
    db.exec("DELETE FROM cost_by_agent");
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    expect(report.discrepancies[0]?.kind).toBe("agent_day_row_missing");
  });

  it("detects budget row tampering", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000 }));
    db.exec("UPDATE cost_budgets SET limit_micro_usd = 999 WHERE 1=1");
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find((d) => d.kind === "budget_state_mismatch");
    expect(mismatch).toBeDefined();
  });

  it("replay-check detects missing threshold crossing rows (H1)", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    // Crosses 50% — produces one threshold-crossed event AND projection row.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    // Tamper: delete the projection row but leave the event in event_log.
    db.exec("DELETE FROM cost_threshold_crossings");
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const missing = report.discrepancies.find((d) => d.kind === "threshold_crossing_missing");
    expect(missing).toBeDefined();
  });

  it("replay-check detects ghost threshold crossing rows (H1)", () => {
    const { db, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    const budgetSetLsnInt = parseLsn(setResult.lsn).localLsn;
    // Inject a stray crossing row that points to the existing budget_set
    // event LSN (so the FK is satisfied), but with a threshold the
    // event_log has no matching `cost.budget.threshold.crossed` for.
    db.prepare<[string, number, number, number, number, number]>(
      `INSERT INTO cost_threshold_crossings
         (budget_id, budget_set_lsn, threshold_bps, crossed_at_lsn, observed_micro_usd, limit_micro_usd)
       VALUES (?, ?, ?, ?, ?, ?)`,
    ).run("01ARZ3NDEKTSV4RRFFQ69G5FAZ", budgetSetLsnInt, 5_000, budgetSetLsnInt, 100, 5_000_000);
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const ghost = report.discrepancies.find((d) => d.kind === "threshold_crossing_ghost");
    expect(ghost).toBeDefined();
  });

  it("replay-check detects threshold crossing field mismatch (H1)", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    // Tamper the observed amount.
    db.exec("UPDATE cost_threshold_crossings SET observed_micro_usd = 99 WHERE 1=1");
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const fieldMismatch = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_field_mismatch",
    );
    expect(fieldMismatch).toBeDefined();
    if (fieldMismatch?.kind === "threshold_crossing_field_mismatch") {
      expect(fieldMismatch.field).toBe("observedMicroUsd");
    }
  });

  it("§15.A I1 + I2 hold even when some cost_events are taskless (B2)", () => {
    const { db, ledger } = setup();
    // Mix of task-attributed and taskless events. Per the B2 wording,
    // I1 covers both (agent projection always updates); I2 covers only
    // the task-attributed subset.
    ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 100_000, taskId: "01BRZ3NDEKTSV4RRFFQ69G5FA0" }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 250_000 })); // taskless
    ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 50_000, taskId: "01BRZ3NDEKTSV4RRFFQ69G5FA0" }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 25_000 })); // taskless

    // I1: total in event_log == total in cost_by_agent.
    const eventTotal = 100_000 + 250_000 + 50_000 + 25_000;
    const agentTotal = ledger
      .listAgentSpend()
      .reduce((acc, r) => acc + (r.totalMicroUsd as number), 0);
    expect(agentTotal).toBe(eventTotal);

    // I2: sum of task-attributed events == cost_by_task.total.
    const taskAttributedTotal = 100_000 + 50_000;
    const taskRow = ledger.getTaskSpend("01BRZ3NDEKTSV4RRFFQ69G5FA0");
    expect(taskRow?.totalMicroUsd as number).toBe(taskAttributedTotal);

    // Replay-check is ok — taskless events don't break it.
    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(report.discrepancies).toEqual([]);
  });

  it("surfaces unparseable cost event payload as structured discrepancy (#822)", () => {
    const { db, ledger } = setup();
    // One good event so the projection has something to compare against,
    // then tamper a later row to be unparseable.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 250_000 }));
    // Overwrite the second cost.event payload with garbage JSON so the
    // codec rejects it. (We pick the highest cost.event LSN.)
    const tamperedLsn = db
      .prepare<[], { readonly lsn: number }>(
        "SELECT MAX(lsn) AS lsn FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    if (tamperedLsn === undefined) throw new Error("no cost.event row to tamper");
    db.prepare<[Buffer, number]>("UPDATE event_log SET payload = ? WHERE lsn = ?").run(
      Buffer.from('{"not":"a valid cost event"}', "utf8"),
      tamperedLsn.lsn,
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const parseFail = report.discrepancies.find((d) => d.kind === "event_payload_unparseable");
    expect(parseFail).toBeDefined();
    if (parseFail?.kind === "event_payload_unparseable") {
      expect(parseFail.type).toBe("cost.event");
      expect(typeof parseFail.reason).toBe("string");
      expect(parseFail.reason.length).toBeGreaterThan(0);
    }
  });
});

describe("replay-check oracle (#836)", () => {
  it("flags threshold_crossing_unemitted when reactor under-emits", () => {
    // Simulates a reactor bug that fails to emit a crossing: the threshold
    // event AND its projection row are both missing, so the event-log-as-
    // oracle replay can't see the omission. The oracle's independent
    // computation should still flag it.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    db.exec("DELETE FROM event_log WHERE type = 'cost.budget.threshold.crossed'");
    db.exec("DELETE FROM cost_threshold_crossings");

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const unemitted = report.discrepancies.find((d) => d.kind === "threshold_crossing_unemitted");
    expect(unemitted).toBeDefined();
    if (unemitted?.kind === "threshold_crossing_unemitted") {
      expect(unemitted.thresholdBps).toBe(5_000);
      expect(unemitted.observedMicroUsdString).toBe("2500000");
      expect(unemitted.limitMicroUsd as number).toBe(5_000_000);
    }
    // The existing event-log replay sees nothing wrong (both projection
    // and log are silent), so the only discrepancy is the oracle's.
    expect(
      report.discrepancies.filter(
        (d) => d.kind === "threshold_crossing_missing" || d.kind === "threshold_crossing_ghost",
      ),
    ).toEqual([]);
  });

  it("flags threshold_crossing_spurious when reactor over-emits", () => {
    const { db, eventLog, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    // A tiny cost event: cumulative spend stays at 1% of the limit, so no
    // threshold should fire.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 50_000 }));

    // Hand-craft a stray `cost.budget.threshold.crossed` event and matching
    // projection row that the oracle's math says shouldn't exist.
    const budgetSetLsnInt = parseLsn(setResult.lsn).localLsn;
    const crossingPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: setResult.lsn,
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(budgetSetLsnInt),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const bytes = costAuditPayloadToBytes("budget_threshold_crossed", crossingPayload);
    const crossingLsn = eventLog.append({
      type: "cost.budget.threshold.crossed",
      payload: Buffer.from(bytes),
    });
    db.prepare<[string, number, number, number, number, number]>(
      `INSERT INTO cost_threshold_crossings
         (budget_id, budget_set_lsn, threshold_bps, crossed_at_lsn, observed_micro_usd, limit_micro_usd)
       VALUES (?, ?, ?, ?, ?, ?)`,
    ).run("01ARZ3NDEKTSV4RRFFQ69G5FAZ", budgetSetLsnInt, 5_000, crossingLsn, 2_500_000, 5_000_000);

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const spurious = report.discrepancies.find((d) => d.kind === "threshold_crossing_spurious");
    expect(spurious).toBeDefined();
    if (spurious?.kind === "threshold_crossing_spurious") {
      expect(spurious.thresholdBps).toBe(5_000);
      // Round-4 wire-shape assertion (PR #845 round-4 codex api lens
      // finding 3): a regression that re-emits `observedMicroUsd:
      // MicroUsd` (forging the brand on a cumulative value) must fail
      // this. The shipping shape is a decimal-string field.
      expect(typeof spurious.observedMicroUsdString).toBe("string");
      expect(spurious.observedMicroUsdString).toBe("2500000");
      expect((spurious as { observedMicroUsd?: unknown }).observedMicroUsd).toBeUndefined();
    }
  });

  it("flags threshold_crossing_oracle_observed_mismatch when observed disagrees", () => {
    // Reactor emits a crossing, but the recorded observedMicroUsd is
    // tampered to a different value. The oracle's independent computation
    // should disagree even though the keys match.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    // Tamper the threshold-crossed event payload in event_log so the
    // replayed observedMicroUsd doesn't match the oracle. Need a valid
    // payload — just change the observed value.
    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(1),
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_001), // off by one
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );
    // Also tamper the projection so the existing log-vs-projection
    // comparator doesn't fire its own discrepancy and obscure the test.
    db.exec("UPDATE cost_threshold_crossings SET observed_micro_usd = 2500001");

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_oracle_observed_mismatch",
    );
    expect(mismatch).toBeDefined();
    if (mismatch?.kind === "threshold_crossing_oracle_observed_mismatch") {
      expect(mismatch.expectedMicroUsdString).toBe("2500000");
      expect(mismatch.loggedMicroUsdString).toBe("2500001");
    }
  });

  it("flags threshold_crossing_oracle_field_mismatch on crossedAtLsn + limitMicroUsd drift", () => {
    // Cover the remaining two field-mismatch branches in one shot: tamper
    // the threshold-crossed event payload so BOTH crossedAtLsn and
    // limitMicroUsd disagree with the oracle's computed values.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(1),
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_001), // off by one
      crossedAtLsn: lsnFromV1Number(99), // wrong LSN
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );
    // The projection row stays at the original (correct) values, so the
    // existing log-vs-projection comparator will ALSO fire field_mismatch
    // discrepancies for the same fields. That's expected — the new
    // oracle_field_mismatch variant is what this test verifies.

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const fields = new Set(
      report.discrepancies
        .filter(
          (d): d is Extract<typeof d, { kind: "threshold_crossing_oracle_field_mismatch" }> =>
            d.kind === "threshold_crossing_oracle_field_mismatch",
        )
        .map((d) => d.field),
    );
    expect(fields.has("crossedAtLsn")).toBe(true);
    expect(fields.has("limitMicroUsd")).toBe(true);
  });

  it("oracle stays silent on a healthy flow with multiple crossings", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000, 8_000, 10_000] }),
    );
    // 50% crosses 5000bps.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    // 90% crosses 5000+8000bps (already fired) — only 8000 newly fires.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_000_000 }));

    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(
      report.discrepancies.filter(
        (d) =>
          d.kind === "threshold_crossing_unemitted" ||
          d.kind === "threshold_crossing_spurious" ||
          d.kind === "threshold_crossing_oracle_field_mismatch",
      ),
    ).toEqual([]);
  });

  it("oracle skips tombstoned budgets", () => {
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    // Tombstone (set limit to 0) BEFORE any cost event: same budgetId,
    // limit=0 marks it tombstoned in replay.
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
        limitMicroUsd: 0,
        thresholdsBps: [5_000],
      }),
    );
    // Spend that WOULD have crossed the original threshold; tombstoned
    // budget must produce no expected crossing.
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(report.discrepancies.filter((d) => d.kind === "threshold_crossing_unemitted")).toEqual(
      [],
    );
  });

  it("oracle re-arms thresholds on new budget_set epoch", () => {
    const { db, ledger } = setup();
    // Epoch 1: limit 1_000_000, threshold 50%, observed 500_000 — crosses.
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 1_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 500_000 }));
    // Re-set the SAME budgetId at a higher limit — new setAtLsn re-arms.
    // Cumulative spend is now 500_000; at limit 10_000_000, that's 5%,
    // below the 50% threshold. The next cost_event brings it to 5_500_000
    // (55%) which crosses 5000bps under the NEW epoch.
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
        limitMicroUsd: 10_000_000,
        thresholdsBps: [5_000],
      }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 5_000_000 }));

    // Both crossings are emitted by the reactor + present in the log; the
    // oracle should agree.
    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    // Stronger assertion: verify BOTH epochs produced a logged threshold
    // event. A future regression that skipped the second epoch would
    // still pass `ok === true` if no spurious entry fires.
    const crossingCount = db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.budget.threshold.crossed'",
      )
      .get();
    expect(crossingCount?.n).toBe(2);
    const projectionRows = db
      .prepare<[], { readonly budgetSetLsn: number }>(
        "SELECT budget_set_lsn AS budgetSetLsn FROM cost_threshold_crossings ORDER BY budget_set_lsn ASC",
      )
      .all();
    expect(projectionRows.length).toBe(2);
    expect(projectionRows[0]?.budgetSetLsn).not.toBe(projectionRows[1]?.budgetSetLsn);
  });

  it("oracle covers global + agent + task budgets independently", () => {
    const { db, ledger } = setup();
    // 3 budgets of different scope, all targeting the same agent+task.
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
        scope: "global",
        limitMicroUsd: 5_000_000,
        thresholdsBps: [5_000],
      }),
    );
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01BRZ3NDEKTSV4RRFFQ69G5FB1",
        scope: "agent",
        subjectId: "primary",
        limitMicroUsd: 3_000_000,
        thresholdsBps: [5_000],
      }),
    );
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01CRZ3NDEKTSV4RRFFQ69G5FB2",
        scope: "task",
        subjectId: "01DRZ3NDEKTSV4RRFFQ69G5FB3",
        limitMicroUsd: 1_000_000,
        thresholdsBps: [5_000],
      }),
    );
    // One cost event that crosses 50% on the task budget (smallest).
    // Cumulative for global=600_000 (12% — under), agent=600_000 (20% —
    // under), task=600_000 (60% — crosses).
    ledger.appendCostEvent(
      buildCostEvent({ amountMicroUsd: 600_000, taskId: "01DRZ3NDEKTSV4RRFFQ69G5FB3" }),
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(
      report.discrepancies.filter(
        (d) =>
          d.kind === "threshold_crossing_unemitted" ||
          d.kind === "threshold_crossing_spurious" ||
          d.kind === "threshold_crossing_oracle_field_mismatch",
      ),
    ).toEqual([]);
  });
});

describe("replay-check oracle round-2 fixes (#841)", () => {
  it("flags threshold_crossing_duplicate_event when same key appears twice in event_log", () => {
    // Reactor over-emission OR a forged duplicate audit event. The
    // projection PK collapses duplicates to one row, so the prior
    // log-vs-projection comparator can't see it. Multi-entry tracking
    // surfaces both event LSNs.
    const { db, eventLog, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    // Inject a SECOND threshold-crossed event with the same crossing key.
    const budgetSetLsnInt = parseLsn(setResult.lsn).localLsn;
    const dupPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: setResult.lsn,
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2), // matches the legitimate trigger
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const dupBytes = costAuditPayloadToBytes("budget_threshold_crossed", dupPayload);
    const dupLsn = eventLog.append({
      type: "cost.budget.threshold.crossed",
      payload: Buffer.from(dupBytes),
    });

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const duplicate = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_duplicate_event",
    );
    expect(duplicate).toBeDefined();
    if (duplicate?.kind === "threshold_crossing_duplicate_event") {
      expect(duplicate.thresholdBps).toBe(5_000);
      expect(duplicate.eventLsns.length).toBe(2);
      // First entry is the legitimate emit at LSN 3; second is dup at dupLsn.
      expect(duplicate.eventLsns[1]).toBe(lsnFromV1Number(dupLsn));
    }
    // Sanity: budgetSetLsnInt is used to construct the payload's
    // budgetSetLsn; assert the duplicate references it.
    if (duplicate?.kind === "threshold_crossing_duplicate_event") {
      expect(duplicate.budgetSetLsn).toBe(lsnFromV1Number(budgetSetLsnInt));
    }
  });

  it("flags threshold_crossing_causal_order_violation when eventLsn <= crossedAtLsn", () => {
    // The reactor appends threshold-crossed AFTER its triggering cost
    // event, so threshold-event-LSN must strictly exceed crossedAtLsn.
    // Tamper a payload to claim a LATER trigger LSN than the event row
    // itself.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    // Legitimate threshold event landed at LSN 3 with crossedAtLsn=2.
    // Tamper crossedAtLsn to LSN 99 (which would make 99 a "future"
    // cost event the threshold event claims as its trigger — causal
    // order violated).
    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(1),
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(99),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const violation = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_causal_order_violation",
    );
    expect(violation).toBeDefined();
    if (violation?.kind === "threshold_crossing_causal_order_violation") {
      expect(violation.eventLsn).toBe(lsnFromV1Number(3));
      expect(violation.crossedAtLsn).toBe(lsnFromV1Number(99));
    }
  });

  it("flags threshold_crossing_oracle_field_mismatch on crossedAt (forged timestamp)", () => {
    // Forged `crossedAt` payload field. Reactor sets crossedAt from the
    // triggering cost event's occurredAt; tampering this field should
    // surface as field=crossedAtMs.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(1),
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2),
      crossedAt: new Date("2099-12-31T23:59:59.000Z"), // future timestamp
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find(
      (d): d is Extract<typeof d, { kind: "threshold_crossing_oracle_field_mismatch" }> =>
        d.kind === "threshold_crossing_oracle_field_mismatch" && d.field === "crossedAtMs",
    );
    expect(mismatch).toBeDefined();
    if (mismatch !== undefined) {
      expect(mismatch.expected).toBe(new Date("2026-05-08T10:00:00.000Z").getTime());
      expect(mismatch.logged).toBe(new Date("2099-12-31T23:59:59.000Z").getTime());
    }
  });

  it("surfaces eventLsn on threshold_crossing_spurious for on-call repair", () => {
    // Reuse the spurious setup: oracle says no crossing should fire,
    // but a stray event was injected. The new `eventLsn` field must
    // point at that injected event_log row.
    const { db, eventLog, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 50_000 }));

    const budgetSetLsnInt = parseLsn(setResult.lsn).localLsn;
    const strayPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: setResult.lsn,
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(budgetSetLsnInt),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const strayBytes = costAuditPayloadToBytes("budget_threshold_crossed", strayPayload);
    const strayLsn = eventLog.append({
      type: "cost.budget.threshold.crossed",
      payload: Buffer.from(strayBytes),
    });
    db.prepare<[string, number, number, number, number, number]>(
      `INSERT INTO cost_threshold_crossings
         (budget_id, budget_set_lsn, threshold_bps, crossed_at_lsn, observed_micro_usd, limit_micro_usd)
       VALUES (?, ?, ?, ?, ?, ?)`,
    ).run("01ARZ3NDEKTSV4RRFFQ69G5FAZ", budgetSetLsnInt, 5_000, strayLsn, 2_500_000, 5_000_000);

    const report = runReplayCheck(db);
    const spurious = report.discrepancies.find((d) => d.kind === "threshold_crossing_spurious");
    expect(spurious).toBeDefined();
    if (spurious?.kind === "threshold_crossing_spurious") {
      expect(spurious.eventLsn).toBe(lsnFromV1Number(strayLsn));
    }
  });
});

describe("replay-check oracle round-2 fixes (PR #841 r2)", () => {
  it("flags threshold_crossing_dangling_reference when crossedAtLsn names a non-cost-event row", () => {
    // Tamper the threshold-crossed event payload so crossedAtLsn points
    // at the budget_set LSN (LSN 1) — a real event_log row, but not a
    // cost.event. The numeric causal-order check passes because the
    // event row LSN (3) > crossedAtLsn (1); only the per-entry reference
    // validator can see this.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(1),
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(1), // tampered: LSN 1 is the budget_set, not a cost.event
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const dangling = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_dangling_reference",
    );
    expect(dangling).toBeDefined();
    if (dangling?.kind === "threshold_crossing_dangling_reference") {
      expect(dangling.eventLsn).toBe(lsnFromV1Number(3));
      expect(dangling.crossedAtLsn).toBe(lsnFromV1Number(1));
    }
  });

  it("survives unparseable thresholds_bps projection cell without crashing the diagnostic", () => {
    // Round-2 finding (security MED): `parseStoredThresholds` used to
    // bare `JSON.parse` the projection cell — a corrupted DB row would
    // throw out of `runReplayCheck` and blind the diagnostic. Now the
    // bad cell surfaces as a structured discrepancy.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    // Corrupt the stored thresholds JSON.
    db.exec("UPDATE cost_budgets SET thresholds_bps = '{invalid json' WHERE 1=1");

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find(
      (d): d is Extract<typeof d, { kind: "budget_state_mismatch" }> =>
        d.kind === "budget_state_mismatch" && d.field === "thresholdsBps",
    );
    expect(mismatch).toBeDefined();
    if (mismatch !== undefined) {
      const stored = mismatch.stored as { unparseable?: string; raw?: string };
      expect(typeof stored.unparseable).toBe("string");
      expect(stored.raw).toBe("{invalid json");
    }
  });

  it("per-entry validation catches duplicate with divergent crossedAt", () => {
    // Two threshold events for the same key. The first agrees with the
    // oracle's expected timestamp; the second tampers `crossedAt`. The
    // existing `_oracle_field_mismatch` check only looks at the first
    // entry, but per-entry validation should still flag the second.
    const { db, eventLog, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const tamperedDuplicate: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: setResult.lsn,
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2),
      crossedAt: new Date("2099-12-31T00:00:00.000Z"), // forged
    };
    const dupBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedDuplicate);
    const dupLsn = eventLog.append({
      type: "cost.budget.threshold.crossed",
      payload: Buffer.from(dupBytes),
    });

    const report = runReplayCheck(db);
    // Both the duplicate and a per-entry crossedAt mismatch should fire.
    const dupes = report.discrepancies.find((d) => d.kind === "threshold_crossing_duplicate_event");
    expect(dupes).toBeDefined();
    const crossedAtMismatches = report.discrepancies.filter(
      (d): d is Extract<typeof d, { kind: "threshold_crossing_oracle_field_mismatch" }> =>
        d.kind === "threshold_crossing_oracle_field_mismatch" && d.field === "crossedAtMs",
    );
    // The duplicate's eventLsn should appear in a crossedAtMs mismatch.
    const dupEntry = crossedAtMismatches.find((d) => d.eventLsn === lsnFromV1Number(dupLsn));
    expect(dupEntry).toBeDefined();
    if (dupEntry !== undefined) {
      expect(dupEntry.logged).toBe(new Date("2099-12-31T00:00:00.000Z").getTime());
    }
  });
});

describe("replay-check oracle round-3 fixes (PR #841 r3)", () => {
  it("flags threshold_crossing_delayed_emission when a forged event is appended outside the contiguous window", () => {
    // Reactor invariant: threshold events are appended SYNCHRONOUSLY
    // right after their cost.event. A legitimate cost.event at LSN 2
    // that crosses one threshold yields a threshold-crossed at LSN 3
    // — window {3}. A forger appending another threshold-crossed for
    // the same key MUCH LATER (after intervening events) would
    // otherwise pass duplicate, causal-order, reference, and
    // crossedAt checks if the duplicate's payload is otherwise
    // identical to the first. The window check catches the gap.
    //
    // We provoke a delayed emission by emitting a legitimate first
    // crossing, appending unrelated events that push the LSN forward,
    // then injecting a forged duplicate at a much later LSN.
    const { db, eventLog, ledger } = setup();
    const setResult = ledger.appendBudgetSet(
      buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    // Push LSN forward with two more cost events that don't fire any
    // new threshold (cumulative stays under 80%).
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 100_000 }));
    // Now inject a forged duplicate threshold-crossed event MUCH later
    // — outside the reactor's contiguous {LSN 3} window.
    const forgedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: setResult.lsn,
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const forgedBytes = costAuditPayloadToBytes("budget_threshold_crossed", forgedPayload);
    const forgedLsn = eventLog.append({
      type: "cost.budget.threshold.crossed",
      payload: Buffer.from(forgedBytes),
    });

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const delayed = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_delayed_emission",
    );
    expect(delayed).toBeDefined();
    if (delayed?.kind === "threshold_crossing_delayed_emission") {
      expect(delayed.eventLsn).toBe(lsnFromV1Number(forgedLsn));
      expect(delayed.crossedAtLsn).toBe(lsnFromV1Number(2));
      expect(delayed.expectedWindowMinLsn).toBe(lsnFromV1Number(3));
      expect(delayed.expectedWindowMaxLsn).toBe(lsnFromV1Number(3));
    }
  });

  it("flags threshold_crossing_invalid_budget_set_reference when budgetSetLsn names a non-budget LSN", () => {
    // Tamper budgetSetLsn to point at the cost.event LSN (LSN 2),
    // not the actual budget_set LSN (LSN 1). The threshold-event row
    // itself is at LSN 3.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));

    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(2), // tampered: LSN 2 is the cost.event, not budget_set
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(2),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const invalid = report.discrepancies.find(
      (d) => d.kind === "threshold_crossing_invalid_budget_set_reference",
    );
    expect(invalid).toBeDefined();
    if (invalid?.kind === "threshold_crossing_invalid_budget_set_reference") {
      expect(invalid.reason).toBe("lsn_not_a_budget_set");
      expect(invalid.eventLsn).toBe(lsnFromV1Number(3));
    }
  });

  it("flags threshold_crossing_invalid_budget_set_reference on budgetId mismatch", () => {
    // Two budgets exist. Tamper a threshold event so its budgetSetLsn
    // points at the OTHER budget's set event (real budget_set row,
    // but for a different budgetId).
    const { db, ledger } = setup();
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01ARZ3NDEKTSV4RRFFQ69G5FAZ",
        limitMicroUsd: 5_000_000,
        thresholdsBps: [5_000],
      }),
    );
    // Second budget at LSN 2.
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: "01BRZ3NDEKTSV4RRFFQ69G5FB1",
        limitMicroUsd: 10_000_000,
        thresholdsBps: [5_000],
      }),
    );
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 })); // LSN 3, fires LSN 4 for budget 1

    const tamperedPayload: BudgetThresholdCrossedAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      budgetSetLsn: lsnFromV1Number(2), // tampered: LSN 2 is budget #2's set event
      thresholdBps: 5_000,
      observedMicroUsd: asMicroUsd(2_500_000),
      limitMicroUsd: asMicroUsd(5_000_000),
      crossedAtLsn: lsnFromV1Number(3),
      crossedAt: new Date("2026-05-08T10:00:00.000Z"),
    };
    const tamperedBytes = costAuditPayloadToBytes("budget_threshold_crossed", tamperedPayload);
    db.prepare<[Buffer, string]>("UPDATE event_log SET payload = ? WHERE type = ?").run(
      Buffer.from(tamperedBytes),
      "cost.budget.threshold.crossed",
    );

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find(
      (d): d is Extract<typeof d, { kind: "threshold_crossing_invalid_budget_set_reference" }> =>
        d.kind === "threshold_crossing_invalid_budget_set_reference" &&
        d.reason === "budget_id_mismatch",
    );
    expect(mismatch).toBeDefined();
    if (mismatch !== undefined) {
      expect(mismatch.actualBudgetId).toBe("01BRZ3NDEKTSV4RRFFQ69G5FB1");
    }
  });

  it("parseStoredThresholds rejects Infinity / non-safe-integer entries (round-3 LOW)", () => {
    // `JSON.parse("[1e999]")` returns `[Infinity]`. The round-2 check
    // only filtered non-numbers; round-3 strengthens to require
    // positive safe integers ≤ 10000.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    db.exec("UPDATE cost_budgets SET thresholds_bps = '[1e999]' WHERE 1=1");

    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find(
      (d): d is Extract<typeof d, { kind: "budget_state_mismatch" }> =>
        d.kind === "budget_state_mismatch" && d.field === "thresholdsBps",
    );
    expect(mismatch).toBeDefined();
    if (mismatch !== undefined) {
      const stored = mismatch.stored as { unparseable?: string };
      expect(typeof stored.unparseable).toBe("string");
      expect(stored.unparseable).toContain("invalid entry");
    }
  });
});

describe("replay-check oracle unsafe-lifetime (#843)", () => {
  // Direct unit tests against the internal helper. An end-to-end test
  // through `runReplayCheck` is blocked by the protocol per-event cap
  // (`MAX_COST_EVENT_AMOUNT_MICRO_USD = 1e8`), which would require
  // ~9e7 events to push an accumulator past `Number.MAX_SAFE_INTEGER`
  // (≈ 9e15 microUsd ≈ $9B cumulative spend). See the comment on
  // `__replayCheckTesting` in `replay-check.ts` for the rationale.
  const {
    flagUnsafeAccumulator,
    MAX_SAFE_INTEGER_BIG,
    MAX_BUDGET_LIMIT_MICRO_USD_BIG,
    crossesThresholdBigInt,
  } = __replayCheckTesting;

  it("flagUnsafeAccumulator: silent at or below the MicroUsd brand ceiling", () => {
    const out: ReplayDiscrepancy[] = [];
    const flagged = new Set<string>();
    // 1e12 — exactly at MAX_BUDGET_LIMIT_MICRO_USD. Strict `>` so no fire.
    flagUnsafeAccumulator("global", null, MAX_BUDGET_LIMIT_MICRO_USD_BIG, 42, flagged, out);
    expect(out).toHaveLength(0);
    expect(flagged.size).toBe(0);
  });

  it("flagUnsafeAccumulator: fires exceeds_micro_usd_brand at 1e12+1 but not exceeds_safe_integer yet", () => {
    const out: ReplayDiscrepancy[] = [];
    const flagged = new Set<string>();
    const post = MAX_BUDGET_LIMIT_MICRO_USD_BIG + 1n;
    flagUnsafeAccumulator("global", null, post, 42, flagged, out);
    expect(out).toHaveLength(1);
    const d = out[0];
    if (d === undefined || d.kind !== "unsafe_lifetime_accumulator") {
      throw new Error("expected unsafe_lifetime_accumulator");
    }
    expect(d.reason).toBe("exceeds_micro_usd_brand");
    expect(d.scope).toBe("global");
    expect(d.subjectId).toBeNull();
    expect(d.accumulatedMicroUsd).toBe(post.toString());
    expect(flagged.size).toBe(1);

    // Second call past the same boundary but still safe-integer-safe —
    // no second brand emission (already flagged).
    flagUnsafeAccumulator("global", null, post + 1_000_000_000n, 43, flagged, out);
    expect(out).toHaveLength(1);
    expect(flagged.size).toBe(1);
  });

  it("flagUnsafeAccumulator: fires exceeds_safe_integer separately at 2^53+1", () => {
    const out: ReplayDiscrepancy[] = [];
    const flagged = new Set<string>();
    // Past both boundaries → both reasons fire on the first call.
    const post = MAX_SAFE_INTEGER_BIG + 1n;
    flagUnsafeAccumulator("global", null, post, 42, flagged, out);
    expect(out).toHaveLength(2);
    const reasons = out
      .filter((d) => d.kind === "unsafe_lifetime_accumulator")
      .map((d) => (d.kind === "unsafe_lifetime_accumulator" ? d.reason : ""))
      .sort();
    expect(reasons).toEqual(["exceeds_micro_usd_brand", "exceeds_safe_integer"]);
    expect(flagged.size).toBe(2);

    // Repeat — no more emissions.
    flagUnsafeAccumulator("global", null, post + 100n, 43, flagged, out);
    expect(out).toHaveLength(2);
  });

  it("flagUnsafeAccumulator: agent and task scopes track distinct (scope, subjectId, reason) keys", () => {
    const out: ReplayDiscrepancy[] = [];
    const flagged = new Set<string>();
    // Past both boundaries → each call emits 2 (one per reason).
    const post = MAX_SAFE_INTEGER_BIG + 1n;
    flagUnsafeAccumulator("agent", "primary", post, 1, flagged, out);
    flagUnsafeAccumulator("agent", "secondary", post, 2, flagged, out);
    flagUnsafeAccumulator("task", "primary", post, 3, flagged, out);
    flagUnsafeAccumulator("global", null, post, 4, flagged, out);
    expect(out).toHaveLength(8); // 4 keys × 2 reasons
    expect(flagged.size).toBe(8);

    // Re-emit on each key — no new discrepancies.
    flagUnsafeAccumulator("agent", "primary", post + 1n, 5, flagged, out);
    flagUnsafeAccumulator("task", "primary", post + 1n, 6, flagged, out);
    flagUnsafeAccumulator("global", null, post + 1n, 7, flagged, out);
    expect(out).toHaveLength(8);
  });

  it("crossesThresholdBigInt: stays exact past Number.MAX_SAFE_INTEGER for observed", () => {
    // Observed = 2^53 + 100 (1 microUsd past the safe-integer boundary
    // by a margin that rounds when converted via `Number`).
    // Limit = 1e10 microUsd ($10k), threshold = 5000 bps (50%).
    // 2^53 + 100 ≥ 1e10 * 5000 / 10000 = 5e9 → true.
    const observed = MAX_SAFE_INTEGER_BIG + 100n;
    expect(crossesThresholdBigInt(observed, 10_000_000_000, 5_000)).toBe(true);
  });

  it("crossesThresholdBigInt: returns false when limit is zero (tombstoned)", () => {
    // Preserves the prior contract: tombstoned budgets (limit=0) never
    // cross any threshold, regardless of observed.
    expect(crossesThresholdBigInt(1_000_000_000n, 0, 5_000)).toBe(false);
  });

  it("threshold_crossing_unemitted emits observedMicroUsdString (no MicroUsd brand forgery)", () => {
    // Reactor under-emits: oracle says crossing should fire, but no
    // threshold-crossed event exists. The discrepancy must surface the
    // observed value via the decimal-string field rather than casting
    // an unbounded bigint into a `MicroUsd` brand.
    const { db, ledger } = setup();
    ledger.appendBudgetSet(buildBudgetSet({ limitMicroUsd: 5_000_000, thresholdsBps: [5_000] }));
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 2_500_000 }));
    db.exec("DELETE FROM event_log WHERE type = 'cost.budget.threshold.crossed'");
    db.exec("DELETE FROM cost_threshold_crossings");

    const report = runReplayCheck(db);
    const unemitted = report.discrepancies.find((d) => d.kind === "threshold_crossing_unemitted");
    expect(unemitted).toBeDefined();
    if (unemitted?.kind === "threshold_crossing_unemitted") {
      expect(unemitted.observedMicroUsdString).toBe("2500000");
      // Field is a plain `string`, not a branded number — no forgery risk.
      expect(typeof unemitted.observedMicroUsdString).toBe("string");
    }
  });
});

describe("replay-check aggregate paths round-4 (PR #845 r4 triangulation)", () => {
  // Round-4 fix for the api+security 2-of-2 convergent finding: the
  // aggregate-totals path (`compareAgentDays`, `compareTasks`) used to
  // accumulate as JS `number` and emit `replayed`/`stored` as branded
  // `MicroUsd`. Cumulative spend is unbounded — past
  // `MAX_BUDGET_LIMIT_MICRO_USD` it forges the brand; past
  // `Number.MAX_SAFE_INTEGER` (or a hostile projection row at that
  // boundary) it silently rounds. The accumulators are now bigint, the
  // SQLite reads use `.safeIntegers(true)`, and the discrepancies emit
  // decimal strings.
  //
  // End-to-end coverage past 2^53 is blocked by the protocol per-event
  // cap (same rationale as the threshold-oracle path). These tests
  // drive the compare helpers directly with synthetic bigint maps.
  const { compareAgentDays, compareTasks, MAX_SAFE_INTEGER_BIG, MAX_BUDGET_LIMIT_MICRO_USD_BIG } =
    __replayCheckTesting;

  it("agent_day_total_mismatch emits decimal-string fields past MAX_SAFE_INTEGER", () => {
    const replayed = new Map<string, bigint>([
      ["primary|2026-05-08", MAX_SAFE_INTEGER_BIG + 12_345n],
    ]);
    const stored = new Map<string, bigint>([
      ["primary|2026-05-08", MAX_SAFE_INTEGER_BIG + 67_890n],
    ]);
    const out: ReplayDiscrepancy[] = [];
    compareAgentDays(replayed, stored, out);
    expect(out).toHaveLength(1);
    const d = out[0];
    if (d === undefined || d.kind !== "agent_day_total_mismatch") {
      throw new Error("expected agent_day_total_mismatch");
    }
    expect(d.replayedMicroUsdString).toBe((MAX_SAFE_INTEGER_BIG + 12_345n).toString());
    expect(d.storedMicroUsdString).toBe((MAX_SAFE_INTEGER_BIG + 67_890n).toString());
    // No brand forgery: fields are plain strings, no `replayed`/`stored`
    // number leaks.
    expect(typeof d.replayedMicroUsdString).toBe("string");
    expect(typeof d.storedMicroUsdString).toBe("string");
    expect((d as { replayed?: unknown }).replayed).toBeUndefined();
    expect((d as { stored?: unknown }).stored).toBeUndefined();
  });

  it("task_total_mismatch emits decimal-string fields past MAX_BUDGET_LIMIT_MICRO_USD", () => {
    // Past the brand bound but still within safe-integer range.
    const replayed = new Map<string, bigint>([["task-A", MAX_BUDGET_LIMIT_MICRO_USD_BIG + 1n]]);
    const stored = new Map<string, bigint>([["task-A", MAX_BUDGET_LIMIT_MICRO_USD_BIG + 2n]]);
    const out: ReplayDiscrepancy[] = [];
    compareTasks(replayed, stored, out);
    expect(out).toHaveLength(1);
    const d = out[0];
    if (d === undefined || d.kind !== "task_total_mismatch") {
      throw new Error("expected task_total_mismatch");
    }
    expect(d.replayedMicroUsdString).toBe((MAX_BUDGET_LIMIT_MICRO_USD_BIG + 1n).toString());
    expect(d.storedMicroUsdString).toBe((MAX_BUDGET_LIMIT_MICRO_USD_BIG + 2n).toString());
    expect((d as { replayed?: unknown }).replayed).toBeUndefined();
    expect((d as { stored?: unknown }).stored).toBeUndefined();
  });

  it("agent_day_row_missing and _ghost emit decimal-string single fields", () => {
    const replayed = new Map<string, bigint>([["alpha|2026-05-08", 1_500_000_000n]]);
    const stored = new Map<string, bigint>([["beta|2026-05-08", 2_500_000_000n]]);
    const out: ReplayDiscrepancy[] = [];
    compareAgentDays(replayed, stored, out);
    expect(out).toHaveLength(2);

    const missing = out.find((d) => d.kind === "agent_day_row_missing");
    const ghost = out.find((d) => d.kind === "agent_day_row_ghost");
    if (missing?.kind !== "agent_day_row_missing") throw new Error("expected missing");
    if (ghost?.kind !== "agent_day_row_ghost") throw new Error("expected ghost");

    expect(missing.replayedMicroUsdString).toBe("1500000000");
    expect(typeof missing.replayedMicroUsdString).toBe("string");
    expect((missing as { replayed?: unknown }).replayed).toBeUndefined();

    expect(ghost.storedMicroUsdString).toBe("2500000000");
    expect(typeof ghost.storedMicroUsdString).toBe("string");
    expect((ghost as { stored?: unknown }).stored).toBeUndefined();
  });

  it("task_row_missing and _ghost emit decimal-string single fields", () => {
    const replayed = new Map<string, bigint>([["task-A", 7_777_777n]]);
    const stored = new Map<string, bigint>([["task-B", 8_888_888n]]);
    const out: ReplayDiscrepancy[] = [];
    compareTasks(replayed, stored, out);
    expect(out).toHaveLength(2);

    const missing = out.find((d) => d.kind === "task_row_missing");
    const ghost = out.find((d) => d.kind === "task_row_ghost");
    if (missing?.kind !== "task_row_missing") throw new Error("expected missing");
    if (ghost?.kind !== "task_row_ghost") throw new Error("expected ghost");

    expect(missing.replayedMicroUsdString).toBe("7777777");
    expect(ghost.storedMicroUsdString).toBe("8888888");
    expect((missing as { replayed?: unknown }).replayed).toBeUndefined();
    expect((ghost as { stored?: unknown }).stored).toBeUndefined();
  });

  it("compare functions silent when replayed equals stored at huge bigint values", () => {
    // Exact integer equality past 2^53 — a `number` accumulator would
    // round both sides and could either spuriously pass or spuriously
    // disagree. With bigint compare, equality is byte-exact.
    const huge = MAX_SAFE_INTEGER_BIG * 4n + 999_999_999n;
    const replayed = new Map<string, bigint>([["agent-X|2026-05-08", huge]]);
    const stored = new Map<string, bigint>([["agent-X|2026-05-08", huge]]);
    const out: ReplayDiscrepancy[] = [];
    compareAgentDays(replayed, stored, out);
    expect(out).toHaveLength(0);
  });

  it("end-to-end: agent_day_row_missing carries decimal-string from real cost event", () => {
    // Validates the wire shape end-to-end through `runReplayCheck`,
    // not just the helper. Drop the projection row to force a
    // missing-row discrepancy and confirm the field name + type.
    const { db, ledger } = setup();
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 1_000_000 }));
    db.exec("DELETE FROM cost_by_agent");
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const missing = report.discrepancies.find((d) => d.kind === "agent_day_row_missing");
    expect(missing).toBeDefined();
    if (missing?.kind === "agent_day_row_missing") {
      expect(typeof missing.replayedMicroUsdString).toBe("string");
      expect(missing.replayedMicroUsdString).toBe("1000000");
      expect((missing as { replayed?: unknown }).replayed).toBeUndefined();
    }
  });

  it("end-to-end: hostile projection row past MAX_SAFE_INTEGER reports exact bigint via string", () => {
    // The security lens's specific concern: stored projection rows are
    // hostile input. A row past 2^53 used to round through JS number
    // and the diagnostic would carry the rounded value. With
    // `.safeIntegers(true)` the bigint flows straight to the string
    // emission. Use 2^53 + 17 — outside JS-number exact range.
    const { db, ledger } = setup();
    ledger.appendCostEvent(buildCostEvent({ amountMicroUsd: 1_000_000 }));
    // Tamper the projection row to a hostile huge value. SQLite accepts
    // values up to 9_223_372_036_854_775_807 (signed 64-bit).
    const hostile = "9007199254741009"; // 2^53 + 17, exact decimal.
    db.exec(`UPDATE cost_by_agent SET total_micro_usd = ${hostile}`);
    const report = runReplayCheck(db);
    expect(report.ok).toBe(false);
    const mismatch = report.discrepancies.find((d) => d.kind === "agent_day_total_mismatch");
    expect(mismatch).toBeDefined();
    if (mismatch?.kind === "agent_day_total_mismatch") {
      expect(mismatch.storedMicroUsdString).toBe(hostile);
      // Replayed side is 1_000_000 from the single cost event.
      expect(mismatch.replayedMicroUsdString).toBe("1000000");
    }
  });
});
