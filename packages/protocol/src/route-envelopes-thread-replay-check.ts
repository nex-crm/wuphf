import { MAX_ROUTE_THREAD_LIST_ITEMS, validateRouteErrorMessageBudget } from "./budgets.ts";
import { canonicalJSON, type JsonValue } from "./canonical-json.ts";
import { type EventLsn, parseLsn } from "./event-lsn.ts";
import { asThreadId, type ThreadId } from "./receipt-types.ts";
import { assertKnownKeys, hasOwn, omitUndefined, requireRecord } from "./receipt-utils.ts";

const ROUTE_ENVELOPE_SCHEMA_VERSION = 1;
type RouteEnvelopeSchemaVersion = 1;

export interface ThreadReplayCheckReport {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly ok: boolean;
  readonly highestLsn: EventLsn;
  readonly eventsScanned: number;
  readonly discrepancies: readonly ThreadReplayCheckDiscrepancy[];
}

export type ThreadReplayCheckDiscrepancy =
  | {
      readonly kind: "event_payload_unparseable";
      readonly lsn: EventLsn;
      readonly eventType: string;
      readonly reason: string;
    }
  | {
      readonly kind: "approval_replay_failed";
      readonly reason: string;
    }
  | {
      readonly kind: "thread_state_row_missing" | "thread_state_row_ghost";
      readonly threadId: ThreadId;
    }
  | {
      readonly kind: "thread_state_field_mismatch";
      readonly threadId: ThreadId;
      readonly field: string;
      readonly replayed: JsonValue;
      readonly stored: JsonValue;
    }
  | {
      readonly kind: "thread_receipt_index_mismatch";
      readonly threadId: ThreadId;
      readonly field: "receiptIds" | "taskIds" | "latestReceiptStatus";
      readonly replayed: JsonValue;
      readonly stored: JsonValue;
    }
  | {
      readonly kind: "thread_pinned_approvals_mismatch";
      readonly threadId: ThreadId;
      readonly replayed: JsonValue;
      readonly stored: JsonValue;
    }
  | {
      readonly kind: "thread_effective_status_mismatch";
      readonly threadId: ThreadId;
      readonly field:
        | "effectiveStatus"
        | "attentionReason"
        | "boardColumn"
        | "currentSeat"
        | "pendingApprovalCount";
      readonly replayed: JsonValue;
      readonly stored: JsonValue;
    }
  | {
      readonly kind: "thread_log_invariant_violation";
      readonly lsn: EventLsn;
      readonly eventType: string;
      readonly reason: string;
      readonly threadId?: ThreadId | undefined;
      readonly expected?: JsonValue | undefined;
      readonly actual?: JsonValue | undefined;
    };

type ThreadReplayCheckReportWire = Readonly<
  Record<"schemaVersion" | "ok" | "highestLsn" | "eventsScanned" | "discrepancies", unknown>
>;
type ThreadReplayCheckDiscrepancyWire = Readonly<
  Record<
    | "kind"
    | "lsn"
    | "eventType"
    | "reason"
    | "threadId"
    | "field"
    | "replayed"
    | "stored"
    | "expected"
    | "actual",
    unknown
  >
>;

const THREAD_REPLAY_CHECK_REPORT_KEYS_TUPLE = [
  "schemaVersion",
  "ok",
  "highestLsn",
  "eventsScanned",
  "discrepancies",
] as const satisfies readonly (keyof ThreadReplayCheckReportWire)[];
const THREAD_REPLAY_CHECK_DISCREPANCY_KEYS_TUPLE = [
  "kind",
  "lsn",
  "eventType",
  "reason",
  "threadId",
  "field",
  "replayed",
  "stored",
  "expected",
  "actual",
] as const satisfies readonly (keyof ThreadReplayCheckDiscrepancyWire)[];

const THREAD_REPLAY_CHECK_REPORT_KEYS: ReadonlySet<string> = new Set(
  THREAD_REPLAY_CHECK_REPORT_KEYS_TUPLE,
);
const THREAD_REPLAY_CHECK_DISCREPANCY_KEYS: ReadonlySet<string> = new Set(
  THREAD_REPLAY_CHECK_DISCREPANCY_KEYS_TUPLE,
);
const MAX_THREAD_REPLAY_CHECK_DISCREPANCIES = MAX_ROUTE_THREAD_LIST_ITEMS * 8;
const THREAD_REPLAY_CHECK_RECEIPT_INDEX_FIELDS: ReadonlySet<string> = new Set([
  "receiptIds",
  "taskIds",
  "latestReceiptStatus",
]);
const THREAD_REPLAY_CHECK_EFFECTIVE_STATUS_FIELDS: ReadonlySet<string> = new Set([
  "effectiveStatus",
  "attentionReason",
  "boardColumn",
  "currentSeat",
  "pendingApprovalCount",
]);

export function threadReplayCheckReportFromJson(value: unknown): ThreadReplayCheckReport {
  const record = requireRecord(value, "threadReplayCheckReport");
  assertKnownKeys(record, "threadReplayCheckReport", THREAD_REPLAY_CHECK_REPORT_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadReplayCheckReport");
  return {
    schemaVersion,
    ok: requiredBoolean(record, "ok", "threadReplayCheckReport.ok"),
    highestLsn: eventLsnFromJson(
      requiredString(record, "highestLsn", "threadReplayCheckReport.highestLsn"),
      "threadReplayCheckReport.highestLsn",
    ),
    eventsScanned: requiredNonNegativeSafeInteger(
      record,
      "eventsScanned",
      "threadReplayCheckReport.eventsScanned",
    ),
    discrepancies: threadReplayCheckDiscrepanciesFromJson(
      requiredField(record, "discrepancies", "threadReplayCheckReport.discrepancies"),
      "threadReplayCheckReport.discrepancies",
    ),
  };
}

export function threadReplayCheckReportToJsonValue(
  report: ThreadReplayCheckReport,
): Readonly<Record<string, unknown>> {
  if (report.discrepancies.length > MAX_THREAD_REPLAY_CHECK_DISCREPANCIES) {
    throw new Error(
      `threadReplayCheckReport.discrepancies: length exceeds MAX_THREAD_REPLAY_CHECK_DISCREPANCIES: ${report.discrepancies.length} > ${MAX_THREAD_REPLAY_CHECK_DISCREPANCIES}`,
    );
  }
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    ok: report.ok,
    highestLsn: report.highestLsn,
    eventsScanned: report.eventsScanned,
    discrepancies: report.discrepancies.map(threadReplayCheckDiscrepancyToJsonValue),
  };
}

function threadReplayCheckDiscrepanciesFromJson(
  value: unknown,
  path: string,
): readonly ThreadReplayCheckDiscrepancy[] {
  if (!Array.isArray(value)) {
    throw new Error(`${path}: must be an array`);
  }
  if (value.length > MAX_THREAD_REPLAY_CHECK_DISCREPANCIES) {
    throw new Error(
      `${path}: length exceeds MAX_THREAD_REPLAY_CHECK_DISCREPANCIES: ${value.length} > ${MAX_THREAD_REPLAY_CHECK_DISCREPANCIES}`,
    );
  }
  return value.map((item, index) =>
    threadReplayCheckDiscrepancyFromJsonValue(item, `${path}/${index}`),
  );
}

function threadReplayCheckDiscrepancyFromJsonValue(
  value: unknown,
  path: string,
): ThreadReplayCheckDiscrepancy {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, THREAD_REPLAY_CHECK_DISCREPANCY_KEYS);
  const kind = requiredString(record, "kind", `${path}.kind`);
  switch (kind) {
    case "event_payload_unparseable":
      return {
        kind,
        lsn: eventLsnFromJson(requiredString(record, "lsn", `${path}.lsn`), `${path}.lsn`),
        eventType: requiredNonEmptyString(record, "eventType", `${path}.eventType`),
        reason: routeErrorMessageFromJson(requiredString(record, "reason", `${path}.reason`)),
      };
    case "approval_replay_failed":
      return {
        kind,
        reason: routeErrorMessageFromJson(requiredString(record, "reason", `${path}.reason`)),
      };
    case "thread_state_row_missing":
    case "thread_state_row_ghost":
      return {
        kind,
        threadId: threadIdFromJson(
          requiredString(record, "threadId", `${path}.threadId`),
          `${path}.threadId`,
        ),
      };
    case "thread_state_field_mismatch":
      return {
        kind,
        threadId: threadIdFromJson(
          requiredString(record, "threadId", `${path}.threadId`),
          `${path}.threadId`,
        ),
        field: requiredNonEmptyString(record, "field", `${path}.field`),
        replayed: replayCheckJsonField(record, "replayed", `${path}.replayed`),
        stored: replayCheckJsonField(record, "stored", `${path}.stored`),
      };
    case "thread_receipt_index_mismatch":
      return {
        kind,
        threadId: threadIdFromJson(
          requiredString(record, "threadId", `${path}.threadId`),
          `${path}.threadId`,
        ),
        field: replayCheckEnumField(
          record,
          "field",
          `${path}.field`,
          THREAD_REPLAY_CHECK_RECEIPT_INDEX_FIELDS,
        ) as "receiptIds" | "taskIds" | "latestReceiptStatus",
        replayed: replayCheckJsonField(record, "replayed", `${path}.replayed`),
        stored: replayCheckJsonField(record, "stored", `${path}.stored`),
      };
    case "thread_pinned_approvals_mismatch":
      return {
        kind,
        threadId: threadIdFromJson(
          requiredString(record, "threadId", `${path}.threadId`),
          `${path}.threadId`,
        ),
        replayed: replayCheckJsonField(record, "replayed", `${path}.replayed`),
        stored: replayCheckJsonField(record, "stored", `${path}.stored`),
      };
    case "thread_effective_status_mismatch":
      return {
        kind,
        threadId: threadIdFromJson(
          requiredString(record, "threadId", `${path}.threadId`),
          `${path}.threadId`,
        ),
        field: replayCheckEnumField(
          record,
          "field",
          `${path}.field`,
          THREAD_REPLAY_CHECK_EFFECTIVE_STATUS_FIELDS,
        ) as
          | "effectiveStatus"
          | "attentionReason"
          | "boardColumn"
          | "currentSeat"
          | "pendingApprovalCount",
        replayed: replayCheckJsonField(record, "replayed", `${path}.replayed`),
        stored: replayCheckJsonField(record, "stored", `${path}.stored`),
      };
    case "thread_log_invariant_violation":
      return omitUndefined({
        kind,
        lsn: eventLsnFromJson(requiredString(record, "lsn", `${path}.lsn`), `${path}.lsn`),
        eventType: requiredNonEmptyString(record, "eventType", `${path}.eventType`),
        reason: routeErrorMessageFromJson(requiredString(record, "reason", `${path}.reason`)),
        threadId: optionalThreadId(record, "threadId", `${path}.threadId`),
        expected: optionalReplayCheckJsonField(record, "expected", `${path}.expected`),
        actual: optionalReplayCheckJsonField(record, "actual", `${path}.actual`),
      });
    default:
      throw new Error(`${path}.kind: unknown thread replay-check discrepancy kind ${kind}`);
  }
}

function threadReplayCheckDiscrepancyToJsonValue(
  discrepancy: ThreadReplayCheckDiscrepancy,
): Readonly<Record<string, unknown>> {
  switch (discrepancy.kind) {
    case "event_payload_unparseable":
      return {
        kind: discrepancy.kind,
        lsn: discrepancy.lsn,
        eventType: discrepancy.eventType,
        reason: routeErrorMessageToJsonValue(discrepancy.reason),
      };
    case "approval_replay_failed":
      return {
        kind: discrepancy.kind,
        reason: routeErrorMessageToJsonValue(discrepancy.reason),
      };
    case "thread_state_row_missing":
    case "thread_state_row_ghost":
      return {
        kind: discrepancy.kind,
        threadId: discrepancy.threadId,
      };
    case "thread_state_field_mismatch":
    case "thread_receipt_index_mismatch":
    case "thread_effective_status_mismatch":
      return {
        kind: discrepancy.kind,
        threadId: discrepancy.threadId,
        field: discrepancy.field,
        replayed: replayCheckJsonToJsonValue(discrepancy.replayed, `${discrepancy.kind}.replayed`),
        stored: replayCheckJsonToJsonValue(discrepancy.stored, `${discrepancy.kind}.stored`),
      };
    case "thread_pinned_approvals_mismatch":
      return {
        kind: discrepancy.kind,
        threadId: discrepancy.threadId,
        replayed: replayCheckJsonToJsonValue(discrepancy.replayed, `${discrepancy.kind}.replayed`),
        stored: replayCheckJsonToJsonValue(discrepancy.stored, `${discrepancy.kind}.stored`),
      };
    case "thread_log_invariant_violation":
      return omitUndefined({
        kind: discrepancy.kind,
        lsn: discrepancy.lsn,
        eventType: discrepancy.eventType,
        reason: routeErrorMessageToJsonValue(discrepancy.reason),
        threadId: discrepancy.threadId,
        expected:
          discrepancy.expected === undefined
            ? undefined
            : replayCheckJsonToJsonValue(discrepancy.expected, "thread_log_invariant.expected"),
        actual:
          discrepancy.actual === undefined
            ? undefined
            : replayCheckJsonToJsonValue(discrepancy.actual, "thread_log_invariant.actual"),
      });
  }
}

function optionalSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): RouteEnvelopeSchemaVersion {
  if (!hasOwn(record, key)) return ROUTE_ENVELOPE_SCHEMA_VERSION;
  const path = `${basePath}.${key}`;
  const value = requiredField(record, key, path);
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw new Error(`${path}: must be an integer`);
  }
  if (value > ROUTE_ENVELOPE_SCHEMA_VERSION) {
    throw new Error(`${path}: unsupported schemaVersion`);
  }
  if (value !== ROUTE_ENVELOPE_SCHEMA_VERSION) {
    throw new Error(`${path}: must be ${ROUTE_ENVELOPE_SCHEMA_VERSION}`);
  }
  return ROUTE_ENVELOPE_SCHEMA_VERSION;
}

function requiredBoolean(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): boolean {
  const value = requiredField(record, key, path);
  if (typeof value !== "boolean") {
    throw new Error(`${path}: must be a boolean`);
  }
  return value;
}

function requiredNonNegativeSafeInteger(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): number {
  const value = requiredField(record, key, path);
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path}: must be a non-negative safe integer`);
  }
  return value;
}

function requiredField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown {
  if (!hasOwn(record, key)) {
    throw new Error(`${path}: is required`);
  }
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${path}: must be a data property`);
  }
  if (descriptor.value === undefined) {
    throw new Error(`${path}: is required`);
  }
  return descriptor.value;
}

function optionalField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): unknown | undefined {
  if (!hasOwn(record, key)) return undefined;
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  if (descriptor === undefined || !("value" in descriptor)) {
    throw new Error(`${path}: must be a data property`);
  }
  return descriptor.value;
}

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return value;
}

function requiredNonEmptyString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredString(record, key, path);
  if (value.length === 0) {
    throw new Error(`${path}: must be a non-empty string`);
  }
  return value;
}

function threadIdFromJson(value: string, path: string): ThreadId {
  try {
    return asThreadId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function optionalThreadId(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): ThreadId | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${path}: must be a string`);
  return threadIdFromJson(value, path);
}

function eventLsnFromJson(value: string, path: string): EventLsn {
  try {
    parseLsn(value as EventLsn);
    return value as EventLsn;
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function routeErrorMessageFromJson(value: string): string {
  const budget = validateRouteErrorMessageBudget(value);
  if (!budget.ok) throw new Error(`routeError.message: ${budget.reason}`);
  return value;
}

function routeErrorMessageToJsonValue(value: string): string {
  const budget = validateRouteErrorMessageBudget(value);
  if (!budget.ok) throw new Error(`routeError.message: ${budget.reason}`);
  return value;
}

function replayCheckEnumField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  allowed: ReadonlySet<string>,
): string {
  const value = requiredString(record, key, path);
  if (!allowed.has(value)) {
    throw new Error(`${path}: unsupported field ${value}`);
  }
  return value;
}

function replayCheckJsonField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): JsonValue {
  return replayCheckJsonToJsonValue(requiredField(record, key, path), path);
}

function optionalReplayCheckJsonField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): JsonValue | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  return replayCheckJsonToJsonValue(value, path);
}

function replayCheckJsonToJsonValue(value: unknown, path: string): JsonValue {
  try {
    canonicalJSON(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
  return value as JsonValue;
}
