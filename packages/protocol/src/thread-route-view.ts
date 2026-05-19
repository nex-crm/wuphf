import { MAX_ROUTE_THREAD_LIST_ITEMS } from "./budgets.ts";
import { assertKnownKeys, hasOwn, omitUndefined, requireRecord } from "./receipt-utils.ts";
import { type Thread, threadFromJsonValue, threadToJsonValue } from "./thread.ts";

export const THREAD_EFFECTIVE_STATUS_VALUES = [
  "open",
  "in_progress",
  "needs_review",
  "needs_attention",
  "merged",
  "closed",
] as const;
export type ThreadEffectiveStatus = (typeof THREAD_EFFECTIVE_STATUS_VALUES)[number];

export const THREAD_ATTENTION_REASON_VALUES = ["pending_approval", "failed", "stalled"] as const;
export type ThreadAttentionReason = (typeof THREAD_ATTENTION_REASON_VALUES)[number];

export const THREAD_BOARD_COLUMN_VALUES = ["running", "review", "needs_me", "done"] as const;
export type ThreadBoardColumn = (typeof THREAD_BOARD_COLUMN_VALUES)[number];

export const THREAD_CURRENT_SEAT_VALUES = ["agent", "human"] as const;
export type ThreadCurrentSeat = (typeof THREAD_CURRENT_SEAT_VALUES)[number];

export interface ThreadView extends Thread {
  readonly effectiveStatus: ThreadEffectiveStatus;
  readonly attentionReason?: ThreadAttentionReason | undefined;
  readonly boardColumn: ThreadBoardColumn;
  readonly currentSeat: ThreadCurrentSeat;
  readonly pendingApprovalCount: number;
}

type ThreadViewWire = Readonly<
  Record<
    | "thread_id"
    | "title"
    | "status"
    | "spec"
    | "external_refs"
    | "task_ids"
    | "created_by"
    | "created_at"
    | "updated_at"
    | "closed_at"
    | "effectiveStatus"
    | "attentionReason"
    | "boardColumn"
    | "currentSeat"
    | "pendingApprovalCount",
    unknown
  >
>;

const THREAD_VIEW_KEYS_TUPLE = [
  "thread_id",
  "title",
  "status",
  "spec",
  "external_refs",
  "task_ids",
  "created_by",
  "created_at",
  "updated_at",
  "closed_at",
  "effectiveStatus",
  "attentionReason",
  "boardColumn",
  "currentSeat",
  "pendingApprovalCount",
] as const satisfies readonly (keyof ThreadViewWire)[];
const THREAD_VIEW_KEYS: ReadonlySet<string> = new Set(THREAD_VIEW_KEYS_TUPLE);
const THREAD_EFFECTIVE_STATUS_SET: ReadonlySet<string> = new Set<string>(
  THREAD_EFFECTIVE_STATUS_VALUES,
);
const THREAD_ATTENTION_REASON_SET: ReadonlySet<string> = new Set<string>(
  THREAD_ATTENTION_REASON_VALUES,
);
const THREAD_BOARD_COLUMN_SET: ReadonlySet<string> = new Set<string>(THREAD_BOARD_COLUMN_VALUES);
const THREAD_CURRENT_SEAT_SET: ReadonlySet<string> = new Set<string>(THREAD_CURRENT_SEAT_VALUES);

export function threadViewFromJson(value: unknown): ThreadView {
  return threadViewFromJsonValue(value, "threadView");
}

export function threadViewToJsonValue(view: ThreadView): Readonly<Record<string, unknown>> {
  validateThreadViewStatusCoupling(view, "threadView");
  const threadWire = threadToJsonValue(view);
  return omitUndefined({
    ...threadWire,
    effectiveStatus: view.effectiveStatus,
    attentionReason: view.attentionReason,
    boardColumn: view.boardColumn,
    currentSeat: view.currentSeat,
    pendingApprovalCount: validatePendingApprovalCount(view.pendingApprovalCount),
  });
}

export function threadViewFromJsonValue(value: unknown, path: string): ThreadView {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, THREAD_VIEW_KEYS);
  const thread = threadFromJsonValue(threadJsonFromThreadViewRecord(record));
  const effectiveStatus = threadEffectiveStatusFromJson(
    requiredString(record, "effectiveStatus", `${path}.effectiveStatus`),
    `${path}.effectiveStatus`,
  );
  const attentionReason = optionalThreadAttentionReason(
    record,
    "attentionReason",
    `${path}.attentionReason`,
  );
  const view: ThreadView = omitUndefined({
    ...thread,
    effectiveStatus,
    attentionReason,
    boardColumn: threadBoardColumnFromJson(
      requiredString(record, "boardColumn", `${path}.boardColumn`),
      `${path}.boardColumn`,
    ),
    currentSeat: threadCurrentSeatFromJson(
      requiredString(record, "currentSeat", `${path}.currentSeat`),
      `${path}.currentSeat`,
    ),
    pendingApprovalCount: pendingApprovalCountFromJson(
      requiredField(record, "pendingApprovalCount", `${path}.pendingApprovalCount`),
      `${path}.pendingApprovalCount`,
    ),
  });
  validateThreadViewStatusCoupling(view, path);
  return view;
}

export function threadArrayFromJson(value: unknown, path: string): readonly ThreadView[] {
  if (!Array.isArray(value)) {
    throw new Error(`${path}: must be an array`);
  }
  if (value.length > MAX_ROUTE_THREAD_LIST_ITEMS) {
    throw new Error(
      `${path}: length exceeds MAX_ROUTE_THREAD_LIST_ITEMS: ${value.length} > ${MAX_ROUTE_THREAD_LIST_ITEMS}`,
    );
  }
  return value.map((item, index) => threadViewFromJsonValue(item, `${path}/${index}`));
}

function threadJsonFromThreadViewRecord(
  record: Readonly<Record<string, unknown>>,
): Readonly<Record<string, unknown>> {
  const threadRecord = record as ThreadViewWire;
  return omitUndefined({
    thread_id: threadRecord.thread_id,
    title: threadRecord.title,
    status: threadRecord.status,
    spec: threadRecord.spec,
    external_refs: threadRecord.external_refs,
    task_ids: threadRecord.task_ids,
    created_by: threadRecord.created_by,
    created_at: threadRecord.created_at,
    updated_at: threadRecord.updated_at,
    closed_at: threadRecord.closed_at,
  });
}

function threadEffectiveStatusFromJson(value: string, path: string): ThreadEffectiveStatus {
  if (!THREAD_EFFECTIVE_STATUS_SET.has(value)) {
    throw new Error(`${path}: must be one of ${THREAD_EFFECTIVE_STATUS_VALUES.join(", ")}`);
  }
  return value as ThreadEffectiveStatus;
}

function optionalThreadAttentionReason(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): ThreadAttentionReason | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string" || !THREAD_ATTENTION_REASON_SET.has(value)) {
    throw new Error(`${path}: must be one of ${THREAD_ATTENTION_REASON_VALUES.join(", ")}`);
  }
  return value as ThreadAttentionReason;
}

function threadBoardColumnFromJson(value: string, path: string): ThreadBoardColumn {
  if (!THREAD_BOARD_COLUMN_SET.has(value)) {
    throw new Error(`${path}: must be one of ${THREAD_BOARD_COLUMN_VALUES.join(", ")}`);
  }
  return value as ThreadBoardColumn;
}

function threadCurrentSeatFromJson(value: string, path: string): ThreadCurrentSeat {
  if (!THREAD_CURRENT_SEAT_SET.has(value)) {
    throw new Error(`${path}: must be one of ${THREAD_CURRENT_SEAT_VALUES.join(", ")}`);
  }
  return value as ThreadCurrentSeat;
}

function validateThreadViewStatusCoupling(view: ThreadView, path: string): void {
  if (view.effectiveStatus === "needs_attention" && view.attentionReason === undefined) {
    throw new Error(`${path}.attentionReason: is required when effectiveStatus is needs_attention`);
  }
  if (view.effectiveStatus !== "needs_attention" && view.attentionReason !== undefined) {
    throw new Error(
      `${path}.attentionReason: must be absent unless effectiveStatus is needs_attention`,
    );
  }
  if (view.boardColumn !== boardColumnForEffectiveStatus(view.effectiveStatus)) {
    throw new Error(`${path}.boardColumn: must match effectiveStatus`);
  }
  const expectedSeat =
    view.effectiveStatus === "needs_attention" || view.status === "needs_review"
      ? "human"
      : "agent";
  if (view.currentSeat !== expectedSeat) {
    throw new Error(`${path}.currentSeat: must match effectiveStatus and status`);
  }
  validatePendingApprovalCount(view.pendingApprovalCount);
}

function boardColumnForEffectiveStatus(status: ThreadEffectiveStatus): ThreadBoardColumn {
  if (status === "needs_attention") return "needs_me";
  if (status === "needs_review") return "review";
  if (status === "merged" || status === "closed") return "done";
  return "running";
}

function pendingApprovalCountFromJson(value: unknown, path: string): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path}: must be a non-negative safe integer`);
  }
  return value;
}

function validatePendingApprovalCount(value: number): number {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error("threadView.pendingApprovalCount: must be a non-negative safe integer");
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
