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
import { createCostLedger, parseIdempotencyKey, runReplayCheck } from "../src/cost-ledger/index.ts";
import {
  __createBudgetCandidateIndexesForTesting,
  __replayCheckTesting,
} from "../src/cost-ledger/replay-check.ts";
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

function budgetIdFromNumber(n: number): string {
  return `01${String(n).padStart(24, "0")}`;
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
      expect(unemitted.observedMicroUsd as number).toBe(2_500_000);
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
    }
  });

  it("flags threshold_crossing_oracle_field_mismatch when observed disagrees", () => {
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
      (d) => d.kind === "threshold_crossing_oracle_field_mismatch",
    );
    expect(mismatch).toBeDefined();
    if (mismatch?.kind === "threshold_crossing_oracle_field_mismatch") {
      expect(mismatch.field).toBe("observedMicroUsd");
      expect(mismatch.expected).toBe(2_500_000);
      expect(mismatch.logged).toBe(2_500_001);
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

  it("oracle filters candidates across tombstones and scope transitions (#842)", () => {
    const { db, ledger } = setup();
    for (let i = 1; i <= 1_000; i += 1) {
      const budgetId = budgetIdFromNumber(i);
      const subjectId = `noise-${i}`;
      ledger.appendBudgetSet(
        buildBudgetSet({
          budgetId,
          scope: "agent",
          subjectId,
          limitMicroUsd: 1_000_000,
          thresholdsBps: [5_000],
        }),
      );
      ledger.appendBudgetSet(
        buildBudgetSet({
          budgetId,
          scope: "agent",
          subjectId,
          limitMicroUsd: 0,
          thresholdsBps: [5_000],
        }),
      );
    }

    const transitioningBudgetId = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: transitioningBudgetId,
        scope: "agent",
        subjectId: "legacy",
        limitMicroUsd: 10_000_000,
        thresholdsBps: [5_000],
      }),
    );
    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: transitioningBudgetId,
        scope: "global",
        limitMicroUsd: 1_000_000,
        thresholdsBps: [5_000],
      }),
    );

    const crossed = ledger.appendCostEvent(
      buildCostEvent({ agentSlug: "primary", amountMicroUsd: 600_000 }),
    );
    expect(crossed.newCrossings.length).toBe(1);

    ledger.appendBudgetSet(
      buildBudgetSet({
        budgetId: transitioningBudgetId,
        scope: "global",
        limitMicroUsd: 0,
        thresholdsBps: [5_000],
      }),
    );
    const afterTombstone = ledger.appendCostEvent(
      buildCostEvent({ agentSlug: "primary", amountMicroUsd: 600_000 }),
    );
    expect(afterTombstone.newCrossings).toEqual([]);

    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(report.eventsScanned).toBeGreaterThan(1_000);
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

describe("BudgetCandidateIndexes (#846 round-2)", () => {
  // Round-2 fix for the perf-test gap flagged 2-of-2 by triangulation
  // (perf + adversarial, LOW): the existing #842 regression test only
  // asserts `eventsScanned > 1_000`, which a revert to the
  // O(events × budgets) iteration would still satisfy. These tests
  // exercise the index helpers directly so a regression of the
  // candidate filter is caught structurally. Round-3 (staff review)
  // adds direct coverage of `computeExpectedCrossings` (the hot path)
  // plus positive task-scope coverage.
  const {
    addBudgetToIndex,
    removeBudgetFromIndex,
    replaceBudgetInIndex,
    computeExpectedCrossings,
  } = __replayCheckTesting;

  function makeReplayedBudget(opts: {
    scope: "global" | "agent" | "task";
    subjectId?: string;
    tombstoned?: boolean;
  }) {
    return {
      scope: opts.scope,
      subjectId: opts.subjectId ?? null,
      limitMicroUsd: opts.tombstoned ? 0 : 1_000_000,
      thresholdsBps: [5_000],
      setAtLsn: 1,
      tombstoned: opts.tombstoned ?? false,
    } as const;
  }

  it("adds and removes a global budget", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    const budget = makeReplayedBudget({ scope: "global" });
    addBudgetToIndex(indexes, "B1", budget);
    expect(indexes.globalBudgetIds.has("B1")).toBe(true);
    removeBudgetFromIndex(indexes, "B1", budget);
    expect(indexes.globalBudgetIds.has("B1")).toBe(false);
  });

  it("adds and removes an agent-scoped budget under its slug", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    const budget = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
    addBudgetToIndex(indexes, "B2", budget);
    expect(indexes.agentBudgetIds.get("primary")?.has("B2")).toBe(true);
    removeBudgetFromIndex(indexes, "B2", budget);
    // Empty subject set is cleaned up (Map.delete fires when set size hits 0).
    expect(indexes.agentBudgetIds.has("primary")).toBe(false);
  });

  it("isolates agent budgets across different slugs", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    addBudgetToIndex(indexes, "B3", makeReplayedBudget({ scope: "agent", subjectId: "primary" }));
    addBudgetToIndex(indexes, "B4", makeReplayedBudget({ scope: "agent", subjectId: "secondary" }));
    expect(indexes.agentBudgetIds.get("primary")?.has("B3")).toBe(true);
    expect(indexes.agentBudgetIds.get("secondary")?.has("B4")).toBe(true);
    expect(indexes.agentBudgetIds.get("primary")?.has("B4")).toBe(false);
    expect(indexes.agentBudgetIds.get("secondary")?.has("B3")).toBe(false);
  });

  it("handles agent → global scope transitions symmetrically", () => {
    // The replay loop removes-then-adds on every budget_set, so the
    // helper must move a budget cleanly between scope indexes.
    const indexes = __createBudgetCandidateIndexesForTesting();
    const agentForm = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
    const globalForm = makeReplayedBudget({ scope: "global" });
    addBudgetToIndex(indexes, "B5", agentForm);
    removeBudgetFromIndex(indexes, "B5", agentForm);
    addBudgetToIndex(indexes, "B5", globalForm);
    expect(indexes.agentBudgetIds.has("primary")).toBe(false);
    expect(indexes.globalBudgetIds.has("B5")).toBe(true);
  });

  it("noise budgets (tombstoned) leave the index empty after the lifecycle", () => {
    // Validates the reviewer's specific perf concern: with 1000
    // tombstoned budgets, the candidate index has 0 entries — no
    // matter how many cost events scan it.
    const indexes = __createBudgetCandidateIndexesForTesting();
    for (let i = 0; i < 1_000; i += 1) {
      const id = `B${i}`;
      const live = makeReplayedBudget({ scope: "agent", subjectId: `slug-${i}` });
      const tombstoned = makeReplayedBudget({
        scope: "agent",
        subjectId: `slug-${i}`,
        tombstoned: true,
      });
      addBudgetToIndex(indexes, id, live);
      removeBudgetFromIndex(indexes, id, tombstoned);
    }
    expect(indexes.globalBudgetIds.size).toBe(0);
    expect(indexes.agentBudgetIds.size).toBe(0);
    expect(indexes.taskBudgetIds.size).toBe(0);
  });

  // Task-scope positive coverage (round-3 staff finding 2). Round-2
  // only exercised global and agent scopes directly; a typo swapping
  // `taskBudgetIds`/`agentBudgetIds` in either helper would have left
  // the suite green.
  it("adds and removes a task-scoped budget under its task id", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    const budget = makeReplayedBudget({ scope: "task", subjectId: "task-A" });
    addBudgetToIndex(indexes, "B6", budget);
    expect(indexes.taskBudgetIds.get("task-A")?.has("B6")).toBe(true);
    expect(indexes.agentBudgetIds.has("task-A")).toBe(false);
    removeBudgetFromIndex(indexes, "B6", budget);
    expect(indexes.taskBudgetIds.has("task-A")).toBe(false);
  });

  it("isolates task budgets across different task ids", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    addBudgetToIndex(indexes, "B7", makeReplayedBudget({ scope: "task", subjectId: "task-A" }));
    addBudgetToIndex(indexes, "B8", makeReplayedBudget({ scope: "task", subjectId: "task-B" }));
    expect(indexes.taskBudgetIds.get("task-A")?.has("B7")).toBe(true);
    expect(indexes.taskBudgetIds.get("task-B")?.has("B8")).toBe(true);
    expect(indexes.taskBudgetIds.get("task-A")?.has("B8")).toBe(false);
    expect(indexes.taskBudgetIds.get("task-B")?.has("B7")).toBe(false);
  });

  it("handles agent → task scope transitions symmetrically", () => {
    const indexes = __createBudgetCandidateIndexesForTesting();
    const agentForm = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
    const taskForm = makeReplayedBudget({ scope: "task", subjectId: "task-A" });
    addBudgetToIndex(indexes, "B9", agentForm);
    removeBudgetFromIndex(indexes, "B9", agentForm);
    addBudgetToIndex(indexes, "B9", taskForm);
    expect(indexes.agentBudgetIds.has("primary")).toBe(false);
    expect(indexes.taskBudgetIds.get("task-A")?.has("B9")).toBe(true);
  });

  // Round-3 staff finding 1: direct coverage of `computeExpectedCrossings`,
  // the actual hot path. The five helper tests above test the index
  // shape; they would not catch a regression that re-introduces
  // `new Set(globalBudgetIds)` + merge in the hot path or, worse, a
  // regression that iterates the full `args.budgets` universe.
  // This test wraps `args.budgets` in a Proxy that throws on any
  // access other than `.get(id)` and `.has(id)`, proving the
  // implementation never iterates the universe; it also records every
  // `.get()` to assert only scope-matched ids are visited.
  describe("computeExpectedCrossings (hot path)", () => {
    type ReplayedBudgetShape = ReturnType<typeof makeReplayedBudget>;
    type ComputeExpectedCrossingsArgs = Parameters<typeof computeExpectedCrossings>[0];

    function makeHostileBudgets(entries: ReadonlyArray<[string, ReplayedBudgetShape]>) {
      const underlying = new Map(entries);
      const getCalls: string[] = [];
      const proxy = new Proxy(underlying, {
        get(_target, prop) {
          if (prop === "get") {
            return (key: string) => {
              getCalls.push(key);
              return underlying.get(key);
            };
          }
          if (prop === "has") return (key: string) => underlying.has(key);
          if (prop === "size") return underlying.size;
          throw new Error(
            `computeExpectedCrossings must access args.budgets only via .get(id); attempted .${String(prop)}`,
          );
        },
      });
      return { budgets: proxy as unknown as ReadonlyMap<string, ReplayedBudgetShape>, getCalls };
    }

    it("visits only scope-matched candidates, never iterates the universe", () => {
      // 100 noise budgets under agent "OTHER_AGENT" + 1 candidate
      // under "MY_AGENT" + 1 global + 1 under task "MY_TASK" + 5
      // noise budgets under task "OTHER_TASK". Call with
      // agentSlug="MY_AGENT", taskId="MY_TASK": expected 3 visits.
      const indexes = __createBudgetCandidateIndexesForTesting();
      const allBudgets: Array<[string, ReplayedBudgetShape]> = [];

      const myAgentBudget = makeReplayedBudget({ scope: "agent", subjectId: "MY_AGENT" });
      const globalBudget = makeReplayedBudget({ scope: "global" });
      const myTaskBudget = makeReplayedBudget({ scope: "task", subjectId: "MY_TASK" });
      addBudgetToIndex(indexes, "MY", myAgentBudget);
      addBudgetToIndex(indexes, "GLOBAL", globalBudget);
      addBudgetToIndex(indexes, "TASK_MY", myTaskBudget);
      allBudgets.push(["MY", myAgentBudget], ["GLOBAL", globalBudget], ["TASK_MY", myTaskBudget]);

      for (let i = 0; i < 100; i += 1) {
        const id = `OTHER_AGENT_${i}`;
        const noise = makeReplayedBudget({ scope: "agent", subjectId: "OTHER_AGENT" });
        addBudgetToIndex(indexes, id, noise);
        allBudgets.push([id, noise]);
      }
      for (let i = 0; i < 5; i += 1) {
        const id = `OTHER_TASK_${i}`;
        const noise = makeReplayedBudget({ scope: "task", subjectId: "OTHER_TASK" });
        addBudgetToIndex(indexes, id, noise);
        allBudgets.push([id, noise]);
      }

      const { budgets, getCalls } = makeHostileBudgets(allBudgets);
      const out: ComputeExpectedCrossingsArgs["out"] = new Map();
      // limitMicroUsd = 1_000_000 with threshold 5000bps fires when
      // observed >= 500_000. globalLifetime = 600_000 forces a
      // crossing on the visited candidates.
      computeExpectedCrossings({
        costEventLsn: 42,
        costEventOccurredAtMs: 1_700_000_000_000,
        agentSlug: "MY_AGENT",
        taskId: "MY_TASK",
        budgets,
        globalBudgetIds: indexes.globalBudgetIds,
        agentBudgetIds: indexes.agentBudgetIds,
        taskBudgetIds: indexes.taskBudgetIds,
        globalLifetime: 600_000,
        agentLifetime: new Map([["MY_AGENT", 600_000]]),
        taskLifetime: new Map([["MY_TASK", 600_000]]),
        out,
      });

      // Three .get() calls — one per scope-matched candidate. If the
      // Proxy didn't trigger an iteration error AND the call count is
      // exactly 3, no universe walk and no transient merge-Set
      // construction visiting noise IDs.
      expect(getCalls.sort()).toEqual(["GLOBAL", "MY", "TASK_MY"]);
      expect(out.size).toBe(3);
    });

    it("handles missing agent index entry without iteration", () => {
      // No agent budget at all under `agentSlug`. visitCandidates
      // must early-return on undefined; the Proxy is not even
      // touched for the agent branch.
      const indexes = __createBudgetCandidateIndexesForTesting();
      const globalBudget = makeReplayedBudget({ scope: "global" });
      addBudgetToIndex(indexes, "G", globalBudget);

      const { budgets, getCalls } = makeHostileBudgets([["G", globalBudget]]);
      const out: ComputeExpectedCrossingsArgs["out"] = new Map();
      computeExpectedCrossings({
        costEventLsn: 7,
        costEventOccurredAtMs: 1_700_000_000_000,
        agentSlug: "UNKNOWN_AGENT",
        taskId: undefined,
        budgets,
        globalBudgetIds: indexes.globalBudgetIds,
        agentBudgetIds: indexes.agentBudgetIds,
        taskBudgetIds: indexes.taskBudgetIds,
        globalLifetime: 600_000,
        agentLifetime: new Map(),
        taskLifetime: new Map(),
        out,
      });

      expect(getCalls).toEqual(["G"]);
      expect(out.size).toBe(1);
    });

    // Round-4 fix (PR #846 triangulation adversarial #1 MEDIUM): the
    // hostile-budgets Proxy above only protects `args.budgets`. A
    // future refactor that scans `agentBudgetIds.entries()` /
    // `taskBudgetIds.entries()` to find a matching subject would still
    // call `budgets.get()` only for matched candidates and pass the
    // test, while reintroducing O(events × subjects) behavior. These
    // tests additionally wrap the agent/task scope maps in proxies
    // that allow `.get(slug)` but throw on `.entries()`, `.values()`,
    // `.keys()`, `.forEach(...)`, and iteration — locking in the
    // direct-lookup invariant.
    describe("with hostile index maps", () => {
      function makeHostileSubjectIndex(
        underlying: Map<string, Set<string>>,
      ): ReadonlyMap<string, ReadonlySet<string>> {
        return new Proxy(underlying, {
          get(_target, prop) {
            if (prop === "get") return (key: string) => underlying.get(key);
            if (prop === "has") return (key: string) => underlying.has(key);
            if (prop === "size") return underlying.size;
            throw new Error(
              `computeExpectedCrossings must access scope index only via .get(subjectId); attempted .${String(prop)}`,
            );
          },
        }) as unknown as ReadonlyMap<string, ReadonlySet<string>>;
      }

      it("hostile agentBudgetIds: only .get(slug) reaches the underlying map", () => {
        const indexes = __createBudgetCandidateIndexesForTesting();
        const myAgentBudget = makeReplayedBudget({ scope: "agent", subjectId: "MY_AGENT" });
        addBudgetToIndex(indexes, "MY", myAgentBudget);
        for (let i = 0; i < 50; i += 1) {
          addBudgetToIndex(
            indexes,
            `OTHER_${i}`,
            makeReplayedBudget({ scope: "agent", subjectId: `OTHER_${i}` }),
          );
        }
        const allEntries: Array<[string, ReplayedBudgetShape]> = Array.from(
          indexes.agentBudgetIds.entries(),
        ).flatMap(([subjectId, ids]) =>
          Array.from(ids).map((id): [string, ReplayedBudgetShape] => [
            id,
            makeReplayedBudget({ scope: "agent", subjectId }),
          ]),
        );

        const { budgets } = makeHostileBudgets(allEntries);
        const out: ComputeExpectedCrossingsArgs["out"] = new Map();
        computeExpectedCrossings({
          costEventLsn: 11,
          costEventOccurredAtMs: 1_700_000_000_000,
          agentSlug: "MY_AGENT",
          taskId: undefined,
          budgets,
          globalBudgetIds: indexes.globalBudgetIds,
          agentBudgetIds: makeHostileSubjectIndex(indexes.agentBudgetIds),
          taskBudgetIds: makeHostileSubjectIndex(indexes.taskBudgetIds),
          globalLifetime: 600_000,
          agentLifetime: new Map([["MY_AGENT", 600_000]]),
          taskLifetime: new Map(),
          out,
        });
        expect(out.size).toBe(1);
        expect(out.has("MY|1|5000")).toBe(true);
      });

      it("hostile taskBudgetIds: only .get(taskId) reaches the underlying map", () => {
        const indexes = __createBudgetCandidateIndexesForTesting();
        addBudgetToIndex(
          indexes,
          "MY",
          makeReplayedBudget({ scope: "task", subjectId: "MY_TASK" }),
        );
        for (let i = 0; i < 50; i += 1) {
          addBudgetToIndex(
            indexes,
            `OTHER_${i}`,
            makeReplayedBudget({ scope: "task", subjectId: `OTHER_${i}` }),
          );
        }
        const allEntries: Array<[string, ReplayedBudgetShape]> = Array.from(
          indexes.taskBudgetIds.entries(),
        ).flatMap(([subjectId, ids]) =>
          Array.from(ids).map((id): [string, ReplayedBudgetShape] => [
            id,
            makeReplayedBudget({ scope: "task", subjectId }),
          ]),
        );

        const { budgets } = makeHostileBudgets(allEntries);
        const out: ComputeExpectedCrossingsArgs["out"] = new Map();
        computeExpectedCrossings({
          costEventLsn: 13,
          costEventOccurredAtMs: 1_700_000_000_000,
          agentSlug: "ANY",
          taskId: "MY_TASK",
          budgets,
          globalBudgetIds: indexes.globalBudgetIds,
          agentBudgetIds: makeHostileSubjectIndex(indexes.agentBudgetIds),
          taskBudgetIds: makeHostileSubjectIndex(indexes.taskBudgetIds),
          globalLifetime: 0,
          agentLifetime: new Map(),
          taskLifetime: new Map([["MY_TASK", 600_000]]),
          out,
        });
        expect(out.size).toBe(1);
        expect(out.has("MY|1|5000")).toBe(true);
      });
    });
  });

  // Round-4 fix (PR #846 triangulation perf+architecture 2-of-3 MEDIUM
  // / 3-of-3 LOW): the original lifecycle tests cover scope transitions
  // and the noise-budget perf path, but miss the same-scope subject
  // moves (agent A→B / task A→B), tombstone→live reactivation, and
  // tombstone-with-changed-scope. A regression that left stale subject
  // entries behind would be masked by `evalBudget`'s defensive
  // `isOracleApplicable` filter while degrading the hot path. These
  // tests pin `replaceBudgetInIndex` to the exact post-condition.
  describe("replaceBudgetInIndex (lifecycle invariant)", () => {
    it("agent → agent: same scope, different subject — moves the entry", () => {
      const indexes = __createBudgetCandidateIndexesForTesting();
      const a = makeReplayedBudget({ scope: "agent", subjectId: "agent-A" });
      const b = makeReplayedBudget({ scope: "agent", subjectId: "agent-B" });
      replaceBudgetInIndex(indexes, "B1", undefined, a);
      replaceBudgetInIndex(indexes, "B1", a, b);
      expect(indexes.agentBudgetIds.has("agent-A")).toBe(false);
      expect(indexes.agentBudgetIds.get("agent-B")?.has("B1")).toBe(true);
      expect(indexes.agentBudgetIds.size).toBe(1);
    });

    it("task → task: same scope, different subject — moves the entry", () => {
      const indexes = __createBudgetCandidateIndexesForTesting();
      const a = makeReplayedBudget({ scope: "task", subjectId: "task-A" });
      const b = makeReplayedBudget({ scope: "task", subjectId: "task-B" });
      replaceBudgetInIndex(indexes, "B2", undefined, a);
      replaceBudgetInIndex(indexes, "B2", a, b);
      expect(indexes.taskBudgetIds.has("task-A")).toBe(false);
      expect(indexes.taskBudgetIds.get("task-B")?.has("B2")).toBe(true);
      expect(indexes.taskBudgetIds.size).toBe(1);
    });

    it("live → tombstone → live: reactivation returns to the index cleanly", () => {
      const indexes = __createBudgetCandidateIndexesForTesting();
      const live = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
      const tombstoned = makeReplayedBudget({
        scope: "agent",
        subjectId: "primary",
        tombstoned: true,
      });
      const reactivated = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
      replaceBudgetInIndex(indexes, "B3", undefined, live);
      replaceBudgetInIndex(indexes, "B3", live, tombstoned);
      expect(indexes.agentBudgetIds.has("primary")).toBe(false);
      replaceBudgetInIndex(indexes, "B3", tombstoned, reactivated);
      expect(indexes.agentBudgetIds.get("primary")?.has("B3")).toBe(true);
    });

    it("live agent → tombstone global: removed from agent, never added to global", () => {
      const indexes = __createBudgetCandidateIndexesForTesting();
      const liveAgent = makeReplayedBudget({ scope: "agent", subjectId: "primary" });
      const tombstonedGlobal = makeReplayedBudget({ scope: "global", tombstoned: true });
      replaceBudgetInIndex(indexes, "B4", undefined, liveAgent);
      replaceBudgetInIndex(indexes, "B4", liveAgent, tombstonedGlobal);
      expect(indexes.agentBudgetIds.has("primary")).toBe(false);
      expect(indexes.globalBudgetIds.has("B4")).toBe(false);
      expect(indexes.taskBudgetIds.size).toBe(0);
    });

    it("scope move on a tombstoned record: remove-then-no-add (single index, no leak)", () => {
      const indexes = __createBudgetCandidateIndexesForTesting();
      const liveTask = makeReplayedBudget({ scope: "task", subjectId: "task-A" });
      const tombstonedTask = makeReplayedBudget({
        scope: "task",
        subjectId: "task-A",
        tombstoned: true,
      });
      const tombstonedAgent = makeReplayedBudget({
        scope: "agent",
        subjectId: "primary",
        tombstoned: true,
      });
      replaceBudgetInIndex(indexes, "B5", undefined, liveTask);
      replaceBudgetInIndex(indexes, "B5", liveTask, tombstonedTask);
      // A subsequent scope-move tombstone must not re-add anywhere
      // (the prior placement is already empty, but the helper still
      // needs to not panic on an undefined-subject lookup).
      replaceBudgetInIndex(indexes, "B5", tombstonedTask, tombstonedAgent);
      expect(indexes.globalBudgetIds.size).toBe(0);
      expect(indexes.agentBudgetIds.size).toBe(0);
      expect(indexes.taskBudgetIds.size).toBe(0);
    });
  });
});
