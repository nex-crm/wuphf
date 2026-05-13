import { request as httpRequest } from "node:http";

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
  costAuditPayloadFromJsonValue,
  costAuditPayloadToJsonValue,
} from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { CostLedger } from "../src/cost-ledger/index.ts";
import {
  createCostLedger,
  DEFAULT_COMMAND_IDEMPOTENCY_TTL_MS,
  parseIdempotencyKey,
} from "../src/cost-ledger/index.ts";
import { createEventLog, openDatabase, runMigrations } from "../src/event-log/index.ts";
import type { BrokerHandle, BrokerLogger } from "../src/index.ts";
import { createBroker } from "../src/index.ts";

const FIXED_TOKEN = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const OPERATOR_TOKEN = asApiToken("operator-token-with-enough-entropy-AAAA");
const OPERATOR_IDENTITY = "operator@example.com";
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
    setBy: asSignerIdentity(OPERATOR_IDENTITY),
    setAt: new Date("2026-05-08T09:00:00.000Z"),
  };
  return costAuditPayloadToJsonValue("budget_set", payload);
}

interface Fixture {
  readonly broker: BrokerHandle;
  readonly ledger: CostLedger;
  readonly db: ReturnType<typeof openDatabase>;
}

async function buildFixture(): Promise<Fixture> {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const ledger = createCostLedger(db, eventLog);
  const broker = await createBroker({
    port: 0,
    token: FIXED_TOKEN,
    cost: { ledger, db, operatorToken: OPERATOR_TOKEN },
  });
  return { broker, ledger, db };
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

function mutationHeaders(extra: Record<string, string> = {}): Record<string, string> {
  return {
    ...baseHeaders,
    "X-Operator-Capability": OPERATOR_TOKEN,
    "X-Operator-Identity": OPERATOR_IDENTITY,
    ...extra,
  };
}

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
      headers: mutationHeaders({
        "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}`,
      }),
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
    const headers = mutationHeaders({ "Idempotency-Key": key });
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
      headers: mutationHeaders(),
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
      headers: mutationHeaders({
        "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}`,
      }),
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
      headers: mutationHeaders({
        "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}`,
      }),
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
      headers: mutationHeaders({
        "Idempotency-Key": `cmd_cost.budget.tombstone_${RECEIPT_ID}`,
      }),
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
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` }),
      body: JSON.stringify(budgetSetJson(5_000_000)),
    });
    const res = await fetch(`${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`, {
      method: "DELETE",
      headers: {
        Authorization: `Bearer ${FIXED_TOKEN}`,
        "X-Operator-Capability": OPERATOR_TOKEN,
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
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` }),
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
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}` }),
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

  it("POST /idempotency/prune deletes expired replay rows only", async () => {
    if (fix === null) throw new Error("fixture missing");
    const key = `cmd_cost.event_${RECEIPT_ID}`;
    const append = await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: mutationHeaders({ "Idempotency-Key": key }),
      body: JSON.stringify(costEventJson(1_000_000)),
    });
    expect(append.status).toBe(201);
    fix.db
      .prepare<[number, string]>(
        "UPDATE command_idempotency SET created_at_ms = ? WHERE idempotency_key = ?",
      )
      .run(1, key);

    const res = await fetch(`${fix.broker.url}/api/v1/cost/idempotency/prune`, {
      method: "POST",
      headers: mutationHeaders(),
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      readonly pruned: number;
      readonly olderThanMs: number;
      readonly cutoffMs: number;
    };
    expect(body.pruned).toBe(1);
    expect(body.olderThanMs).toBe(DEFAULT_COMMAND_IDEMPOTENCY_TTL_MS);
    expect(Number.isSafeInteger(body.cutoffMs)).toBe(true);

    const idempotencyCount = fix.db
      .prepare<[], { readonly n: number }>("SELECT COUNT(*) AS n FROM command_idempotency")
      .get();
    expect(idempotencyCount?.n).toBe(0);
    const eventCount = fix.db
      .prepare<[], { readonly n: number }>(
        "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
      )
      .get();
    expect(eventCount?.n).toBe(1);
  });

  it("POST /idempotency/prune rejects invalid olderThanMs", async () => {
    if (fix === null) throw new Error("fixture missing");
    const res = await fetch(
      `${fix.broker.url}/api/v1/cost/idempotency/prune?olderThanMs=not-a-number`,
      {
        method: "POST",
        headers: mutationHeaders(),
      },
    );
    expect(res.status).toBe(400);
    const body = (await res.json()) as { readonly error: string };
    expect(body.error).toBe("invalid_older_than_ms");
  });

  it("GET /summary returns the current aggregate state", async () => {
    if (fix === null) throw new Error("fixture missing");
    await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` }),
      body: JSON.stringify(budgetSetJson(5_000_000)),
    });
    await fetch(`${fix.broker.url}/api/v1/cost/events`, {
      method: "POST",
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}` }),
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
    const res = await fetch(`${fix.broker.url}/api/v1/cost/idempotency/prune`, {
      method: "POST",
      headers: {
        "X-Operator-Capability": OPERATOR_TOKEN,
        "X-Operator-Identity": OPERATOR_IDENTITY,
      },
    });
    expect(res.status).toBe(401);
  });

  it("operator capability is required for mutation routes", async () => {
    if (fix === null) throw new Error("fixture missing");
    const identityOnlyHeaders = {
      ...baseHeaders,
      "X-Operator-Identity": OPERATOR_IDENTITY,
    };
    const cases = [
      {
        url: `${fix.broker.url}/api/v1/cost/events`,
        init: {
          method: "POST",
          headers: {
            ...identityOnlyHeaders,
            "Idempotency-Key": `cmd_cost.event_${RECEIPT_ID}`,
          },
          body: JSON.stringify(costEventJson(1_000_000)),
        },
      },
      {
        url: `${fix.broker.url}/api/v1/cost/budgets`,
        init: {
          method: "POST",
          headers: {
            ...identityOnlyHeaders,
            "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}`,
          },
          body: JSON.stringify(budgetSetJson(5_000_000)),
        },
      },
      {
        url: `${fix.broker.url}/api/v1/cost/budgets/${BUDGET_ID}`,
        init: {
          method: "DELETE",
          headers: {
            ...identityOnlyHeaders,
            "Idempotency-Key": `cmd_cost.budget.tombstone_${RECEIPT_ID}`,
          },
        },
      },
      {
        url: `${fix.broker.url}/api/v1/cost/idempotency/prune`,
        init: {
          method: "POST",
          headers: identityOnlyHeaders,
        },
      },
    ] as const;

    for (const c of cases) {
      const res = await fetch(c.url, c.init);
      expect(res.status).toBe(403);
      const body = (await res.json()) as { readonly error: string };
      expect(body.error).toBe("operator_capability_required");
    }
  });

  it("POST /idempotency/prune inherits the loopback gate", async () => {
    if (fix === null) throw new Error("fixture missing");
    const url = new URL(fix.broker.url);
    const status = await new Promise<number>((resolveFn, rejectFn) => {
      const req = httpRequest(
        {
          host: url.hostname,
          port: Number(url.port),
          path: "/api/v1/cost/idempotency/prune",
          method: "POST",
          headers: {
            Authorization: `Bearer ${FIXED_TOKEN}`,
            "X-Operator-Capability": OPERATOR_TOKEN,
            "X-Operator-Identity": OPERATOR_IDENTITY,
            Host: "evil.example.com",
          },
        },
        (res) => {
          resolveFn(res.statusCode ?? 0);
          res.resume();
        },
      );
      req.setTimeout(2_000, () => req.destroy(new Error("loopback-gate timeout")));
      req.on("error", rejectFn);
      req.end();
    });
    expect(status).toBe(403);
  });

  it("POST /budgets overwrites caller-supplied setBy and setAt", async () => {
    if (fix === null) throw new Error("fixture missing");
    const forgedBody = {
      budgetId: BUDGET_ID,
      scope: "global",
      limitMicroUsd: 5_000_000,
      thresholdsBps: [5_000, 10_000],
      setBy: "attacker@example.com",
      setAt: "2000-01-01T00:00:00.000Z",
    };

    const res = await fetch(`${fix.broker.url}/api/v1/cost/budgets`, {
      method: "POST",
      headers: mutationHeaders({ "Idempotency-Key": `cmd_cost.budget.set_${RECEIPT_ID}` }),
      body: JSON.stringify(forgedBody),
    });
    expect(res.status).toBe(201);

    const row = fix.db
      .prepare<[], { readonly payload: Buffer }>(
        "SELECT payload FROM event_log WHERE type = 'cost.budget.set' ORDER BY lsn DESC LIMIT 1",
      )
      .get();
    if (row === undefined) throw new Error("missing budget_set event");
    const payload = costAuditPayloadFromJsonValue(
      "budget_set",
      JSON.parse(row.payload.toString("utf8")) as unknown,
    ) as BudgetSetAuditPayload;
    expect(payload.setBy).toBe(OPERATOR_IDENTITY);
    expect(payload.setAt.toISOString()).not.toBe(forgedBody.setAt);
  });
});

describe("cost idempotency startup prune", () => {
  it("runs a best-effort TTL prune when cost routes are mounted", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const key = parseIdempotencyKey(`cmd_cost.event_${RECEIPT_ID}`, "cost.event");
    expect(key.ok).toBe(true);
    if (!key.ok) {
      db.close();
      return;
    }
    ledger.appendCostEventIdempotent({
      payload: costAuditPayloadFromJsonValue(
        "cost_event",
        costEventJson(1_000_000),
      ) as CostEventAuditPayload,
      idempotency: key.key,
      nowMs: 1,
      render: (applied) => ({
        statusCode: 201,
        payload: Buffer.from(JSON.stringify({ lsn: applied.lsn }), "utf8"),
      }),
    });

    const broker = await createBroker({
      port: 0,
      token: FIXED_TOKEN,
      cost: { ledger, db, operatorToken: OPERATOR_TOKEN },
    });
    try {
      const idempotencyCount = db
        .prepare<[], { readonly n: number }>("SELECT COUNT(*) AS n FROM command_idempotency")
        .get();
      expect(idempotencyCount?.n).toBe(0);
      const eventCount = db
        .prepare<[], { readonly n: number }>(
          "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
        )
        .get();
      expect(eventCount?.n).toBe(1);
    } finally {
      await broker.stop();
      db.close();
    }
  });

  it("still starts when the startup TTL prune fails", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const failingLedger: CostLedger = {
      ...ledger,
      pruneIdempotencyOlderThan: () => {
        throw new Error("simulated prune failure");
      },
    };
    const warnings: string[] = [];
    const logger: BrokerLogger = {
      info: () => undefined,
      warn: (event) => {
        warnings.push(event);
      },
      error: () => undefined,
    };
    const broker = await createBroker({
      port: 0,
      token: FIXED_TOKEN,
      cost: { ledger: failingLedger, db, operatorToken: OPERATOR_TOKEN },
      logger,
    });
    try {
      const res = await fetch(`${broker.url}/api/health`, {
        headers: { Authorization: `Bearer ${FIXED_TOKEN}` },
      });
      expect(res.status).toBe(200);
      expect(warnings).toContain("cost_idempotency_prune_failed");
    } finally {
      await broker.stop();
      db.close();
    }
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
