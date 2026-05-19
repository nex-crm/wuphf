import {
  asAgentSlug,
  asBudgetId,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  BUDGET_SCOPE_VALUES,
  type BudgetSetAuditPayload,
  type BudgetThresholdCrossedAuditPayload,
  type CostEventAuditPayload,
  canonicalJSON,
  costAuditPayloadFromJsonValue,
  costAuditPayloadToBytes,
  costAuditPayloadToJsonValue,
  type EventLsn,
  isBudgetId,
  isBudgetScope,
  isCostAuditEventKind,
  isMicroUsd,
  MAX_BUDGET_THRESHOLD_BPS,
  MAX_BUDGET_THRESHOLDS,
  MAX_COST_EVENT_AMOUNT_MICRO_USD,
  MAX_COST_MODEL_BYTES,
  MINIMUM_PROTOCOL_VERSION_FOR_PROVIDER_KIND,
  validateBudgetSetAuditPayload,
  validateBudgetThresholdCrossedAuditPayload,
  validateCostAuditPayloadForKind,
  validateCostEventAuditPayload,
} from "../../src/index.ts";
import { expectEqual, expectThrows, header, textDecoder } from "./harness.ts";

export function runCostScenarios(): void {
  header(25, "Cost event uses integer MicroUsd — float drift is a wire-shape break");
  // ────────────────────────────────────────────────────────────────────────
  // The §15.A invariant sum(cost_events) == sum(by_agent) == sum(by_task) is only
  // decidable on integers. A 0.1+0.2 style accumulation across thousands of
  // events is exactly what breaks ledger reconciliation in production.
  const costEvent: CostEventAuditPayload = {
    receiptId: asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV"),
    agentSlug: asAgentSlug("primary"),
    taskId: asTaskId("01BRZ3NDEKTSV4RRFFQ69G5FA0"),
    providerKind: asProviderKind("anthropic"),
    model: "claude-opus-4-7",
    amountMicroUsd: asMicroUsd(2_500_000),
    units: { inputTokens: 1_024, outputTokens: 512, cacheReadTokens: 0, cacheCreationTokens: 0 },
    occurredAt: new Date("2026-05-08T18:03:00.000Z"),
  };
  expectEqual("typed cost_event validates", validateCostEventAuditPayload(costEvent), { ok: true });
  expectThrows(() => asMicroUsd(1.5), /non-negative safe integer/);
  expectThrows(() => asMicroUsd(-1), /non-negative safe integer/);
  // Brand bound = MAX_BUDGET_LIMIT_MICRO_USD ($1M); per-event cap
  // MAX_COST_EVENT_AMOUNT_MICRO_USD ($100) is enforced in the validator so a
  // rogue cost_event over $100 cannot dominate a daily budget even if the
  // MicroUsd brand accepted the value.
  const overEventCap = asMicroUsd(MAX_COST_EVENT_AMOUNT_MICRO_USD + 1);
  expectEqual(
    "validator rejects amount over MAX_COST_EVENT_AMOUNT_MICRO_USD",
    validateCostEventAuditPayload({ ...costEvent, amountMicroUsd: overEventCap }).ok,
    false,
  );
  const costBytes = costAuditPayloadToBytes("cost_event", costEvent);
  const roundTrip = costAuditPayloadFromJsonValue(
    "cost_event",
    JSON.parse(textDecoder.decode(costBytes)),
  );
  expectEqual("cost_event round-trips through canonical bytes", roundTrip, costEvent);

  header(26, "Budget thresholds: ascending, deduplicated, bounded");
  // ────────────────────────────────────────────────────────────────────────
  // Threshold arrays drive the reactor — a duplicate would re-fire the same
  // crossing; a descending sequence would skip thresholds during scan; an
  // unbounded array would let one budget pin the reactor on adversarial input.
  const goodBudget: BudgetSetAuditPayload = {
    budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
    scope: "global",
    limitMicroUsd: asMicroUsd(5_000_000),
    thresholdsBps: [5_000, 8_000, 10_000],
    setBy: asSignerIdentity("fran@example.com"),
    setAt: new Date("2026-05-08T18:00:00.000Z"),
  };
  expectEqual("ascending unique thresholds accepted", validateBudgetSetAuditPayload(goodBudget), {
    ok: true,
  });
  expectEqual(
    "duplicate threshold rejected",
    validateBudgetSetAuditPayload({ ...goodBudget, thresholdsBps: [5_000, 5_000] }).ok,
    false,
  );
  expectEqual(
    "descending threshold rejected",
    validateBudgetSetAuditPayload({ ...goodBudget, thresholdsBps: [9_000, 5_000] }).ok,
    false,
  );
  expectEqual(
    "empty threshold list rejected",
    validateBudgetSetAuditPayload({ ...goodBudget, thresholdsBps: [] }).ok,
    false,
  );
  const overCapThresholds = Array.from(
    { length: MAX_BUDGET_THRESHOLDS + 1 },
    (_, i) => (i + 1) * 100,
  );
  expectEqual(
    `more than ${MAX_BUDGET_THRESHOLDS} thresholds rejected`,
    validateBudgetSetAuditPayload({ ...goodBudget, thresholdsBps: overCapThresholds }).ok,
    false,
  );
  // global scope MUST have absent subjectId; agent/task MUST have matching brand.
  expectEqual(
    "global scope with subjectId rejected",
    validateBudgetSetAuditPayload({
      ...goodBudget,
      subjectId: "primary",
    }).ok,
    false,
  );
  expectEqual(
    "agent scope without AgentSlug subjectId rejected",
    validateBudgetSetAuditPayload({ ...goodBudget, scope: "agent" }).ok,
    false,
  );
  // limit === 0 is the tombstone marker — must validate as ok (codec preserves 0).
  expectEqual(
    "tombstone (limit=0) accepted as budget_set",
    validateBudgetSetAuditPayload({ ...goodBudget, limitMicroUsd: asMicroUsd(0) }),
    { ok: true },
  );

  header(27, "Threshold crossing payload carries budgetSetLsn so bumps re-arm");
  // ────────────────────────────────────────────────────────────────────────
  // The reactor projection keys crossings by (budgetId, budgetSetLsn, thresholdBps).
  // Without budgetSetLsn, raising a budget would silently re-fire the existing
  // crossing rows; with it, a new budget_set LSN re-arms thresholds automatically.
  const crossing: BudgetThresholdCrossedAuditPayload = {
    budgetId: asBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"),
    budgetSetLsn: "v1:5" as EventLsn,
    thresholdBps: 5_000,
    observedMicroUsd: asMicroUsd(2_500_000),
    limitMicroUsd: asMicroUsd(5_000_000),
    crossedAtLsn: "v1:4" as EventLsn,
    crossedAt: new Date("2026-05-08T18:03:00.001Z"),
  };
  expectEqual(
    "crossing payload with valid LSNs validates",
    validateBudgetThresholdCrossedAuditPayload(crossing),
    { ok: true },
  );
  expectEqual(
    "crossing payload rejects malformed budgetSetLsn",
    validateBudgetThresholdCrossedAuditPayload({
      ...crossing,
      budgetSetLsn: "v0:bogus" as EventLsn,
    }).ok,
    false,
  );
  expectEqual(
    "crossing payload rejects out-of-range thresholdBps",
    validateBudgetThresholdCrossedAuditPayload({ ...crossing, thresholdBps: 20_000 }).ok,
    false,
  );
  // Canonical bytes for the crossing must reproduce the cost_event/budget_set chain
  // position: this is the same byte projection hashed by the audit chain.
  const crossingBytes = costAuditPayloadToBytes("budget_threshold_crossed", crossing);
  expectEqual(
    "crossing canonical bytes parseable as canonical JSON",
    canonicalJSON(JSON.parse(textDecoder.decode(crossingBytes))),
    textDecoder.decode(crossingBytes),
  );

  header(28, "Cost ledger surface guards: brand guards, kind dispatch, closed enums");
  // ────────────────────────────────────────────────────────────────────────
  // Public exports must each be exercised — these guards and dispatchers are
  // the public boundary; if any rot, downstream packages silently break.
  expectEqual("isMicroUsd accepts a valid MicroUsd", isMicroUsd(asMicroUsd(1_000)), true);
  expectEqual("isMicroUsd rejects 1.5", isMicroUsd(1.5), false);
  expectEqual("isMicroUsd rejects -1", isMicroUsd(-1), false);
  expectEqual("isBudgetId accepts ULID-shaped id", isBudgetId("01ARZ3NDEKTSV4RRFFQ69G5FAZ"), true);
  expectEqual("isBudgetId rejects lowercase", isBudgetId("01arz3ndektsv4rrffq69g5faz"), false);
  expectEqual("isBudgetScope accepts global|agent|task", isBudgetScope("agent"), true);
  expectEqual("isBudgetScope rejects bogus", isBudgetScope("user"), false);
  expectEqual("BUDGET_SCOPE_VALUES is the closed scope tuple", Array.from(BUDGET_SCOPE_VALUES), [
    "global",
    "agent",
    "task",
  ]);
  expectEqual(
    "ProviderKind compatibility floor is public",
    MINIMUM_PROTOCOL_VERSION_FOR_PROVIDER_KIND,
    "cost-provider-kind-v1",
  );
  expectEqual("isCostAuditEventKind accepts cost_event", isCostAuditEventKind("cost_event"), true);
  expectEqual("isCostAuditEventKind rejects unknown", isCostAuditEventKind("budget_unset"), false);
  expectEqual(
    "validateCostAuditPayloadForKind dispatches by kind",
    validateCostAuditPayloadForKind("cost_event", costEvent),
    { ok: true },
  );
  expectEqual(
    "MAX_BUDGET_THRESHOLD_BPS bounds threshold space at 10000 (= 100%)",
    MAX_BUDGET_THRESHOLD_BPS,
    10_000,
  );
  expectEqual(
    "MAX_COST_MODEL_BYTES bounds cost_event.model at 128 UTF-8 bytes",
    MAX_COST_MODEL_BYTES,
    128,
  );
  // costAuditPayloadToJsonValue is the plain-JSON projection used before
  // canonicalJSON encodes for the wire. Object identity-shape is locked here.
  const costJson = costAuditPayloadToJsonValue("cost_event", costEvent);
  expectEqual(
    "costAuditPayloadToJsonValue projects amount as plain number",
    typeof (costJson as Record<string, unknown>).amountMicroUsd,
    "number",
  );
  expectEqual(
    "costAuditPayloadToJsonValue projects occurredAt as ISO string",
    (costJson as Record<string, unknown>).occurredAt,
    "2026-05-08T18:03:00.000Z",
  );
}
