import {
  asAgentSlug,
  asBudgetId,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  type BudgetSetAuditPayload,
  type CostEventAuditPayload,
  lsnFromV1Number,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";
import { createCostLedger, parseIdempotencyKey, runReplayCheck } from "../src/cost-ledger/index.ts";
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
    const budgetSetLsnInt = Number(setResult.lsn.slice(3)); // "v1:N" → N
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
