import { createCostLedger, createEventLog, openDatabase, runMigrations } from "@wuphf/broker/cost-ledger";
import { asAgentSlug, asProviderKind, asReceiptId, asTaskId } from "@wuphf/protocol";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  CapExceededError,
  CircuitBreakerOpenError,
  type CostEstimator,
  createGateway,
  createStubProvider,
  type Gateway,
  IdleModeError,
  type Provider,
  type ProviderRequest,
  STUB_FIXED_COST_MICRO_USD,
  STUB_MODEL_ERROR,
  STUB_MODEL_FIXED_COST,
  type SupervisorContext,
} from "../src/index.ts";

interface Clock {
  now: number;
}

function setup(opts: { dailyMicroUsd?: number; wakeCapPerHour?: number } = {}): {
  gateway: Gateway;
  clock: Clock;
  ledger: ReturnType<typeof createCostLedger>;
  db: ReturnType<typeof openDatabase>;
} {
  const db = openDatabase({ path: ":memory:" });
  runMigrations(db);
  const eventLog = createEventLog(db);
  const ledger = createCostLedger(db, eventLog);
  const clock: Clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
  const gateway = createGateway({
    ledger,
    providers: [createStubProvider()],
    nowMs: () => clock.now,
    config: {
      caps: {
        ...(opts.dailyMicroUsd !== undefined ? { dailyMicroUsd: opts.dailyMicroUsd } : {}),
        ...(opts.wakeCapPerHour !== undefined ? { wakeCapPerHour: opts.wakeCapPerHour } : {}),
      },
    },
  });
  return { gateway, clock, ledger, db };
}

const CTX: SupervisorContext = {
  agentSlug: asAgentSlug("primary"),
  taskId: asTaskId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
  receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
};

const REQ: ProviderRequest = {
  model: STUB_MODEL_FIXED_COST,
  prompt: "hello",
  maxOutputTokens: 64,
};

describe("gateway happy path", () => {
  let fix: ReturnType<typeof setup>;
  beforeEach(() => {
    fix = setup();
  });
  afterEach(() => {
    fix.db.close();
  });

  it("writes a cost_event before returning, and the LSN matches event_log", async () => {
    const result = await fix.gateway.complete(CTX, REQ);
    expect(result.text).toBe("ack");
    expect(result.costMicroUsd as number).toBe(STUB_FIXED_COST_MICRO_USD);
    expect(result.dedupeReplay).toBe(false);
    expect(result.costEventLsn).toBe("v1:1");

    // The §15.A invariant: event_log holds exactly one cost.event row at
    // the LSN we returned.
    const row = fix.db
      .prepare<[number], { readonly lsn: number; readonly type: string }>(
        "SELECT lsn, type FROM event_log WHERE lsn = ?",
      )
      .get(1);
    expect(row?.type).toBe("cost.event");
    expect(row?.lsn).toBe(1);
  });

  it("agent + task projections reflect the call atomically", async () => {
    await fix.gateway.complete(CTX, REQ);
    const agentRow = fix.ledger.getAgentSpend("primary", "2026-05-12");
    expect(agentRow?.totalMicroUsd as number).toBe(STUB_FIXED_COST_MICRO_USD);
    const taskRow = fix.ledger.getTaskSpend("01BRZ3NDEKTSV4RRFFQ69G5FA0");
    expect(taskRow?.totalMicroUsd as number).toBe(STUB_FIXED_COST_MICRO_USD);
  });
});

describe("dedupe", () => {
  it("identical payload within 60s returns the cached result without a new ledger row", async () => {
    const fix = setup();
    try {
      const r1 = await fix.gateway.complete(CTX, REQ);
      expect(r1.dedupeReplay).toBe(false);

      fix.clock.now += 30_000; // 30s later

      const r2 = await fix.gateway.complete(CTX, REQ);
      expect(r2.dedupeReplay).toBe(true);
      expect(r2.costEventLsn).toBe(r1.costEventLsn);
      expect(r2.costMicroUsd).toBe(r1.costMicroUsd);

      const eventCount = fix.db
        .prepare<[], { readonly n: number }>(
          "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
        )
        .get();
      expect(eventCount?.n).toBe(1);
    } finally {
      fix.db.close();
    }
  });

  it("after the 60s window expires, the same payload triggers a fresh ledger write", async () => {
    const fix = setup();
    try {
      const r1 = await fix.gateway.complete(CTX, REQ);
      fix.clock.now += 61_000; // past the dedupe window
      const r2 = await fix.gateway.complete(CTX, REQ);
      expect(r2.dedupeReplay).toBe(false);
      expect(r2.costEventLsn).not.toBe(r1.costEventLsn);
    } finally {
      fix.db.close();
    }
  });

  it("dedupe does NOT replay across agents — context is part of the key (B3)", async () => {
    const fix = setup();
    try {
      const ctxA: SupervisorContext = { agentSlug: asAgentSlug("agent_a") };
      const ctxB: SupervisorContext = { agentSlug: asAgentSlug("agent_b") };
      // Same prompt, different agents. Each must produce a fresh
      // cost_event LSN; the second call must NOT replay the first.
      const r1 = await fix.gateway.complete(ctxA, REQ);
      const r2 = await fix.gateway.complete(ctxB, REQ);
      expect(r1.dedupeReplay).toBe(false);
      expect(r2.dedupeReplay).toBe(false);
      expect(r2.costEventLsn).not.toBe(r1.costEventLsn);
      // Both agents have a row in cost_by_agent.
      expect(fix.ledger.getAgentSpend("agent_a", "2026-05-12")?.totalMicroUsd as number).toBe(
        STUB_FIXED_COST_MICRO_USD,
      );
      expect(fix.ledger.getAgentSpend("agent_b", "2026-05-12")?.totalMicroUsd as number).toBe(
        STUB_FIXED_COST_MICRO_USD,
      );
    } finally {
      fix.db.close();
    }
  });

  it("dedupe does NOT replay across tasks — taskId is part of the key (B3)", async () => {
    const fix = setup();
    try {
      const ctxTask1: SupervisorContext = {
        agentSlug: asAgentSlug("primary"),
        taskId: asTaskId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
      };
      const ctxTask2: SupervisorContext = {
        agentSlug: asAgentSlug("primary"),
        taskId: asTaskId("01BRZ3NDEKTSV4RRFFQ69G5FA1"),
      };
      const r1 = await fix.gateway.complete(ctxTask1, REQ);
      const r2 = await fix.gateway.complete(ctxTask2, REQ);
      expect(r2.dedupeReplay).toBe(false);
      expect(r2.costEventLsn).not.toBe(r1.costEventLsn);
    } finally {
      fix.db.close();
    }
  });
});

describe("daily cap", () => {
  it("blocks once today's office spend reaches the cap", async () => {
    // Cap of one stub call. After call #1 the projection is at the cap,
    // and call #2 must reject before reaching the provider.
    const fix = setup({ dailyMicroUsd: STUB_FIXED_COST_MICRO_USD });
    try {
      await fix.gateway.complete(CTX, REQ);
      // Bypass dedupe by changing the prompt.
      const second: ProviderRequest = { ...REQ, prompt: "different" };
      await expect(fix.gateway.complete(CTX, second)).rejects.toBeInstanceOf(CapExceededError);
    } finally {
      fix.db.close();
    }
  });

  it("counts spend across all agents (per-office cap, not per-agent)", async () => {
    const fix = setup({ dailyMicroUsd: STUB_FIXED_COST_MICRO_USD });
    try {
      const ctxA: SupervisorContext = { agentSlug: asAgentSlug("agent_a") };
      const ctxB: SupervisorContext = { agentSlug: asAgentSlug("agent_b") };
      await fix.gateway.complete(ctxA, { ...REQ, prompt: "a" });
      await expect(fix.gateway.complete(ctxB, { ...REQ, prompt: "b" })).rejects.toBeInstanceOf(
        CapExceededError,
      );
    } finally {
      fix.db.close();
    }
  });
});

describe("wake cap", () => {
  it("blocks the 13th wake within an hour for the same agent", async () => {
    const fix = setup({ wakeCapPerHour: 3 });
    try {
      // 3 successful wakes fit; the 4th is rejected.
      for (let i = 0; i < 3; i += 1) {
        await fix.gateway.complete(CTX, { ...REQ, prompt: `unique-${i}` });
      }
      await expect(
        fix.gateway.complete(CTX, { ...REQ, prompt: "overflow" }),
      ).rejects.toBeInstanceOf(CapExceededError);
    } finally {
      fix.db.close();
    }
  });

  it("wake-cap window slides — old wakes drop out after wakeWindowMs", async () => {
    const fix = setup({ wakeCapPerHour: 2 });
    try {
      await fix.gateway.complete(CTX, { ...REQ, prompt: "a" });
      await fix.gateway.complete(CTX, { ...REQ, prompt: "b" });
      // 3rd within an hour: rejected
      await expect(fix.gateway.complete(CTX, { ...REQ, prompt: "c" })).rejects.toBeInstanceOf(
        CapExceededError,
      );
      // Advance past the wake window; old wakes drop out. Also note
      // human activity so we don't trip the idle gate (which has the
      // same 60min+ horizon as our advance).
      fix.clock.now += 60 * 60 * 1000 + 1;
      fix.gateway.noteHumanActivity();
      const r = await fix.gateway.complete(CTX, { ...REQ, prompt: "c" });
      expect(r.dedupeReplay).toBe(false);
    } finally {
      fix.db.close();
    }
  });

  it("wake cap is per-agent (one agent's saturation doesn't block another)", async () => {
    const fix = setup({ wakeCapPerHour: 1 });
    try {
      const ctxA: SupervisorContext = { agentSlug: asAgentSlug("agent_a") };
      const ctxB: SupervisorContext = { agentSlug: asAgentSlug("agent_b") };
      await fix.gateway.complete(ctxA, { ...REQ, prompt: "a" });
      await expect(fix.gateway.complete(ctxA, { ...REQ, prompt: "a2" })).rejects.toBeInstanceOf(
        CapExceededError,
      );
      // Different agent still works.
      const r = await fix.gateway.complete(ctxB, { ...REQ, prompt: "b" });
      expect(r.dedupeReplay).toBe(false);
    } finally {
      fix.db.close();
    }
  });
});

describe("circuit breaker", () => {
  it("post-provider failures also record breaker errors (H5)", async () => {
    // Custom provider whose .complete() succeeds (so the
    // pre-existing provider-error catch does NOT fire), but whose
    // estimator throws — exercising the new post-provider catch
    // around estimate + appendCostEvent. Two failures must open the
    // breaker.
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const clock: Clock = { now: new Date("2026-05-12T10:00:00.000Z").getTime() };
    const throwingEstimator: CostEstimator = {
      estimate() {
        throw new Error("estimator_unavailable");
      },
    };
    const failingProvider: Provider = {
      kind: asProviderKind("openai-compat"),
      models: ["test-throwing-model"],
      costEstimator: throwingEstimator,
      async complete() {
        return Promise.resolve({
          text: "ok",
          usage: {
            inputTokens: 1,
            outputTokens: 1,
            cacheReadTokens: 0,
            cacheCreationTokens: 0,
          },
        });
      },
    };
    const gateway = createGateway({
      ledger,
      providers: [failingProvider],
      nowMs: () => clock.now,
    });
    try {
      await expect(
        gateway.complete(CTX, {
          model: "test-throwing-model",
          prompt: "p1",
          maxOutputTokens: 8,
        }),
      ).rejects.toThrow(/estimator_unavailable/);
      await expect(
        gateway.complete(CTX, {
          model: "test-throwing-model",
          prompt: "p2",
          maxOutputTokens: 8,
        }),
      ).rejects.toThrow(/estimator_unavailable/);
      const inspection = gateway.inspect();
      const agent = inspection.perAgent.get("primary");
      expect(agent?.breaker.status).toBe("open");
    } finally {
      db.close();
    }
  });

  it("opens after 2 errors in the window and rejects further calls until cooldown", async () => {
    const fix = setup();
    try {
      const errReq: ProviderRequest = { ...REQ, model: STUB_MODEL_ERROR };
      // First error: breaker stays closed.
      await expect(fix.gateway.complete(CTX, errReq)).rejects.toBeTruthy();
      // Second error: breaker opens.
      await expect(fix.gateway.complete(CTX, { ...errReq, prompt: "err2" })).rejects.toBeTruthy();
      // Third call (even with success-able model): breaker open.
      await expect(
        fix.gateway.complete(CTX, { ...REQ, prompt: "after-open" }),
      ).rejects.toBeInstanceOf(CircuitBreakerOpenError);
    } finally {
      fix.db.close();
    }
  });

  it("cooldown elapses → breaker half-opens → next call closes it on success", async () => {
    const fix = setup();
    try {
      const errReq: ProviderRequest = { ...REQ, model: STUB_MODEL_ERROR };
      await expect(fix.gateway.complete(CTX, errReq)).rejects.toBeTruthy();
      await expect(fix.gateway.complete(CTX, { ...errReq, prompt: "err2" })).rejects.toBeTruthy();
      // Advance past cooldown. Same horizon as idle threshold (5min), so
      // note human activity so the idle gate doesn't reject the retry.
      fix.clock.now += 5 * 60 * 1000 + 1;
      fix.gateway.noteHumanActivity();
      const r = await fix.gateway.complete(CTX, { ...REQ, prompt: "after-cooldown" });
      expect(r.dedupeReplay).toBe(false);
    } finally {
      fix.db.close();
    }
  });
});

describe("idle mode", () => {
  it("rejects after 5min of inactivity", async () => {
    const fix = setup();
    try {
      // Start active.
      await fix.gateway.complete(CTX, REQ);
      // Advance past idle threshold. noteHumanActivity is the only reset.
      fix.clock.now += 5 * 60 * 1000 + 1;
      await expect(
        fix.gateway.complete(CTX, { ...REQ, prompt: "after-idle" }),
      ).rejects.toBeInstanceOf(IdleModeError);
    } finally {
      fix.db.close();
    }
  });

  it("noteHumanActivity resets the idle clock", async () => {
    const fix = setup();
    try {
      fix.clock.now += 5 * 60 * 1000 + 1;
      // Without note: rejected.
      await expect(fix.gateway.complete(CTX, { ...REQ, prompt: "p" })).rejects.toBeInstanceOf(
        IdleModeError,
      );
      fix.gateway.noteHumanActivity();
      const r = await fix.gateway.complete(CTX, { ...REQ, prompt: "p2" });
      expect(r.dedupeReplay).toBe(false);
    } finally {
      fix.db.close();
    }
  });
});

describe("inspect()", () => {
  it("returns wake counts and breaker state per agent", async () => {
    const fix = setup({ wakeCapPerHour: 5 });
    try {
      await fix.gateway.complete(CTX, { ...REQ, prompt: "a" });
      await fix.gateway.complete(CTX, { ...REQ, prompt: "b" });
      const inspection = fix.gateway.inspect();
      const agent = inspection.perAgent.get("primary");
      expect(agent?.recentWakeCount).toBe(2);
      expect(agent?.breaker.status).toBe("closed");
    } finally {
      fix.db.close();
    }
  });
});

// ─────────────────────────────────────────────────────────────────────
// Followups #827 + #828 + #824 — surgical gateway-side fixes
// ─────────────────────────────────────────────────────────────────────

describe("gateway construction-time validation (#828)", () => {
  it("rejects a Provider whose .kind is not a registered ProviderKind", () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const stub = createStubProvider();
    // Forge a fake kind via `as` cast — the defense is the construction
    // check, not the type system.
    const forged: Provider = { ...stub, kind: "not-a-real-kind" as never };
    try {
      expect(() =>
        createGateway({ ledger, providers: [forged], nowMs: () => Date.now() }),
      ).toThrow(/not a registered ProviderKind/);
    } finally {
      db.close();
    }
  });
});

describe("audit row records served model id (#827)", () => {
  it("uses ProviderResponse.model when the adapter supplies it", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const fixedKind = asProviderKind("openai-compat");
    const estimator: CostEstimator = {
      estimate: (_model, _usage) => 0 as never,
    };
    const provider: Provider = {
      kind: fixedKind,
      models: ["alias-model"],
      costEstimator: estimator,
      complete: async (_req) => ({
        text: "ok",
        usage: { inputTokens: 1, outputTokens: 1, cacheReadTokens: 0, cacheCreationTokens: 0 },
        // Adapter returns a pinned snapshot identifier; gateway should record THIS.
        model: "alias-model-2025-04-12-snapshot",
      }),
    };
    const gateway = createGateway({ ledger, providers: [provider], nowMs: () => Date.now() });
    try {
      await gateway.complete(
        { agentSlug: asAgentSlug("a") },
        { model: "alias-model", prompt: "p", maxOutputTokens: 8 },
      );
      const row = db
        .prepare<[], { readonly payload: Buffer }>(
          "SELECT payload FROM event_log WHERE type = 'cost.event' LIMIT 1",
        )
        .get();
      if (row === undefined) throw new Error("no cost.event row");
      const parsed = JSON.parse(row.payload.toString("utf8")) as { model: string };
      expect(parsed.model).toBe("alias-model-2025-04-12-snapshot");
    } finally {
      db.close();
    }
  });

  it("falls back to ProviderRequest.model when adapter does not supply one", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const gateway = createGateway({
      ledger,
      providers: [createStubProvider()],
      nowMs: () => Date.now(),
    });
    try {
      await gateway.complete(CTX, REQ);
      const row = db
        .prepare<[], { readonly payload: Buffer }>(
          "SELECT payload FROM event_log WHERE type = 'cost.event' LIMIT 1",
        )
        .get();
      if (row === undefined) throw new Error("no cost.event row");
      const parsed = JSON.parse(row.payload.toString("utf8")) as { model: string };
      expect(parsed.model).toBe(STUB_MODEL_FIXED_COST);
    } finally {
      db.close();
    }
  });
});

describe("gateway rejects runaway cost estimate before ledger append (#824)", () => {
  it("treats an over-cap estimator output as breaker-worthy and skips the ledger write", async () => {
    const db = openDatabase({ path: ":memory:" });
    runMigrations(db);
    const eventLog = createEventLog(db);
    const ledger = createCostLedger(db, eventLog);
    const kind = asProviderKind("openai-compat");
    // 200 million μUSD = $200 — exceeds the $100 per-event cap.
    const estimator: CostEstimator = {
      estimate: (_model, _usage) => 200_000_000 as never,
    };
    const provider: Provider = {
      kind,
      models: ["runaway-model"],
      costEstimator: estimator,
      complete: async () => ({
        text: "ok",
        usage: { inputTokens: 1, outputTokens: 1, cacheReadTokens: 0, cacheCreationTokens: 0 },
      }),
    };
    const gateway = createGateway({ ledger, providers: [provider], nowMs: () => Date.now() });
    try {
      await expect(
        gateway.complete(
          { agentSlug: asAgentSlug("a") },
          { model: "runaway-model", prompt: "p", maxOutputTokens: 8 },
        ),
      ).rejects.toThrow(/exceeds per-event cap/);
      // No cost_event written for the over-cap call.
      const count = db
        .prepare<[], { readonly n: number }>(
          "SELECT COUNT(*) AS n FROM event_log WHERE type = 'cost.event'",
        )
        .get();
      expect(count?.n).toBe(0);
    } finally {
      db.close();
    }
  });
});
