// End-to-end test that exercises the cost ledger's threshold reactor
// through the gateway. This is what validates that PR A's storage path
// composes with PR B's gateway: each Gateway.complete() writes a
// cost_event, which the in-transaction reactor scans against active
// budgets, which emits cost.budget.threshold.crossed events.
//
// Per the §15.A architecture-proof contract:
//   sum(cost_events) == sum(cost_by_agent) == sum(cost_by_task)
// must hold after every commit. We assert this at the end of the run.

import {
  createCostLedger,
  createEventLog,
  openDatabase,
  runMigrations,
  runReplayCheck,
} from "@wuphf/broker/cost-ledger";
import {
  asAgentSlug,
  asBudgetId,
  asMicroUsd,
  asSignerIdentity,
  asTaskId,
  type BudgetSetAuditPayload,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  createGateway,
  createStubProvider,
  STUB_FIXED_COST_MICRO_USD,
  STUB_MODEL_FIXED_COST,
  type SupervisorContext,
} from "../src/index.ts";

describe("gateway → ledger → threshold reactor integration", () => {
  it("threshold crossings fire from gateway calls, replay-check passes after the run", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
    const gateway = createGateway({
      ledger,
      providers: [createStubProvider()],
      nowMs: () => clock.now,
      // High caps so this test isn't throttled by caps; we're measuring
      // threshold reactor behavior, not cap enforcement.
      config: {
        caps: {
          dailyMicroUsd: 10_000_000,
          wakeCapPerHour: 1_000,
          idleThresholdMs: 24 * 60 * 60 * 1000,
        },
      },
    });

    // Set a budget targeting one agent so we can see the threshold fire.
    const budget: BudgetSetAuditPayload = {
      budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
      scope: "agent",
      subjectId: "primary",
      limitMicroUsd: asMicroUsd(50_000), // $0.05 = 5 stub calls
      thresholdsBps: [5_000, 10_000], // 50% and 100%
      setBy: asSignerIdentity("operator@example.com"),
      setAt: new Date(clock.now),
    };
    ledger.appendBudgetSet(budget);

    const ctx: SupervisorContext = {
      agentSlug: asAgentSlug("primary"),
      taskId: asTaskId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    };

    // 6 calls × $0.01 = $0.06. Should fire 50% (after call 3) and 100%
    // (after call 5).
    let firstCrossingLsn: string | null = null;
    let secondCrossingLsn: string | null = null;
    for (let i = 0; i < 6; i += 1) {
      await gateway.complete(ctx, {
        model: STUB_MODEL_FIXED_COST,
        prompt: `call-${i}`,
        maxOutputTokens: 8,
      });
      const crossingsNow = ledger.listThresholdCrossings();
      if (firstCrossingLsn === null && crossingsNow.length === 1) {
        firstCrossingLsn = crossingsNow[0]?.crossedAtLsn ?? null;
        expect(crossingsNow[0]?.thresholdBps).toBe(5_000);
        // First crossing should fire after cumulative spend ≥ 25_000 μUSD.
        // 3 × 10_000 = 30_000, so this fires on call 3 (index 2).
        expect(i).toBe(2);
      }
      if (secondCrossingLsn === null && crossingsNow.length === 2) {
        secondCrossingLsn = crossingsNow[1]?.crossedAtLsn ?? null;
        expect(crossingsNow[1]?.thresholdBps).toBe(10_000);
        // Second crossing fires once cumulative ≥ 50_000 μUSD, i.e. call 5.
        expect(i).toBe(4);
      }
      clock.now += 1;
    }

    expect(firstCrossingLsn).not.toBeNull();
    expect(secondCrossingLsn).not.toBeNull();

    // §15.A sum invariant: replay-check must pass after a healthy run.
    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);
    expect(report.discrepancies).toEqual([]);

    // The event_log must contain: 1 budget_set + 6 cost_event + 2
    // threshold-crossed = 9 cost.* events.
    const counts = db
      .prepare<[], { readonly type: string; readonly n: number }>(
        "SELECT type, COUNT(*) AS n FROM event_log GROUP BY type ORDER BY type",
      )
      .all();
    const byType = new Map(counts.map((row) => [row.type, row.n]));
    expect(byType.get("cost.event")).toBe(6);
    expect(byType.get("cost.budget.set")).toBe(1);
    expect(byType.get("cost.budget.threshold.crossed")).toBe(2);

    db.close();
  });

  it("raising a budget mid-run re-arms thresholds (PR A composite-PK contract)", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
    const gateway = createGateway({
      ledger,
      providers: [createStubProvider()],
      nowMs: () => clock.now,
      config: {
        caps: {
          dailyMicroUsd: 10_000_000,
          wakeCapPerHour: 1_000,
          idleThresholdMs: 24 * 60 * 60 * 1000,
        },
      },
    });

    const budgetId = asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ");
    const setBy = asSignerIdentity("operator@example.com");
    const ctx: SupervisorContext = { agentSlug: asAgentSlug("primary") };

    // Budget 1: $0.02 (2 calls at 50% triggers after 1 call).
    ledger.appendBudgetSet({
      budgetId,
      scope: "agent",
      subjectId: "primary",
      limitMicroUsd: asMicroUsd(20_000),
      thresholdsBps: [5_000],
      setBy,
      setAt: new Date(clock.now),
    });

    await gateway.complete(ctx, {
      model: STUB_MODEL_FIXED_COST,
      prompt: "p1",
      maxOutputTokens: 8,
    });
    clock.now += 1;
    let crossings = ledger.listThresholdCrossings();
    expect(crossings.length).toBe(1);
    const firstBudgetSetLsn = crossings[0]?.budgetSetLsn;

    // Raise the budget to $0.10. New budget_set LSN is minted.
    ledger.appendBudgetSet({
      budgetId,
      scope: "agent",
      subjectId: "primary",
      limitMicroUsd: asMicroUsd(100_000),
      thresholdsBps: [5_000],
      setBy,
      setAt: new Date(clock.now),
    });

    // We need 5 more calls to cross 50% of $0.10 = $0.05 (cumulative $0.06).
    // After 4 more calls cumulative is $0.05; after 5 it's $0.06 > $0.05.
    for (let i = 0; i < 5; i += 1) {
      await gateway.complete(ctx, {
        model: STUB_MODEL_FIXED_COST,
        prompt: `re-arm-${i}`,
        maxOutputTokens: 8,
      });
      clock.now += 1;
    }
    crossings = ledger.listThresholdCrossings();
    // Two rows now: one under the first budget_set epoch, one under the
    // second. Same threshold (5_000), different budget_set_lsn — exactly
    // the PR A composite-PK contract.
    expect(crossings.length).toBe(2);
    expect(crossings[0]?.budgetSetLsn).toBe(firstBudgetSetLsn);
    expect(crossings[1]?.budgetSetLsn).not.toBe(firstBudgetSetLsn);
    expect(crossings[1]?.thresholdBps).toBe(5_000);

    // Replay-check still passes.
    const report = runReplayCheck(db);
    expect(report.ok).toBe(true);

    db.close();
  });
});
