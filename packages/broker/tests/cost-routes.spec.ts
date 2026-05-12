import {
  asAgentSlug,
  asApiToken,
  asBudgetId,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  type BudgetSetAuditPayload,
  type CostEventAuditPayload,
  costAuditPayloadToJsonValue,
} from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createEventLog, openDatabase, runMigrations } from "../src/event-log/index.ts";
import type { BrokerHandle, CommandIdempotencyStore, CostLedger } from "../src/index.ts";
import { createBroker, createCommandIdempotencyStore, createCostLedger } from "../src/index.ts";

const FIXED_TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const BUDGET_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ";
const RECEIPT_ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const TASK_ID = "01BRZ3NDEKTSV4RRFFQ69G5FA0";

function costEventJson(amountMicroUsd: number, opts: { taskId?: string } = {}): unknown {
  const payload: CostEventAuditPayload = {
    receiptId: asReceiptId(RECEIPT_ID),
    agentSlug: asAgentSlug("primary"),
    ...(opts.taskId !== undefined ? { taskId: asTaskId(opts.taskId) } : {}),
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    amountMicroUsd: asMicroUsd(amountMicroUsd),
    units: { inputTokens: 100, outputTokens: 50, cacheReadTokens: 0, cacheCreationTokens: 0 },
    occurredAt: new Date("2026-05-08T10:00:00.000Z"),
  };
  return costAuditPayloadToJsonValue("cost_event", payload);
}

function budgetSetJson(limit: number, thresholds: readonly number[] = [5_000, 10_000]): unknown {
  const payload: BudgetSetAuditPayload = {
    budgetId: asBudgetId(BUDGET_ID),
    scope: "global",
    limitMicroUsd: asMicroUsd(limit),
    thresholdsBps: thresholds,
    setBy: asSignerIdentity("operator@example.com"),
    setAt: new Date("2026-05-08T09:00:00.000Z"),
  };
  return costAuditPayloadToJsonValue("budget_set", payload);
}

interface Fixture {
  readonly broker: BrokerHandle;
  readonly ledger: CostLedger;
  readonly idempotency: CommandIdempotencyStore;
  readonly db: ReturnType<typeof openDatabase>;
}

async function buildFixture(): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const ledger = createCostLedger(db, eventLog);
  const idempotency = createCommandIdempotencyStore(db);
  const broker = await createBroker({
    port: 0,
    token: FIXED_TOKEN,
    cost: { ledger, idempotency, db },
  });
  return { broker, ledger, idempotency, db };
}

async function teardown(fix: Fixture | null): Promise<void> {
  if (fix === null) return;
  await fix.broker.stop();
  fix.db.close();
}

const baseHeaders = {
  Authorization: `Bearer ${FIXED_TOKEN}`,
  "Content-Type": "application/json",
};

describe("/api/v1/cost routes", () => {
  let fix: Fixture | null = null;
  beforeEach(async () => {
    fix = await buildFixture();
  });
  afterEach(async () => {
    await teardown(fix);
    fix = null;
  });

  it("POST /events appends and projects in one round-trip", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: {
        ...baseHeaders,
        "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}`,
      },
      body: JSON.stringify(costEventJson(2_500_000, { taskId: TASK_ID })),
    });
    expect(res.status).toBe(201);
    const body = (await res.json()) as {
      readonly lsn: string;
      readonly agentDayTotal: number;
      readonly taskTotal: number | null;
      readonly newCrossings: readonly unknown[];
    };
    expect(body.lsn).toBe("v1:1");
    expect(body.agentDayTotal).toBe(2_500_000);
    expect(body.taskTotal).toBe(2_500_000);
    expect(body.newCrossings).toEqual([]);
  });

  it("duplicate Idempotency-Key replays the response", async () => {
    if (fix === null) throw new Error("fixture missing");
    const key = `cmd_cost.event_${RECEIPT_ID}`;
    const headers = { ...baseHeaders, "Idempotency-Key": key };
    const body = JSON.stringify(costEventJson(1_000_000));
    const r1 = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers,
      body,
    });
    expect(r1.status).toBe(201);
    const r1Body = await r1.text();

    const r2 = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers,
      body,
    });
    expect(r2.status).toBe(201);
    expect(r2.headers.get("Idempotent-Replay")).toBe("true");
    const r2Body = await r2.text();
    expect(r2Body).toBe(r1Body);

    // Only one event was appended (replay didn't double-apply).
    const summary = await fetch(`${fix.broker.url}/api/v1/cost/summary`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    const summaryBody = (await summary.json()) as {
      readonly agentSpend: readonly { totalMicroUsd: number }[];
    };
    const agentSpend = summaryBody.agentSpend;
    expect(agentSpend.length).toBe(1);
    expect(agentSpend[0]?.totalMicroUsd).toBe(1_000_000);
  });

  it("missing Idempotency-Key on POST returns 400", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: baseHeaders,
      body: JSON.stringify(costEventJson(1_000_000)),
    });
    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toBe("idempotency_key_required");
  });

  it("wrong-command Idempotency-Key on POST returns 400", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: {
        ...baseHeaders,
        "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}`,
      },
      body: JSON.stringify(costEventJson(1_000_000)),
    });
    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toBe("idempotency_key_invalid");
  });

  it("POST /budgets + DELETE /budgets/:id round-trips through the ledger", async () => {
    if (fix === null) throw new Error("fixture missing");
    const setRes = await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: {
        ...baseHeaders,
        "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}`,
      },
      body: JSON.stringify(budgetSetJson(5_000_000)),
    });
    expect(setRes.status).toBe(201);

    const getRes = await fetch(`${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(getRes.status).toBe(200);
    const getBody = (await getRes.json()) as {
      readonly limitMicroUsd: number;
      readonly tombstoned: boolean;
    };
    expect(getBody.limitMicroUsd).toBe(5_000_000);
    expect(getBody.tombstoned).toBe(false);

    const delRes = await fetch(`${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`, {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${FIXED_TOKEN}`,
        "Idempotency-Key": `cmd_cost.budget.tombstone_${RECEIPT_ID}`,
        "X-Operator-Identity": "operator@example.com",
      },
    });
    expect(delRes.status).toBe(200);
    const delBody = (await delRes.json()) as { readonly tombstoned: boolean };
    expect(delBody.tombstoned).toBe(true);

    const post = await fetch(`${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    const postBody = (await post.json()) as {
      readonly tombstoned: boolean;
      readonly limitMicroUsd: number;
    };
    expect(postBody.tombstoned).toBe(true);
    expect(postBody.limitMicroUsd).toBe(0);
  });

  it("DELETE without X-Operator-Identity returns 400", async () => {
    if (fix === null) throw new Error("fixture missing");
    // Seed a budget so the DELETE has a real row to act on.
    await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: { ...baseHeaders, "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` },
      body: JSON.stringify(budgetSetJson(5_000_000)),
    });
    const res = await fetch(`${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`, {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${FIXED_TOKEN}`,
        "Idempotency-Key": `cmd_cost.budget.tombstone_${RECEIPT_ID}`,
      },
    });
    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toBe("operator_identity_required");
  });

  it("POST /budgets with limit=0 returns 400 (use DELETE)", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: { ...baseHeaders, "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` },
      body: JSON.stringify(budgetSetJson(0)),
    });
    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toBe("use_delete_to_tombstone");
  });

  it("GET /replay-check returns ok when projection is in sync", async () => {
    if (fix === null) throw new Error("fixture missing");
    await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: { ...baseHeaders, "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}` },
      body: JSON.stringify(costEventJson(1_500_000)),
    });
    const res = await fetch(`${fix.broker.url}/api/v1/cost/replay-check`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      readonly ok: boolean;
      readonly discrepancies: readonly unknown[];
    };
    expect(body.ok).toBe(true);
    expect(body.discrepancies).toEqual([]);
  });

  it("GET /summary returns the current aggregate state", async () => {
    if (fix === null) throw new Error("fixture missing");
    await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: { ...baseHeaders, "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` },
      body: JSON.stringify(budgetSetJson(5_000_000)),
    });
    await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: { ...baseHeaders, "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}` },
      body: JSON.stringify(costEventJson(2_500_000)),
    });
    const res = await fetch(`${fix.broker.url}/api/v1/cost/summary`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      readonly agentSpend: readonly { agentSlug: string; totalMicroUsd: number }[];
      readonly budgets: readonly { budgetId: string; limitMicroUsd: number }[];
      readonly thresholdCrossings: readonly { thresholdBps: number }[];
    };
    expect(body.agentSpend.length).toBe(1);
    expect(body.agentSpend[0]?.totalMicroUsd).toBe(2_500_000);
    expect(body.budgets[0]?.budgetId).toBe(BUDGET_ID);
    expect(body.thresholdCrossings.length).toBe(1);
    expect(body.thresholdCrossings[0]?.thresholdBps).toBe(5_000);
  });

  it("unknown /api/v1/cost/* path returns 404", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/no-such`, {
      headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
    });
    expect(res.status).toBe(404);
  });

  it("bearer required for cost routes", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(`${fix.broker.url}/api/v1/cost/summary`);
    expect(res.status).toBe(401);
  });
});

describe("cost routes when broker has no cost config", () => {
  it("falls through to 404 for /api/v1/cost/* paths", async () => {
    const broker = await createBroker({ port: 0, token: FIXED_TOKEN });
    try {
      const res = await fetch(`${broker.url}/api/v1/cost/summary`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(404);
    } finally {
      await broker.stop();
    }
  });
});
