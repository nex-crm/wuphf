import { describe, expect, it } from "vitest";

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
  costAuditPayloadFromJsonValue,
  costAuditPayloadToBytes,
  costAuditPayloadToJsonValue,
  GENESIS_LSN,
  isBudgetId,
  isBudgetScope,
  isCostAuditEventKind,
  isMicroUsd,
  lsnFromV1Number,
  MAX_BUDGET_LIMIT_MICRO_USD,
  MAX_BUDGET_THRESHOLD_BPS,
  MAX_BUDGET_THRESHOLDS,
  MAX_COST_EVENT_AMOUNT_MICRO_USD,
  MAX_COST_MODEL_BYTES,
  validateBudgetSetAuditPayload,
  validateBudgetThresholdCrossedAuditPayload,
  validateCostAuditPayloadForKind,
  validateCostEventAuditPayload,
} from "../src/index.ts";

const ULID_A = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const ULID_B = "01ARZ3NDEKTSV4RRFFQ69G5FAW";
const ULID_C = "01ARZ3NDEKTSV4RRFFQ69G5FAX";

function validCostEvent(): CostEventAuditPayload {
  return {
    receiptId: asReceiptId(ULID_A),
    agentSlug: asAgentSlug("sam_agent"),
    taskId: asTaskId(ULID_B),
    providerKind: asProviderKind("openai"),
    model: "gpt-5.2",
    amountMicroUsd: asMicroUsd(42_500),
    units: {
      inputTokens: 1200,
      outputTokens: 345,
      cacheReadTokens: 50,
      cacheCreationTokens: 25,
    },
    occurredAt: new Date("2026-05-08T18:00:00.000Z"),
  };
}

function validBudgetSet(): BudgetSetAuditPayload {
  return {
    budgetId: asBudgetId(ULID_C),
    scope: "global",
    limitMicroUsd: asMicroUsd(1_000_000),
    thresholdsBps: [5000, 8000, 10000],
    setBy: asSignerIdentity("fd@example.com"),
    setAt: new Date("2026-05-08T18:00:00.000Z"),
  };
}

function validCrossing(): BudgetThresholdCrossedAuditPayload {
  return {
    budgetId: asBudgetId(ULID_C),
    budgetSetLsn: lsnFromV1Number(7),
    thresholdBps: 5000,
    observedMicroUsd: asMicroUsd(500_000),
    limitMicroUsd: asMicroUsd(1_000_000),
    crossedAtLsn: lsnFromV1Number(42),
    crossedAt: new Date("2026-05-08T18:30:00.000Z"),
  };
}

describe("BudgetId", () => {
  it("accepts a Crockford-base32 ULID", () => {
    expect(asBudgetId(ULID_A)).toBe(ULID_A);
    expect(isBudgetId(ULID_A)).toBe(true);
  });

  it("rejects non-ULID strings", () => {
    expect(() => asBudgetId("not-a-ulid")).toThrow(/not a valid ULID/);
    expect(() => asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FA")).toThrow(/not a valid ULID/);
    expect(() => asBudgetId(ULID_A.toLowerCase())).toThrow(/not a valid ULID/);
    expect(isBudgetId("not-a-ulid")).toBe(false);
    expect(isBudgetId(42)).toBe(false);
    expect(isBudgetId(undefined)).toBe(false);
  });
});

describe("MicroUsd", () => {
  it("accepts non-negative safe integers up to MAX_BUDGET_LIMIT_MICRO_USD", () => {
    expect(asMicroUsd(0) as number).toBe(0);
    expect(asMicroUsd(MAX_BUDGET_LIMIT_MICRO_USD) as number).toBe(MAX_BUDGET_LIMIT_MICRO_USD);
    expect(isMicroUsd(0)).toBe(true);
    expect(isMicroUsd(MAX_BUDGET_LIMIT_MICRO_USD)).toBe(true);
  });

  it("rejects negatives, floats, NaN and over-cap values", () => {
    expect(() => asMicroUsd(-1)).toThrow(/non-negative safe integer/);
    expect(() => asMicroUsd(1.5)).toThrow(/non-negative safe integer/);
    expect(() => asMicroUsd(Number.NaN)).toThrow(/non-negative safe integer/);
    expect(() => asMicroUsd(MAX_BUDGET_LIMIT_MICRO_USD + 1)).toThrow(/exceeds/);
    expect(isMicroUsd(-1)).toBe(false);
    expect(isMicroUsd(1.5)).toBe(false);
    expect(isMicroUsd("0")).toBe(false);
    expect(isMicroUsd(MAX_BUDGET_LIMIT_MICRO_USD + 1)).toBe(false);
  });
});

describe("BudgetScope", () => {
  it("accepts global, agent, task", () => {
    expect(isBudgetScope("global")).toBe(true);
    expect(isBudgetScope("agent")).toBe(true);
    expect(isBudgetScope("task")).toBe(true);
  });
  it("rejects everything else", () => {
    expect(isBudgetScope("workspace")).toBe(false);
    expect(isBudgetScope(undefined)).toBe(false);
    expect(isBudgetScope(0)).toBe(false);
  });
});

describe("isCostAuditEventKind", () => {
  it("accepts the three known kinds", () => {
    expect(isCostAuditEventKind("cost_event")).toBe(true);
    expect(isCostAuditEventKind("budget_set")).toBe(true);
    expect(isCostAuditEventKind("budget_threshold_crossed")).toBe(true);
  });
  it("rejects unknown kinds", () => {
    expect(isCostAuditEventKind("budget_unset")).toBe(false);
    expect(isCostAuditEventKind(null)).toBe(false);
    expect(isCostAuditEventKind(0)).toBe(false);
  });
});

describe("validateCostEventAuditPayload", () => {
  it("accepts a well-formed payload", () => {
    expect(validateCostEventAuditPayload(validCostEvent()).ok).toBe(true);
  });

  it("rejects non-object input", () => {
    const r = validateCostEventAuditPayload("not-an-object");
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors[0]?.message).toMatch(/must be an object/);
  });

  it("rejects unknown keys", () => {
    const r = validateCostEventAuditPayload({ ...validCostEvent(), extra: 1 });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors.some((e) => e.path === "/extra")).toBe(true);
  });

  it("rejects invalid receiptId / taskId / providerKind / agentSlug", () => {
    const r = validateCostEventAuditPayload({
      ...validCostEvent(),
      receiptId: "not-a-receipt",
      taskId: "not-a-task",
      agentSlug: "Invalid Slug!",
      providerKind: "claude-unknown",
    });
    expect(r.ok).toBe(false);
    if (!r.ok) {
      const paths = r.errors.map((e) => e.path);
      expect(paths).toContain("/receiptId");
      expect(paths).toContain("/taskId");
      expect(paths).toContain("/agentSlug");
      expect(paths).toContain("/providerKind");
    }
  });

  it("rejects empty / over-cap model", () => {
    const empty = validateCostEventAuditPayload({ ...validCostEvent(), model: "" });
    expect(empty.ok).toBe(false);

    const huge = "x".repeat(MAX_COST_MODEL_BYTES + 1);
    const over = validateCostEventAuditPayload({ ...validCostEvent(), model: huge });
    expect(over.ok).toBe(false);
    if (!over.ok) expect(over.errors[0]?.message).toMatch(/UTF-8 bytes/);
  });

  it("counts model length in UTF-8 bytes, not code units", () => {
    // "💸" is 4 UTF-8 bytes per codepoint
    const justUnder = "💸".repeat(Math.floor(MAX_COST_MODEL_BYTES / 4));
    expect(validateCostEventAuditPayload({ ...validCostEvent(), model: justUnder }).ok).toBe(true);
    const justOver = "💸".repeat(Math.floor(MAX_COST_MODEL_BYTES / 4) + 1);
    expect(validateCostEventAuditPayload({ ...validCostEvent(), model: justOver }).ok).toBe(false);
  });

  it("rejects over-cap amountMicroUsd, negatives, and floats", () => {
    const over = validateCostEventAuditPayload({
      ...validCostEvent(),
      amountMicroUsd: MAX_COST_EVENT_AMOUNT_MICRO_USD + 1,
    });
    expect(over.ok).toBe(false);
    if (!over.ok) expect(over.errors[0]?.message).toMatch(/at most/);

    const neg = validateCostEventAuditPayload({ ...validCostEvent(), amountMicroUsd: -1 });
    expect(neg.ok).toBe(false);

    const frac = validateCostEventAuditPayload({ ...validCostEvent(), amountMicroUsd: 1.5 });
    expect(frac.ok).toBe(false);
  });

  it("rejects malformed units", () => {
    const notObj = validateCostEventAuditPayload({ ...validCostEvent(), units: "x" });
    expect(notObj.ok).toBe(false);

    const wrongShape = validateCostEventAuditPayload({
      ...validCostEvent(),
      units: { inputTokens: -1, outputTokens: 1.5, cacheReadTokens: 0, cacheCreationTokens: 0 },
    });
    expect(wrongShape.ok).toBe(false);
    if (!wrongShape.ok) {
      const paths = wrongShape.errors.map((e) => e.path);
      expect(paths).toContain("/units/inputTokens");
      expect(paths).toContain("/units/outputTokens");
    }

    const unknownKey = validateCostEventAuditPayload({
      ...validCostEvent(),
      units: { ...validCostEvent().units, extra: 1 },
    });
    expect(unknownKey.ok).toBe(false);
  });

  it("rejects non-Date occurredAt", () => {
    const r = validateCostEventAuditPayload({ ...validCostEvent(), occurredAt: "2026-05-08" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors.some((e) => e.path === "/occurredAt")).toBe(true);
  });

  it("allows omitting receiptId and taskId", () => {
    const { receiptId: _r, taskId: _t, ...rest } = validCostEvent();
    expect(validateCostEventAuditPayload(rest).ok).toBe(true);
  });
});

describe("validateBudgetSetAuditPayload", () => {
  it("accepts global / agent / task scopes with matching subjectId", () => {
    expect(validateBudgetSetAuditPayload(validBudgetSet()).ok).toBe(true);

    const agentBudget = {
      ...validBudgetSet(),
      scope: "agent" as const,
      subjectId: "sam_agent",
    };
    expect(validateBudgetSetAuditPayload(agentBudget).ok).toBe(true);

    const taskBudget = { ...validBudgetSet(), scope: "task" as const, subjectId: ULID_B };
    expect(validateBudgetSetAuditPayload(taskBudget).ok).toBe(true);
  });

  it("rejects non-object input", () => {
    expect(validateBudgetSetAuditPayload(42).ok).toBe(false);
  });

  it("rejects unknown keys", () => {
    const r = validateBudgetSetAuditPayload({ ...validBudgetSet(), unknown: 1 });
    expect(r.ok).toBe(false);
  });

  it("rejects bad budgetId / setBy / setAt", () => {
    const r = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      budgetId: "not-a-ulid",
      setBy: "",
      setAt: "2026-05-08",
    });
    expect(r.ok).toBe(false);
    if (!r.ok) {
      const paths = r.errors.map((e) => e.path);
      expect(paths).toContain("/budgetId");
      expect(paths).toContain("/setBy");
      expect(paths).toContain("/setAt");
    }
  });

  it("rejects invalid scope", () => {
    const r = validateBudgetSetAuditPayload({ ...validBudgetSet(), scope: "workspace" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors.some((e) => e.path === "/scope")).toBe(true);
  });

  it("rejects global scope with subjectId present", () => {
    const r = validateBudgetSetAuditPayload({ ...validBudgetSet(), subjectId: "sam_agent" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.errors.some((e) => e.path === "/subjectId")).toBe(true);
  });

  it("rejects agent scope without a valid AgentSlug subjectId", () => {
    const missing = { ...validBudgetSet(), scope: "agent" as const };
    expect(validateBudgetSetAuditPayload(missing).ok).toBe(false);

    const bad = { ...validBudgetSet(), scope: "agent" as const, subjectId: "Invalid!" };
    expect(validateBudgetSetAuditPayload(bad).ok).toBe(false);
  });

  it("rejects task scope without a valid TaskId subjectId", () => {
    const bad = { ...validBudgetSet(), scope: "task" as const, subjectId: "not-a-task" };
    expect(validateBudgetSetAuditPayload(bad).ok).toBe(false);
  });

  it("rejects bad limitMicroUsd", () => {
    const over = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      limitMicroUsd: MAX_BUDGET_LIMIT_MICRO_USD + 1,
    });
    expect(over.ok).toBe(false);
    const neg = validateBudgetSetAuditPayload({ ...validBudgetSet(), limitMicroUsd: -1 });
    expect(neg.ok).toBe(false);
  });

  it("rejects malformed thresholdsBps", () => {
    expect(
      validateBudgetSetAuditPayload({ ...validBudgetSet(), thresholdsBps: "not-an-array" }).ok,
    ).toBe(false);
    expect(validateBudgetSetAuditPayload({ ...validBudgetSet(), thresholdsBps: [] }).ok).toBe(
      false,
    );
    expect(
      validateBudgetSetAuditPayload({
        ...validBudgetSet(),
        thresholdsBps: new Array(MAX_BUDGET_THRESHOLDS + 1).fill(0).map((_, i) => 100 + i),
      }).ok,
    ).toBe(false);

    const outOfRange = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      thresholdsBps: [0, 5000],
    });
    expect(outOfRange.ok).toBe(false);

    const tooBig = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      thresholdsBps: [MAX_BUDGET_THRESHOLD_BPS + 1],
    });
    expect(tooBig.ok).toBe(false);

    const notAscending = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      thresholdsBps: [8000, 5000],
    });
    expect(notAscending.ok).toBe(false);

    const dupes = validateBudgetSetAuditPayload({
      ...validBudgetSet(),
      thresholdsBps: [5000, 5000],
    });
    expect(dupes.ok).toBe(false);
  });
});

describe("validateBudgetThresholdCrossedAuditPayload", () => {
  it("accepts a well-formed crossing", () => {
    expect(validateBudgetThresholdCrossedAuditPayload(validCrossing()).ok).toBe(true);
  });

  it("rejects non-object input and unknown keys", () => {
    expect(validateBudgetThresholdCrossedAuditPayload(null).ok).toBe(false);
    const extra = validateBudgetThresholdCrossedAuditPayload({ ...validCrossing(), nope: 1 });
    expect(extra.ok).toBe(false);
  });

  it("rejects bad budgetId, thresholdBps, observed/limit MicroUsd, crossedAt", () => {
    const r = validateBudgetThresholdCrossedAuditPayload({
      ...validCrossing(),
      budgetId: "x",
      thresholdBps: 0,
      observedMicroUsd: -1,
      limitMicroUsd: 1.5,
      crossedAt: "2026-05-08",
    });
    expect(r.ok).toBe(false);
    if (!r.ok) {
      const paths = r.errors.map((e) => e.path);
      expect(paths).toContain("/budgetId");
      expect(paths).toContain("/thresholdBps");
      expect(paths).toContain("/observedMicroUsd");
      expect(paths).toContain("/limitMicroUsd");
      expect(paths).toContain("/crossedAt");
    }
  });

  it("rejects non-string and invalid LSN fields", () => {
    const notString = validateBudgetThresholdCrossedAuditPayload({
      ...validCrossing(),
      budgetSetLsn: 5,
    });
    expect(notString.ok).toBe(false);
    if (!notString.ok) expect(notString.errors.some((e) => e.path === "/budgetSetLsn")).toBe(true);

    const badShape = validateBudgetThresholdCrossedAuditPayload({
      ...validCrossing(),
      crossedAtLsn: "v0:nope",
    });
    expect(badShape.ok).toBe(false);
    if (!badShape.ok) expect(badShape.errors.some((e) => e.path === "/crossedAtLsn")).toBe(true);
  });
});

describe("validateCostAuditPayloadForKind", () => {
  it("dispatches to the right validator by kind", () => {
    expect(validateCostAuditPayloadForKind("cost_event", validCostEvent()).ok).toBe(true);
    expect(validateCostAuditPayloadForKind("budget_set", validBudgetSet()).ok).toBe(true);
    expect(validateCostAuditPayloadForKind("budget_threshold_crossed", validCrossing()).ok).toBe(
      true,
    );
  });

  it("throws on unknown kind", () => {
    expect(() =>
      validateCostAuditPayloadForKind("not_a_kind" as "cost_event", validCostEvent()),
    ).toThrow(/unknown CostAuditEventKind/);
  });
});

describe("JSON codecs round-trip every kind", () => {
  it("round-trips cost_event", () => {
    const payload = validCostEvent();
    const json = costAuditPayloadToJsonValue("cost_event", payload);
    const decoded = costAuditPayloadFromJsonValue("cost_event", json);
    expect(decoded.amountMicroUsd).toBe(payload.amountMicroUsd);
    expect(decoded.units).toEqual(payload.units);
    expect(decoded.occurredAt.toISOString()).toBe(payload.occurredAt.toISOString());
    expect(decoded.providerKind).toBe(payload.providerKind);
  });

  it("round-trips budget_set with all scopes", () => {
    for (const scope of ["global", "agent", "task"] as const) {
      const payload: BudgetSetAuditPayload = {
        ...validBudgetSet(),
        scope,
        subjectId: scope === "global" ? undefined : scope === "agent" ? "sam_agent" : ULID_B,
      };
      const json = costAuditPayloadToJsonValue("budget_set", payload);
      const decoded = costAuditPayloadFromJsonValue("budget_set", json);
      expect(decoded.scope).toBe(scope);
      expect(decoded.subjectId).toBe(payload.subjectId);
      expect(decoded.thresholdsBps).toEqual(payload.thresholdsBps);
    }
  });

  it("round-trips budget_threshold_crossed", () => {
    const payload = validCrossing();
    const json = costAuditPayloadToJsonValue("budget_threshold_crossed", payload);
    const decoded = costAuditPayloadFromJsonValue("budget_threshold_crossed", json);
    expect(decoded.budgetSetLsn).toBe(payload.budgetSetLsn);
    expect(decoded.crossedAtLsn).toBe(payload.crossedAtLsn);
    expect(decoded.thresholdBps).toBe(payload.thresholdBps);
    expect(decoded.crossedAt.toISOString()).toBe(payload.crossedAt.toISOString());
  });

  it("toJsonValue throws on unknown kind", () => {
    expect(() => costAuditPayloadToJsonValue("nope" as "cost_event", validCostEvent())).toThrow(
      /unknown CostAuditEventKind/,
    );
  });

  it("fromJsonValue throws on unknown kind", () => {
    expect(() => costAuditPayloadFromJsonValue("nope" as "cost_event", validCostEvent())).toThrow(
      /unknown CostAuditEventKind/,
    );
  });

  it("fromJsonValue rejects invalid payloads via the validator", () => {
    const broken = { ...validCostEvent(), occurredAt: "2026-05-08T18:00:00.000Z" };
    const json = { ...broken, amountMicroUsd: -1 };
    expect(() => costAuditPayloadFromJsonValue("cost_event", json)).toThrow();
  });

  it("fromJsonValue requires canonical ISO-8601 occurredAt strings", () => {
    const json = costAuditPayloadToJsonValue("cost_event", validCostEvent());
    const broken = { ...json, occurredAt: "not-a-date" };
    expect(() => costAuditPayloadFromJsonValue("cost_event", broken)).toThrow(
      /not a valid ISO-8601/,
    );

    const nonCanonical = { ...json, occurredAt: "2026-05-08T18:00:00Z" };
    expect(() => costAuditPayloadFromJsonValue("cost_event", nonCanonical)).toThrow(
      /canonical ISO-8601/,
    );
  });

  it("fromJsonValue rejects non-string occurredAt", () => {
    const json = costAuditPayloadToJsonValue("cost_event", validCostEvent());
    const broken = { ...json, occurredAt: 42 };
    expect(() => costAuditPayloadFromJsonValue("cost_event", broken)).toThrow(/ISO-8601 string/);
  });

  it("budget_set fromJsonValue throws when post-decode validation fails", () => {
    // agent scope without a subjectId parses (subjectId is optional in JSON) but
    // fails the post-decode validateBudgetSetAuditPayload check.
    const json = costAuditPayloadToJsonValue("budget_set", validBudgetSet());
    const broken = { ...json, scope: "agent" };
    expect(() => costAuditPayloadFromJsonValue("budget_set", broken)).toThrow(
      /AgentSlug when scope is agent/,
    );
  });

  it("budget_threshold_crossed fromJsonValue throws when post-decode validation fails", () => {
    // thresholdBps = 0 parses (it's still a number) but fails validation.
    const json = costAuditPayloadToJsonValue("budget_threshold_crossed", validCrossing());
    const broken = { ...json, thresholdBps: 0 };
    expect(() => costAuditPayloadFromJsonValue("budget_threshold_crossed", broken)).toThrow(
      /integer in \(0,/,
    );
  });
});

describe("costAuditPayloadToBytes", () => {
  it("encodes each kind to canonical JSON UTF-8 bytes", () => {
    const a = costAuditPayloadToBytes("cost_event", validCostEvent());
    const b = costAuditPayloadToBytes("budget_set", validBudgetSet());
    const c = costAuditPayloadToBytes("budget_threshold_crossed", validCrossing());
    expect(a).toBeInstanceOf(Uint8Array);
    expect(b).toBeInstanceOf(Uint8Array);
    expect(c).toBeInstanceOf(Uint8Array);
    expect(a.byteLength).toBeGreaterThan(0);
    expect(b.byteLength).toBeGreaterThan(0);
    expect(c.byteLength).toBeGreaterThan(0);
  });

  it("is deterministic for equivalent payloads", () => {
    const a1 = costAuditPayloadToBytes("cost_event", validCostEvent());
    const a2 = costAuditPayloadToBytes("cost_event", validCostEvent());
    expect(Buffer.from(a1).equals(Buffer.from(a2))).toBe(true);
  });

  it("throws on invalid payloads", () => {
    const bad = { ...validCostEvent(), amountMicroUsd: -1 } as unknown as CostEventAuditPayload;
    expect(() => costAuditPayloadToBytes("cost_event", bad)).toThrow();
  });

  it("throws on unknown kind", () => {
    expect(() => costAuditPayloadToBytes("nope" as "cost_event", validCostEvent())).toThrow(
      /unknown CostAuditEventKind/,
    );
  });

  it("encodes GENESIS_LSN-derived crossings without error", () => {
    const payload: BudgetThresholdCrossedAuditPayload = {
      ...validCrossing(),
      budgetSetLsn: GENESIS_LSN,
      crossedAtLsn: GENESIS_LSN,
    };
    expect(() => costAuditPayloadToBytes("budget_threshold_crossed", payload)).not.toThrow();
  });
});
