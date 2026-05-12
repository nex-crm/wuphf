// Cost ledger types and audit payload codecs.
//
// The cost ledger is the chokepoint for AI gateway spend: every model invocation
// appends one `cost_event` with `amountMicroUsd` (integer-only, no float drift),
// and `budget_set` / `budget_threshold_crossed` events drive enforcement. These
// types are the wire shape; broker projections (`cost_by_agent`, `cost_by_task`,
// `cost_budgets`) and the threshold-crossing reactor live in
// `packages/broker/src/cost-ledger/*`.
//
// Three audit kinds:
//   cost_event                — emitted by the gateway after each provider call.
//   budget_set                — emitted on POST/DELETE /api/v1/cost/budgets.
//                               A tombstone is `limitMicroUsd === 0`.
//   budget_threshold_crossed  — emitted by the reactor when cumulative spend
//                               crosses a configured threshold for some budget.
//                               `budgetSetLsn` is included in the projection's
//                               crossings key so increasing a budget re-arms
//                               its thresholds.
//
// Why MicroUsd as an integer brand: floats compounded across thousands of cost
// events drift; the §15.A sum invariant
// `sum(cost_events) == sum(cost_by_agent) == sum(cost_by_task)` is decidable only
// with integers. Bound at `MAX_COST_EVENT_AMOUNT_MICRO_USD` so a single record
// stays well under `Number.MAX_SAFE_INTEGER` even after accumulation.

import type { Brand } from "./brand.ts";
import {
  MAX_BUDGET_LIMIT_MICRO_USD,
  MAX_BUDGET_THRESHOLD_BPS,
  MAX_BUDGET_THRESHOLDS,
  MAX_COST_EVENT_AMOUNT_MICRO_USD,
  MAX_COST_MODEL_BYTES,
} from "./budgets.ts";
import { canonicalJSON } from "./canonical-json.ts";
import { type EventLsn, parseLsn } from "./event-lsn.ts";
import {
  type AgentSlug,
  asAgentSlug,
  asProviderKind,
  asReceiptId,
  asSignerIdentity,
  asTaskId,
  isAgentSlug,
  isProviderKind,
  isReceiptId,
  isSignerIdentity,
  isTaskId,
  type ProviderKind,
  type ReceiptId,
  type ReceiptValidationError,
  type ReceiptValidationResult,
  type SignerIdentity,
  type TaskId,
} from "./receipt.ts";
import {
  addError,
  assertKnownKeys,
  formatValidationErrors,
  hasOwn,
  isRecord,
  omitUndefined,
  pointer,
  recordValue,
  requireRecord,
} from "./receipt-utils.ts";

// ULID base32 alphabet (Crockford). Same shape used by ReceiptId / TaskId /
// ThreadId; declared inline so cost IDs don't depend on the receipt module's
// regex literals.
const ULID_RE = /^[0-9A-HJKMNP-TV-Z]{26}$/;

// ────────────────────────────────────────────────────────────────────────────
// Branded primitives
// ────────────────────────────────────────────────────────────────────────────

/**
 * BudgetId is a 26-character Crockford-base32 ULID brand. A single ULID per
 * (scope, scopeKey) tuple is allowed live; tombstones with `limitMicroUsd === 0`
 * retain the row for replay but mark the budget closed.
 */
export type BudgetId = Brand<string, "BudgetId">;

export function asBudgetId(s: string): BudgetId {
  if (!ULID_RE.test(s)) {
    throw new Error(`asBudgetId: not a valid ULID-shaped BudgetId: ${JSON.stringify(s)}`);
  }
  return s as BudgetId;
}

export function isBudgetId(value: unknown): value is BudgetId {
  return typeof value === "string" && ULID_RE.test(value);
}

/**
 * MicroUsd is an integer brand. One unit = one micro-USD (= 10⁻⁶ USD).
 *
 * Float dollars do not compose: 0.1 + 0.2 !== 0.3 and the §15.A sum invariant
 * (`sum(cost_events) == sum(by_agent) == sum(by_task)`) is not decidable on
 * floats once thousands of events accumulate. Integer micro-USD avoids this
 * entirely; `MAX_COST_EVENT_AMOUNT_MICRO_USD` keeps the per-event ceiling well
 * below `Number.MAX_SAFE_INTEGER` so accumulation across a long-lived ledger
 * still fits in JS numbers.
 */
export type MicroUsd = Brand<number, "MicroUsd">;

export function asMicroUsd(n: number): MicroUsd {
  if (!Number.isSafeInteger(n) || n < 0) {
    throw new Error(`asMicroUsd: expected non-negative safe integer, got ${n}`);
  }
  if (n > MAX_BUDGET_LIMIT_MICRO_USD) {
    throw new Error(
      `asMicroUsd: ${n} exceeds MAX_BUDGET_LIMIT_MICRO_USD (${MAX_BUDGET_LIMIT_MICRO_USD})`,
    );
  }
  return n as MicroUsd;
}

export function isMicroUsd(value: unknown): value is MicroUsd {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 0 &&
    value <= MAX_BUDGET_LIMIT_MICRO_USD
  );
}

export const BUDGET_SCOPE_VALUES = ["global", "agent", "task"] as const;
export type BudgetScope = (typeof BUDGET_SCOPE_VALUES)[number];

const BUDGET_SCOPE_SET: ReadonlySet<string> = new Set<string>(BUDGET_SCOPE_VALUES);

export function isBudgetScope(value: unknown): value is BudgetScope {
  return typeof value === "string" && BUDGET_SCOPE_SET.has(value);
}

// ────────────────────────────────────────────────────────────────────────────
// CostUnits
// ────────────────────────────────────────────────────────────────────────────

/**
 * Token counts for a single provider call. Cache fields distinguish
 * Anthropic-style prompt-caching reads / creations from regular input tokens —
 * they have separate per-token pricing in the provider SDK contracts.
 *
 * All fields are non-negative safe integers. Validator clamps and the gateway
 * passes through whatever the provider returned; if a provider does not expose
 * cache accounting, both cache fields are 0.
 */
export interface CostUnits {
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cacheReadTokens: number;
  readonly cacheCreationTokens: number;
}

const COST_UNITS_KEYS_TUPLE = [
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheCreationTokens",
] as const satisfies readonly (keyof CostUnits)[];
const COST_UNITS_KEYS: ReadonlySet<string> = new Set<string>(COST_UNITS_KEYS_TUPLE);

// ────────────────────────────────────────────────────────────────────────────
// Audit payloads
// ────────────────────────────────────────────────────────────────────────────

export type CostAuditEventKind = "cost_event" | "budget_set" | "budget_threshold_crossed";

const COST_AUDIT_EVENT_KIND_SET: ReadonlySet<string> = new Set<string>([
  "cost_event",
  "budget_set",
  "budget_threshold_crossed",
]);

export function isCostAuditEventKind(value: unknown): value is CostAuditEventKind {
  return typeof value === "string" && COST_AUDIT_EVENT_KIND_SET.has(value);
}

export interface CostEventAuditPayload {
  readonly receiptId?: ReceiptId | undefined;
  readonly agentSlug: AgentSlug;
  readonly taskId?: TaskId | undefined;
  readonly providerKind: ProviderKind;
  readonly model: string;
  readonly amountMicroUsd: MicroUsd;
  readonly units: CostUnits;
  readonly occurredAt: Date;
}

const COST_EVENT_KEYS_TUPLE = [
  "receiptId",
  "agentSlug",
  "taskId",
  "providerKind",
  "model",
  "amountMicroUsd",
  "units",
  "occurredAt",
] as const satisfies readonly (keyof CostEventAuditPayload)[];
const COST_EVENT_KEYS: ReadonlySet<string> = new Set<string>(COST_EVENT_KEYS_TUPLE);

export interface BudgetSetAuditPayload {
  readonly budgetId: BudgetId;
  readonly scope: BudgetScope;
  /**
   * For `scope === "agent"`, this is the AgentSlug. For `scope === "task"`,
   * this is the TaskId. For `scope === "global"`, this is `undefined`. The
   * cost-routes layer normalizes; codecs preserve absence vs explicit null
   * (absence wins to keep canonical JSON minimal — same shape as thread.ts
   * `baseRevisionId`).
   */
  readonly subjectId?: string | undefined;
  /**
   * 0 means tombstoned — projection treats the row as deleted, but the event
   * row stays in the log for replay. `MAX_BUDGET_LIMIT_MICRO_USD` is the
   * upper bound so accumulation stays well within `Number.MAX_SAFE_INTEGER`.
   */
  readonly limitMicroUsd: MicroUsd;
  /**
   * Threshold percentages stored as integer basis points (50bps = 0.5%).
   * Sorted ascending, deduplicated, all in (0, MAX_BUDGET_THRESHOLD_BPS].
   * Bounded length by MAX_BUDGET_THRESHOLDS so a single budget can't pin
   * the threshold-crosser reactor on adversarial input.
   */
  readonly thresholdsBps: readonly number[];
  readonly setBy: SignerIdentity;
  readonly setAt: Date;
}

const BUDGET_SET_KEYS_TUPLE = [
  "budgetId",
  "scope",
  "subjectId",
  "limitMicroUsd",
  "thresholdsBps",
  "setBy",
  "setAt",
] as const satisfies readonly (keyof BudgetSetAuditPayload)[];
const BUDGET_SET_KEYS: ReadonlySet<string> = new Set<string>(BUDGET_SET_KEYS_TUPLE);

export interface BudgetThresholdCrossedAuditPayload {
  readonly budgetId: BudgetId;
  /**
   * LSN of the `budget_set` event the crossing is scoped to. Increasing or
   * resetting a budget mints a new `budget_set` LSN; the broker projection
   * keys crossings by `(budgetId, budgetSetLsn, thresholdBps)` so re-arming
   * after a budget bump is automatic without changing the crossing PK.
   */
  readonly budgetSetLsn: EventLsn;
  readonly thresholdBps: number;
  readonly observedMicroUsd: MicroUsd;
  readonly limitMicroUsd: MicroUsd;
  /**
   * LSN of the triggering `cost_event`. Reactor MUST derive the crossing's
   * time from this LSN (or the triggering event's payload `occurredAt`), not
   * from `Date.now()` — replay must reproduce identical crossings.
   */
  readonly crossedAtLsn: EventLsn;
  readonly crossedAt: Date;
}

const BUDGET_THRESHOLD_CROSSED_KEYS_TUPLE = [
  "budgetId",
  "budgetSetLsn",
  "thresholdBps",
  "observedMicroUsd",
  "limitMicroUsd",
  "crossedAtLsn",
  "crossedAt",
] as const satisfies readonly (keyof BudgetThresholdCrossedAuditPayload)[];
const BUDGET_THRESHOLD_CROSSED_KEYS: ReadonlySet<string> = new Set<string>(
  BUDGET_THRESHOLD_CROSSED_KEYS_TUPLE,
);

export type CostAuditPayload =
  | CostEventAuditPayload
  | BudgetSetAuditPayload
  | BudgetThresholdCrossedAuditPayload;

// ────────────────────────────────────────────────────────────────────────────
// Validation
// ────────────────────────────────────────────────────────────────────────────

export type CostValidationError = ReceiptValidationError;
export type CostValidationResult = ReceiptValidationResult;

export function validateCostAuditPayloadForKind(
  kind: CostAuditEventKind,
  payload: unknown,
): CostValidationResult {
  if (kind === "cost_event") return validateCostEventAuditPayload(payload);
  if (kind === "budget_set") return validateBudgetSetAuditPayload(payload);
  if (kind === "budget_threshold_crossed")
    return validateBudgetThresholdCrossedAuditPayload(payload);
  throw new Error(unknownCostAuditEventKindMessage(kind));
}

export function validateCostEventAuditPayload(input: unknown): CostValidationResult {
  const errors: CostValidationError[] = [];
  validateCostEventAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateBudgetSetAuditPayload(input: unknown): CostValidationResult {
  const errors: CostValidationError[] = [];
  validateBudgetSetAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateBudgetThresholdCrossedAuditPayload(input: unknown): CostValidationResult {
  const errors: CostValidationError[] = [];
  validateBudgetThresholdCrossedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

function validateCostEventAuditPayloadValue(
  input: unknown,
  path: string,
  errors: CostValidationError[],
): void {
  if (!isRecord(input)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(input, path, COST_EVENT_KEYS, errors);
  if (hasOwn(input, "receiptId")) {
    const v = recordValue(input, "receiptId");
    if (v !== undefined && !isReceiptId(v)) {
      addError(errors, pointer(path, "receiptId"), "must be a valid ReceiptId");
    }
  }
  const agentSlug = recordValue(input, "agentSlug");
  if (!isAgentSlug(agentSlug)) {
    addError(errors, pointer(path, "agentSlug"), "must be a valid AgentSlug");
  }
  if (hasOwn(input, "taskId")) {
    const v = recordValue(input, "taskId");
    if (v !== undefined && !isTaskId(v)) {
      addError(errors, pointer(path, "taskId"), "must be a valid TaskId");
    }
  }
  const providerKind = recordValue(input, "providerKind");
  if (!isProviderKind(providerKind)) {
    addError(errors, pointer(path, "providerKind"), "must be a valid ProviderKind");
  }
  const model = recordValue(input, "model");
  if (typeof model !== "string" || model.length === 0) {
    addError(errors, pointer(path, "model"), "must be a non-empty string");
  } else if (utf8ByteLength(model) > MAX_COST_MODEL_BYTES) {
    addError(errors, pointer(path, "model"), `must be at most ${MAX_COST_MODEL_BYTES} UTF-8 bytes`);
  }
  validateMicroUsdField(input, "amountMicroUsd", path, errors, MAX_COST_EVENT_AMOUNT_MICRO_USD);
  validateCostUnitsValue(recordValue(input, "units"), pointer(path, "units"), errors);
  validateDateField(input, "occurredAt", path, errors);
}

function validateBudgetSetAuditPayloadValue(
  input: unknown,
  path: string,
  errors: CostValidationError[],
): void {
  if (!isRecord(input)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(input, path, BUDGET_SET_KEYS, errors);
  const budgetId = recordValue(input, "budgetId");
  if (!isBudgetId(budgetId)) {
    addError(errors, pointer(path, "budgetId"), "must be a ULID-shaped BudgetId");
  }
  const scope = recordValue(input, "scope");
  if (!isBudgetScope(scope)) {
    addError(errors, pointer(path, "scope"), "must be one of global|agent|task");
  } else {
    const subjectId = recordValue(input, "subjectId");
    if (scope === "global") {
      if (hasOwn(input, "subjectId") && subjectId !== undefined) {
        addError(errors, pointer(path, "subjectId"), "must be absent when scope is global");
      }
    } else if (scope === "agent") {
      if (!isAgentSlug(subjectId)) {
        addError(
          errors,
          pointer(path, "subjectId"),
          "must be a valid AgentSlug when scope is agent",
        );
      }
    } else if (scope === "task") {
      if (!isTaskId(subjectId)) {
        addError(errors, pointer(path, "subjectId"), "must be a valid TaskId when scope is task");
      }
    }
  }
  validateMicroUsdField(input, "limitMicroUsd", path, errors, MAX_BUDGET_LIMIT_MICRO_USD);
  validateThresholdsBpsValue(
    recordValue(input, "thresholdsBps"),
    pointer(path, "thresholdsBps"),
    errors,
  );
  const setBy = recordValue(input, "setBy");
  if (!isSignerIdentity(setBy)) {
    addError(errors, pointer(path, "setBy"), "must be a valid SignerIdentity");
  }
  validateDateField(input, "setAt", path, errors);
}

function validateBudgetThresholdCrossedAuditPayloadValue(
  input: unknown,
  path: string,
  errors: CostValidationError[],
): void {
  if (!isRecord(input)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(input, path, BUDGET_THRESHOLD_CROSSED_KEYS, errors);
  const budgetId = recordValue(input, "budgetId");
  if (!isBudgetId(budgetId)) {
    addError(errors, pointer(path, "budgetId"), "must be a ULID-shaped BudgetId");
  }
  validateEventLsnField(input, "budgetSetLsn", path, errors);
  const thresholdBps = recordValue(input, "thresholdBps");
  if (!isValidThresholdBps(thresholdBps)) {
    addError(
      errors,
      pointer(path, "thresholdBps"),
      `must be an integer in (0, ${MAX_BUDGET_THRESHOLD_BPS}]`,
    );
  }
  validateMicroUsdField(input, "observedMicroUsd", path, errors, MAX_BUDGET_LIMIT_MICRO_USD);
  validateMicroUsdField(input, "limitMicroUsd", path, errors, MAX_BUDGET_LIMIT_MICRO_USD);
  validateEventLsnField(input, "crossedAtLsn", path, errors);
  validateDateField(input, "crossedAt", path, errors);
}

function validateCostUnitsValue(input: unknown, path: string, errors: CostValidationError[]): void {
  if (!isRecord(input)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(input, path, COST_UNITS_KEYS, errors);
  for (const key of COST_UNITS_KEYS_TUPLE) {
    const v = recordValue(input, key);
    if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
      addError(errors, pointer(path, key), "must be a non-negative safe integer");
    }
  }
}

function validateMicroUsdField(
  input: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  errors: CostValidationError[],
  max: number,
): void {
  const v = recordValue(input, key);
  if (typeof v !== "number" || !Number.isSafeInteger(v) || v < 0) {
    addError(errors, pointer(path, key), "must be a non-negative safe integer");
    return;
  }
  if (v > max) {
    addError(errors, pointer(path, key), `must be at most ${max}`);
  }
}

function validateDateField(
  input: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  errors: CostValidationError[],
): void {
  const v = recordValue(input, key);
  if (!(v instanceof Date) || Number.isNaN(v.getTime())) {
    addError(errors, pointer(path, key), "must be a valid Date");
  }
}

function validateEventLsnField(
  input: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  errors: CostValidationError[],
): void {
  const v = recordValue(input, key);
  if (typeof v !== "string") {
    addError(errors, pointer(path, key), "must be an EventLsn string");
    return;
  }
  try {
    parseLsn(v as EventLsn);
  } catch (err) {
    addError(errors, pointer(path, key), (err as Error).message);
  }
}

function validateThresholdsBpsValue(
  input: unknown,
  path: string,
  errors: CostValidationError[],
): void {
  if (!Array.isArray(input)) {
    addError(errors, path, "must be an array");
    return;
  }
  if (input.length === 0) {
    addError(errors, path, "must contain at least one threshold");
    return;
  }
  if (input.length > MAX_BUDGET_THRESHOLDS) {
    addError(errors, path, `must contain at most ${MAX_BUDGET_THRESHOLDS} thresholds`);
    return;
  }
  let prior = 0;
  const seen = new Set<number>();
  for (let i = 0; i < input.length; i += 1) {
    const v = input[i];
    if (!isValidThresholdBps(v)) {
      addError(
        errors,
        pointer(path, String(i)),
        `must be an integer in (0, ${MAX_BUDGET_THRESHOLD_BPS}]`,
      );
      continue;
    }
    if (seen.has(v)) {
      addError(errors, pointer(path, String(i)), "must be unique within thresholds");
      continue;
    }
    seen.add(v);
    if (v <= prior) {
      addError(errors, pointer(path, String(i)), "must be ascending");
    }
    prior = v;
  }
}

function isValidThresholdBps(value: unknown): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value > 0 &&
    value <= MAX_BUDGET_THRESHOLD_BPS
  );
}

function validateKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
  errors: CostValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      addError(errors, pointer(basePath, key), "is not allowed");
    }
  }
}

function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

function unknownCostAuditEventKindMessage(kind: unknown): string {
  return `unknown CostAuditEventKind: ${String(kind)}`;
}

// ────────────────────────────────────────────────────────────────────────────
// JSON codecs
// ────────────────────────────────────────────────────────────────────────────

const TEXT_ENCODER = new TextEncoder();

export function costAuditPayloadToJsonValue(
  kind: CostAuditEventKind,
  payload: CostAuditPayload,
): Record<string, unknown> {
  if (kind === "cost_event") {
    const cost = payload as CostEventAuditPayload;
    return omitUndefined({
      receiptId: cost.receiptId,
      agentSlug: cost.agentSlug,
      taskId: cost.taskId,
      providerKind: cost.providerKind,
      model: cost.model,
      amountMicroUsd: cost.amountMicroUsd as number,
      units: {
        inputTokens: cost.units.inputTokens,
        outputTokens: cost.units.outputTokens,
        cacheReadTokens: cost.units.cacheReadTokens,
        cacheCreationTokens: cost.units.cacheCreationTokens,
      },
      occurredAt: cost.occurredAt.toISOString(),
    });
  }
  if (kind === "budget_set") {
    const budget = payload as BudgetSetAuditPayload;
    return omitUndefined({
      budgetId: budget.budgetId,
      scope: budget.scope,
      subjectId: budget.subjectId,
      limitMicroUsd: budget.limitMicroUsd as number,
      thresholdsBps: budget.thresholdsBps,
      setBy: budget.setBy,
      setAt: budget.setAt.toISOString(),
    });
  }
  if (kind === "budget_threshold_crossed") {
    const cross = payload as BudgetThresholdCrossedAuditPayload;
    return {
      budgetId: cross.budgetId,
      budgetSetLsn: cross.budgetSetLsn,
      thresholdBps: cross.thresholdBps,
      observedMicroUsd: cross.observedMicroUsd as number,
      limitMicroUsd: cross.limitMicroUsd as number,
      crossedAtLsn: cross.crossedAtLsn,
      crossedAt: cross.crossedAt.toISOString(),
    };
  }
  throw new Error(unknownCostAuditEventKindMessage(kind));
}

export function costAuditPayloadFromJsonValue(
  kind: CostAuditEventKind,
  value: unknown,
): CostAuditPayload {
  if (kind === "cost_event") return costEventAuditPayloadFromJsonValue(value);
  if (kind === "budget_set") return budgetSetAuditPayloadFromJsonValue(value);
  if (kind === "budget_threshold_crossed") {
    return budgetThresholdCrossedAuditPayloadFromJsonValue(value);
  }
  throw new Error(unknownCostAuditEventKindMessage(kind));
}

export function costAuditPayloadToBytes(
  kind: CostAuditEventKind,
  payload: CostAuditPayload,
): Uint8Array {
  const validation = validateCostAuditPayloadForKind(kind, payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return TEXT_ENCODER.encode(canonicalJSON(costAuditPayloadToJsonValue(kind, payload)));
}

function costEventAuditPayloadFromJsonValue(value: unknown): CostEventAuditPayload {
  const record = requireRecord(value, "");
  assertKnownKeys(record, "", COST_EVENT_KEYS);
  const unitsRaw = requireRecord(recordValue(record, "units"), "/units");
  const payload: CostEventAuditPayload = omitUndefined({
    receiptId: hasOwn(record, "receiptId")
      ? asReceiptId(recordValue(record, "receiptId") as string)
      : undefined,
    agentSlug: asAgentSlug(recordValue(record, "agentSlug") as string),
    taskId: hasOwn(record, "taskId")
      ? asTaskId(recordValue(record, "taskId") as string)
      : undefined,
    providerKind: asProviderKind(recordValue(record, "providerKind") as string),
    model: recordValue(record, "model") as string,
    amountMicroUsd: asMicroUsd(recordValue(record, "amountMicroUsd") as number),
    units: {
      inputTokens: recordValue(unitsRaw, "inputTokens") as number,
      outputTokens: recordValue(unitsRaw, "outputTokens") as number,
      cacheReadTokens: recordValue(unitsRaw, "cacheReadTokens") as number,
      cacheCreationTokens: recordValue(unitsRaw, "cacheCreationTokens") as number,
    },
    occurredAt: parseIsoDate(recordValue(record, "occurredAt"), "/occurredAt"),
  });
  const validation = validateCostEventAuditPayload(payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return payload;
}

function budgetSetAuditPayloadFromJsonValue(value: unknown): BudgetSetAuditPayload {
  const record = requireRecord(value, "");
  assertKnownKeys(record, "", BUDGET_SET_KEYS);
  const scope = recordValue(record, "scope") as BudgetScope;
  const subjectIdRaw = hasOwn(record, "subjectId")
    ? (recordValue(record, "subjectId") as string | undefined)
    : undefined;
  const payload: BudgetSetAuditPayload = omitUndefined({
    budgetId: asBudgetId(recordValue(record, "budgetId") as string),
    scope,
    subjectId: subjectIdRaw,
    limitMicroUsd: asMicroUsd(recordValue(record, "limitMicroUsd") as number),
    thresholdsBps: Array.from(recordValue(record, "thresholdsBps") as readonly number[]),
    setBy: asSignerIdentity(recordValue(record, "setBy") as string),
    setAt: parseIsoDate(recordValue(record, "setAt"), "/setAt"),
  });
  const validation = validateBudgetSetAuditPayload(payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return payload;
}

function budgetThresholdCrossedAuditPayloadFromJsonValue(
  value: unknown,
): BudgetThresholdCrossedAuditPayload {
  const record = requireRecord(value, "");
  assertKnownKeys(record, "", BUDGET_THRESHOLD_CROSSED_KEYS);
  const payload: BudgetThresholdCrossedAuditPayload = {
    budgetId: asBudgetId(recordValue(record, "budgetId") as string),
    budgetSetLsn: recordValue(record, "budgetSetLsn") as EventLsn,
    thresholdBps: recordValue(record, "thresholdBps") as number,
    observedMicroUsd: asMicroUsd(recordValue(record, "observedMicroUsd") as number),
    limitMicroUsd: asMicroUsd(recordValue(record, "limitMicroUsd") as number),
    crossedAtLsn: recordValue(record, "crossedAtLsn") as EventLsn,
    crossedAt: parseIsoDate(recordValue(record, "crossedAt"), "/crossedAt"),
  };
  const validation = validateBudgetThresholdCrossedAuditPayload(payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return payload;
}

function parseIsoDate(value: unknown, path: string): Date {
  if (typeof value !== "string") {
    throw new Error(`${path}: must be an ISO-8601 string`);
  }
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) {
    throw new Error(`${path}: not a valid ISO-8601 date: ${JSON.stringify(value)}`);
  }
  return d;
}
