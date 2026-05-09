import {
  MAX_THREAD_EXTERNAL_REFS,
  MAX_THREAD_TASK_IDS,
  validateThreadExternalRefBudget,
  validateThreadSpecContentBudget,
  validateThreadTitleBudget,
} from "./budgets.ts";
import { canonicalJSON, type JsonValue } from "./canonical-json.ts";
import {
  asSignerIdentity,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  type IdempotencyKey,
  isIdempotencyKey,
  isSignerIdentity,
  isTaskId,
  isThreadId,
  isThreadSpecRevisionId,
  type ReceiptSnapshot,
  type ReceiptValidationError,
  type SignerIdentity,
  type TaskId,
  type ThreadId,
  type ThreadSpecRevisionId,
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
import { asSha256Hex, isSha256Hex, type Sha256Hex, sha256Hex } from "./sha256.ts";

export const THREAD_STATUS_VALUES = [
  "open",
  "in_progress",
  "needs_review",
  "merged",
  "closed",
] as const;

export type ThreadStatus = (typeof THREAD_STATUS_VALUES)[number];

export interface ThreadExternalRefs {
  readonly sourceUrls: readonly string[];
  readonly entityIds: readonly string[];
}

export interface ThreadSpecRevision {
  readonly revisionId: ThreadSpecRevisionId;
  readonly threadId: ThreadId;
  readonly baseRevisionId?: ThreadSpecRevisionId | undefined;
  readonly content: JsonValue;
  readonly contentHash: Sha256Hex;
  readonly authoredBy: SignerIdentity;
  readonly authoredAt: Date;
}

export interface Thread {
  readonly id: ThreadId;
  readonly title: string;
  readonly status: ThreadStatus;
  readonly spec: ThreadSpecRevision;
  readonly externalRefs: ThreadExternalRefs;
  readonly taskIds: readonly TaskId[];
  readonly createdBy: SignerIdentity;
  readonly createdAt: Date;
  readonly updatedAt: Date;
  readonly closedAt?: Date | undefined;
}

export interface ThreadCreatedAuditPayload {
  readonly threadId: ThreadId;
  readonly title: string;
  readonly createdBy: SignerIdentity;
  readonly createdAt: Date;
  readonly externalRefs: ThreadExternalRefs;
}

export interface ThreadSpecEditedAuditPayload {
  readonly threadId: ThreadId;
  readonly revisionId: ThreadSpecRevisionId;
  readonly baseRevisionId?: ThreadSpecRevisionId | undefined;
  readonly content: JsonValue;
  readonly contentHash: Sha256Hex;
  readonly authoredBy: SignerIdentity;
  readonly authoredAt: Date;
}

export interface ThreadStatusChangedAuditPayload {
  readonly threadId: ThreadId;
  readonly fromStatus: ThreadStatus;
  readonly toStatus: ThreadStatus;
  readonly changedBy: SignerIdentity;
  readonly changedAt: Date;
}

export type ThreadAuditPayload =
  | ThreadCreatedAuditPayload
  | ThreadSpecEditedAuditPayload
  | ThreadStatusChangedAuditPayload;

export type ThreadAuditEventKind =
  | "thread_created"
  | "thread_spec_edited"
  | "thread_status_changed";

export type ThreadCommandKind = "thread.create" | "thread.spec.edit" | "thread.status.change";

interface ThreadCommandCommon {
  readonly kind: ThreadCommandKind;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ThreadCreateCommand extends ThreadCommandCommon {
  readonly kind: "thread.create";
  readonly threadId: ThreadId;
  readonly title: string;
  readonly createdBy: SignerIdentity;
  readonly createdAt: Date;
  readonly externalRefs: ThreadExternalRefs;
  readonly content: JsonValue;
}

export interface ThreadSpecEditCommand extends ThreadCommandCommon {
  readonly kind: "thread.spec.edit";
  readonly threadId: ThreadId;
  readonly revisionId: ThreadSpecRevisionId;
  readonly baseRevisionId?: ThreadSpecRevisionId | undefined;
  readonly content: JsonValue;
  readonly contentHash: Sha256Hex;
  readonly authoredBy: SignerIdentity;
  readonly authoredAt: Date;
}

export interface ThreadStatusChangeCommand extends ThreadCommandCommon {
  readonly kind: "thread.status.change";
  readonly threadId: ThreadId;
  readonly fromStatus: ThreadStatus;
  readonly toStatus: ThreadStatus;
  readonly changedBy: SignerIdentity;
  readonly changedAt: Date;
}

export type ThreadCommand = ThreadCreateCommand | ThreadSpecEditCommand | ThreadStatusChangeCommand;

export type ThreadValidationError = ReceiptValidationError;
export type ThreadValidationResult = { ok: true } | { ok: false; errors: ThreadValidationError[] };

export type ThreadStatusFoldEvent =
  | {
      readonly kind: "thread_created";
      readonly threadId: ThreadId;
      readonly status?: "open" | undefined;
    }
  | (ThreadStatusChangedAuditPayload & { readonly kind: "thread_status_changed" });

export interface ThreadForeignKeyValidationInput {
  readonly existingThreadIds: ReadonlySet<ThreadId>;
  readonly specEdits: readonly ThreadSpecEditedAuditPayload[];
  readonly statusChanges: readonly ThreadStatusChangedAuditPayload[];
  readonly receipts: readonly ReceiptSnapshot[];
  readonly inboxThreadId?: ThreadId | undefined;
}

const THREAD_KEYS_TUPLE = [
  "id",
  "title",
  "status",
  "spec",
  "externalRefs",
  "taskIds",
  "createdBy",
  "createdAt",
  "updatedAt",
  "closedAt",
] as const satisfies readonly (keyof Thread)[];
const THREAD_KEYS: ReadonlySet<string> = new Set<string>(THREAD_KEYS_TUPLE);

const THREAD_SPEC_REVISION_KEYS_TUPLE = [
  "revisionId",
  "threadId",
  "baseRevisionId",
  "content",
  "contentHash",
  "authoredBy",
  "authoredAt",
] as const satisfies readonly (keyof ThreadSpecRevision)[];
const THREAD_SPEC_REVISION_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_SPEC_REVISION_KEYS_TUPLE,
);

const THREAD_EXTERNAL_REFS_KEYS_TUPLE = [
  "sourceUrls",
  "entityIds",
] as const satisfies readonly (keyof ThreadExternalRefs)[];
const THREAD_EXTERNAL_REFS_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_EXTERNAL_REFS_KEYS_TUPLE,
);

const THREAD_CREATED_AUDIT_PAYLOAD_KEYS_TUPLE = [
  "threadId",
  "title",
  "createdBy",
  "createdAt",
  "externalRefs",
] as const satisfies readonly (keyof ThreadCreatedAuditPayload)[];
const THREAD_CREATED_AUDIT_PAYLOAD_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_CREATED_AUDIT_PAYLOAD_KEYS_TUPLE,
);

const THREAD_SPEC_EDITED_AUDIT_PAYLOAD_KEYS_TUPLE = [
  "threadId",
  "revisionId",
  "baseRevisionId",
  "content",
  "contentHash",
  "authoredBy",
  "authoredAt",
] as const satisfies readonly (keyof ThreadSpecEditedAuditPayload)[];
const THREAD_SPEC_EDITED_AUDIT_PAYLOAD_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_SPEC_EDITED_AUDIT_PAYLOAD_KEYS_TUPLE,
);

const THREAD_STATUS_CHANGED_AUDIT_PAYLOAD_KEYS_TUPLE = [
  "threadId",
  "fromStatus",
  "toStatus",
  "changedBy",
  "changedAt",
] as const satisfies readonly (keyof ThreadStatusChangedAuditPayload)[];
const THREAD_STATUS_CHANGED_AUDIT_PAYLOAD_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_STATUS_CHANGED_AUDIT_PAYLOAD_KEYS_TUPLE,
);

const THREAD_CREATE_COMMAND_KEYS_TUPLE = [
  "kind",
  "idempotencyKey",
  "threadId",
  "title",
  "createdBy",
  "createdAt",
  "externalRefs",
  "content",
] as const satisfies readonly (keyof ThreadCreateCommand)[];
const THREAD_CREATE_COMMAND_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_CREATE_COMMAND_KEYS_TUPLE,
);

const THREAD_SPEC_EDIT_COMMAND_KEYS_TUPLE = [
  "kind",
  "idempotencyKey",
  "threadId",
  "revisionId",
  "baseRevisionId",
  "content",
  "contentHash",
  "authoredBy",
  "authoredAt",
] as const satisfies readonly (keyof ThreadSpecEditCommand)[];
const THREAD_SPEC_EDIT_COMMAND_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_SPEC_EDIT_COMMAND_KEYS_TUPLE,
);

const THREAD_STATUS_CHANGE_COMMAND_KEYS_TUPLE = [
  "kind",
  "idempotencyKey",
  "threadId",
  "fromStatus",
  "toStatus",
  "changedBy",
  "changedAt",
] as const satisfies readonly (keyof ThreadStatusChangeCommand)[];
const THREAD_STATUS_CHANGE_COMMAND_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_STATUS_CHANGE_COMMAND_KEYS_TUPLE,
);

type ThreadWire = Readonly<
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
    | "closed_at",
    unknown
  >
>;

type ThreadSpecRevisionWire = Readonly<
  Record<
    | "revision_id"
    | "thread_id"
    | "base_revision_id"
    | "content"
    | "content_hash"
    | "authored_by"
    | "authored_at",
    unknown
  >
>;

type ThreadExternalRefsWire = Readonly<Record<"source_urls" | "entity_ids", unknown>>;

const THREAD_WIRE_KEYS_TUPLE = [
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
] as const satisfies readonly (keyof ThreadWire)[];
const THREAD_WIRE_KEYS: ReadonlySet<string> = new Set<string>(THREAD_WIRE_KEYS_TUPLE);

const THREAD_SPEC_REVISION_WIRE_KEYS_TUPLE = [
  "revision_id",
  "thread_id",
  "base_revision_id",
  "content",
  "content_hash",
  "authored_by",
  "authored_at",
] as const satisfies readonly (keyof ThreadSpecRevisionWire)[];
const THREAD_SPEC_REVISION_WIRE_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_SPEC_REVISION_WIRE_KEYS_TUPLE,
);

const THREAD_EXTERNAL_REFS_WIRE_KEYS_TUPLE = [
  "source_urls",
  "entity_ids",
] as const satisfies readonly (keyof ThreadExternalRefsWire)[];
const THREAD_EXTERNAL_REFS_WIRE_KEYS: ReadonlySet<string> = new Set<string>(
  THREAD_EXTERNAL_REFS_WIRE_KEYS_TUPLE,
);

const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);
const TEXT_ENCODER = new TextEncoder();

export function threadToJson(thread: Thread): string {
  const validation = validateThread(thread);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return canonicalJSON(threadToJsonValue(thread));
}

export function threadFromJsonValue(value: unknown): Thread {
  const record = requireRecord(value, "thread");
  assertKnownKeys(record, "thread", THREAD_WIRE_KEYS);
  const closedAt = optionalDateFromJson(record, "closed_at", "thread");
  const thread: Thread = {
    id: asThreadIdAt(requiredStringFromJson(record, "thread_id", "thread"), "thread.thread_id"),
    title: requiredStringFromJson(record, "title", "thread"),
    status: threadStatusFromJson(
      requiredStringFromJson(record, "status", "thread"),
      "thread.status",
    ),
    spec: threadSpecRevisionFromJsonValue(requiredFieldFromJson(record, "spec", "thread"), [
      "thread",
      "spec",
    ]),
    externalRefs: threadExternalRefsFromJsonValue(
      requiredFieldFromJson(record, "external_refs", "thread"),
      ["thread", "external_refs"],
    ),
    taskIds: requiredStringArrayFromJson(record, "task_ids", "thread").map((taskId, index) =>
      asTaskIdAt(taskId, `thread.task_ids.${index}`),
    ),
    createdBy: asSignerIdentityAt(
      requiredStringFromJson(record, "created_by", "thread"),
      "thread.created_by",
    ),
    createdAt: requiredDateFromJson(record, "created_at", "thread"),
    updatedAt: requiredDateFromJson(record, "updated_at", "thread"),
    ...(closedAt === undefined ? {} : { closedAt }),
  };
  const validation = validateThread(thread);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return thread;
}

export function threadFromJson(json: string): Thread {
  return threadFromJsonValue(JSON.parse(json));
}

export function threadToJsonValue(thread: Thread): Record<string, unknown> {
  return omitUndefined({
    thread_id: thread.id,
    title: thread.title,
    status: thread.status,
    spec: threadSpecRevisionToJsonValue(thread.spec),
    external_refs: threadExternalRefsToJsonValue(thread.externalRefs),
    task_ids: [...thread.taskIds],
    created_by: thread.createdBy,
    created_at: thread.createdAt.toISOString(),
    updated_at: thread.updatedAt.toISOString(),
    closed_at: thread.closedAt?.toISOString(),
  });
}

export function threadSpecRevisionToJsonValue(
  revision: ThreadSpecRevision,
): Record<string, unknown> {
  return omitUndefined({
    revision_id: revision.revisionId,
    thread_id: revision.threadId,
    base_revision_id: revision.baseRevisionId,
    content: revision.content,
    content_hash: revision.contentHash,
    authored_by: revision.authoredBy,
    authored_at: revision.authoredAt.toISOString(),
  });
}

export function threadSpecRevisionFromJsonValue(
  value: unknown,
  pathSegments: readonly string[] = ["threadSpecRevision"],
): ThreadSpecRevision {
  const path = pathSegments.join(".");
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, THREAD_SPEC_REVISION_WIRE_KEYS);
  const baseRevisionId = optionalStringFromJson(record, "base_revision_id", path);
  const revision: ThreadSpecRevision = {
    revisionId: asThreadSpecRevisionIdAt(
      requiredStringFromJson(record, "revision_id", path),
      `${path}.revision_id`,
    ),
    threadId: asThreadIdAt(requiredStringFromJson(record, "thread_id", path), `${path}.thread_id`),
    ...(baseRevisionId === undefined
      ? {}
      : {
          baseRevisionId: asThreadSpecRevisionIdAt(baseRevisionId, `${path}.base_revision_id`),
        }),
    content: jsonValueFromUnknown(
      requiredFieldFromJson(record, "content", path),
      `${path}.content`,
    ),
    contentHash: asSha256HexAt(
      requiredStringFromJson(record, "content_hash", path),
      `${path}.content_hash`,
    ),
    authoredBy: asSignerIdentityAt(
      requiredStringFromJson(record, "authored_by", path),
      `${path}.authored_by`,
    ),
    authoredAt: requiredDateFromJson(record, "authored_at", path),
  };
  const validation = validateThreadSpecRevision(revision);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return revision;
}

export function threadExternalRefsToJsonValue(refs: ThreadExternalRefs): Record<string, unknown> {
  return {
    source_urls: [...refs.sourceUrls],
    entity_ids: [...refs.entityIds],
  };
}

export function threadExternalRefsFromJsonValue(
  value: unknown,
  pathSegments: readonly string[] = ["threadExternalRefs"],
): ThreadExternalRefs {
  const path = pathSegments.join(".");
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, THREAD_EXTERNAL_REFS_WIRE_KEYS);
  const refs: ThreadExternalRefs = {
    sourceUrls: requiredStringArrayFromJson(record, "source_urls", path),
    entityIds: requiredStringArrayFromJson(record, "entity_ids", path),
  };
  const validation = validateThreadExternalRefs(refs, `/${pathSegments.join("/")}`);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return refs;
}

export function threadAuditPayloadToJsonValue(
  kind: ThreadAuditEventKind,
  payload: ThreadAuditPayload,
): Record<string, unknown> {
  if (kind === "thread_created") {
    const created = payload as ThreadCreatedAuditPayload;
    return {
      threadId: created.threadId,
      title: created.title,
      createdBy: created.createdBy,
      createdAt: created.createdAt.toISOString(),
      externalRefs: threadExternalRefsToCamelJsonValue(created.externalRefs),
    };
  }
  if (kind === "thread_spec_edited") {
    const edited = payload as ThreadSpecEditedAuditPayload;
    return omitUndefined({
      threadId: edited.threadId,
      revisionId: edited.revisionId,
      baseRevisionId: edited.baseRevisionId,
      content: edited.content,
      contentHash: edited.contentHash,
      authoredBy: edited.authoredBy,
      authoredAt: edited.authoredAt.toISOString(),
    });
  }
  if (kind === "thread_status_changed") {
    const changed = payload as ThreadStatusChangedAuditPayload;
    return {
      threadId: changed.threadId,
      fromStatus: changed.fromStatus,
      toStatus: changed.toStatus,
      changedBy: changed.changedBy,
      changedAt: changed.changedAt.toISOString(),
    };
  }
  throw new Error(unknownThreadAuditEventKindMessage(kind));
}

export function threadAuditPayloadFromJsonValue(
  kind: ThreadAuditEventKind,
  value: unknown,
): ThreadAuditPayload {
  if (kind === "thread_created") return threadCreatedAuditPayloadFromJsonValue(value);
  if (kind === "thread_spec_edited") return threadSpecEditedAuditPayloadFromJsonValue(value);
  if (kind === "thread_status_changed") return threadStatusChangedAuditPayloadFromJsonValue(value);
  throw new Error(unknownThreadAuditEventKindMessage(kind));
}

export function threadAuditPayloadToBytes(
  kind: ThreadAuditEventKind,
  payload: ThreadAuditPayload,
): Uint8Array {
  const validation = validateThreadAuditPayloadForKind(kind, payload);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return TEXT_ENCODER.encode(canonicalJSON(threadAuditPayloadToJsonValue(kind, payload)));
}

export function validateThreadAuditPayloadForKind(
  kind: ThreadAuditEventKind,
  payload: unknown,
): ThreadValidationResult {
  if (kind === "thread_created") return validateThreadCreatedAuditPayload(payload);
  if (kind === "thread_spec_edited") return validateThreadSpecEditedAuditPayload(payload);
  if (kind === "thread_status_changed") return validateThreadStatusChangedAuditPayload(payload);
  throw new Error(unknownThreadAuditEventKindMessage(kind));
}

function unknownThreadAuditEventKindMessage(kind: unknown): string {
  return `unknown ThreadAuditEventKind: ${String(kind)}`;
}

export function validateThread(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadSpecRevision(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadSpecRevisionValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadExternalRefs(input: unknown, path = ""): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadExternalRefsValue(input, path, errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadCreatedAuditPayload(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadCreatedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadSpecEditedAuditPayload(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadSpecEditedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadStatusChangedAuditPayload(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  validateThreadStatusChangedAuditPayloadValue(input, "", errors);
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadCommand(input: unknown): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  if (!isRecord(input)) {
    addError(errors, "", "must be an object");
    return { ok: false, errors };
  }
  const kind = recordValue(input, "kind");
  if (kind === "thread.create") {
    validateKnownKeys(input, "", THREAD_CREATE_COMMAND_KEYS, errors);
    validateThreadCreateCommandValue(input, "", errors);
  } else if (kind === "thread.spec.edit") {
    validateKnownKeys(input, "", THREAD_SPEC_EDIT_COMMAND_KEYS, errors);
    validateThreadSpecEditCommandValue(input, "", errors);
  } else if (kind === "thread.status.change") {
    validateKnownKeys(input, "", THREAD_STATUS_CHANGE_COMMAND_KEYS, errors);
    validateThreadStatusChangeCommandValue(input, "", errors);
  } else {
    addError(errors, "/kind", "must be a valid thread command kind");
  }
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadSpecRevisionChain(
  events: readonly ThreadSpecEditedAuditPayload[],
): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  const priorByThreadId = new Map<string, ThreadSpecRevisionId>();
  const seenRevisionIds = new Set<string>();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (event === undefined) continue;
    const eventPath = pointer("", String(i));
    const validation = validateThreadSpecEditedAuditPayload(event);
    if (!validation.ok) {
      errors.push(...prefixErrors(validation.errors, eventPath));
      continue;
    }
    if (event.baseRevisionId === event.revisionId) {
      addError(errors, pointer(eventPath, "baseRevisionId"), "must not equal revisionId");
    }
    if (seenRevisionIds.has(event.revisionId)) {
      addError(errors, pointer(eventPath, "revisionId"), "duplicate revisionId in chain");
    } else {
      seenRevisionIds.add(event.revisionId);
    }
    const threadKey = event.threadId as string;
    const priorRevisionId = priorByThreadId.get(threadKey);
    const baseRevisionId = event.baseRevisionId;
    if (priorRevisionId === undefined) {
      if (baseRevisionId !== undefined) {
        addError(errors, pointer(eventPath, "baseRevisionId"), "must be absent for initial edit");
      }
    } else if (baseRevisionId !== priorRevisionId) {
      addError(errors, pointer(eventPath, "baseRevisionId"), "must match prior revisionId");
    }
    priorByThreadId.set(threadKey, event.revisionId);
  }
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadStatusFold(
  events: readonly ThreadStatusFoldEvent[],
): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  const statusByThreadId = new Map<string, ThreadStatus>();
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (event === undefined) continue;
    const eventPath = pointer("", String(i));
    const eventKind = (event as { readonly kind?: unknown }).kind;
    if (eventKind === "thread_created") {
      const created = event as Extract<ThreadStatusFoldEvent, { readonly kind: "thread_created" }>;
      if (!isThreadId(created.threadId)) {
        addError(errors, pointer(eventPath, "threadId"), "must be an uppercase ULID ThreadId");
        continue;
      }
      const initialStatus = created.status ?? "open";
      if (initialStatus !== "open") {
        addError(errors, pointer(eventPath, "status"), "must be open");
        continue;
      }
      statusByThreadId.set(created.threadId as string, "open");
      continue;
    }
    if (eventKind !== "thread_status_changed") {
      addError(
        errors,
        pointer(eventPath, "kind"),
        `unexpected event kind in status fold: ${String(eventKind)}`,
      );
      continue;
    }

    const changed = event as ThreadStatusChangedAuditPayload & {
      readonly kind: "thread_status_changed";
    };
    const errorCountBeforeEvent = errors.length;
    validateThreadStatusChangedFields(
      changed as unknown as Readonly<Record<string, unknown>>,
      eventPath,
      errors,
    );
    if (errors.length > errorCountBeforeEvent) continue;
    const threadKey = changed.threadId as string;
    const priorStatus = statusByThreadId.get(threadKey);
    if (priorStatus === undefined) {
      addError(errors, pointer(eventPath, "threadId"), "must reference an existing thread");
      continue;
    }
    if (isTerminalThreadStatus(priorStatus)) {
      addError(errors, pointer(eventPath, "fromStatus"), "must not transition out of terminal");
      continue;
    }
    if (changed.fromStatus !== priorStatus) {
      addError(errors, pointer(eventPath, "fromStatus"), "must equal prior folded status");
      continue;
    }
    statusByThreadId.set(threadKey, changed.toStatus);
  }
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadForeignKeys(
  input: ThreadForeignKeyValidationInput,
): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  for (let i = 0; i < input.specEdits.length; i += 1) {
    const event = input.specEdits[i];
    if (event !== undefined && !input.existingThreadIds.has(event.threadId)) {
      addError(errors, `/specEdits/${i}/threadId`, "must reference an existing thread");
    }
  }
  for (let i = 0; i < input.statusChanges.length; i += 1) {
    const event = input.statusChanges[i];
    if (event !== undefined && !input.existingThreadIds.has(event.threadId)) {
      addError(errors, `/statusChanges/${i}/threadId`, "must reference an existing thread");
    }
  }
  for (let i = 0; i < input.receipts.length; i += 1) {
    const receipt = input.receipts[i];
    if (receipt?.schemaVersion !== 2 || receipt.threadId === undefined) continue;
    if (
      !input.existingThreadIds.has(receipt.threadId) &&
      receipt.threadId !== input.inboxThreadId
    ) {
      addError(errors, `/receipts/${i}/threadId`, "must reference an existing thread");
    }
  }
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function validateThreadReceiptIndex(
  thread: Thread,
  receipts: readonly ReceiptSnapshot[],
): ThreadValidationResult {
  const errors: ThreadValidationError[] = [];
  const taskIds = new Set<string>();
  for (const receipt of receipts) {
    if (receipt.schemaVersion === 2 && receipt.threadId === thread.id) {
      taskIds.add(receipt.taskId as string);
    }
  }
  const projected = [...taskIds];
  const actual = thread.taskIds.map((taskId) => taskId as string);
  if (new Set(actual).size !== actual.length) {
    addError(errors, "/taskIds", "must not contain duplicates");
  }
  if (projected.length !== actual.length || projected.some((taskId) => !actual.includes(taskId))) {
    addError(errors, "/taskIds", "must equal receipt taskIds for this thread");
  }
  return errors.length === 0 ? { ok: true } : { ok: false, errors };
}

export function threadSpecContentHash(content: JsonValue): Sha256Hex {
  return sha256Hex(canonicalJSON(content));
}

function validateThreadValue(value: unknown, path: string, errors: ThreadValidationError[]): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_KEYS, errors);
  validateRequired(value, "id", path, errors, validateThreadIdValue);
  validateRequired(value, "title", path, errors, validateThreadTitleValue);
  validateRequired(value, "status", path, errors, validateThreadStatusValue);
  validateRequired(value, "spec", path, errors, validateThreadSpecRevisionValue);
  validateRequired(value, "externalRefs", path, errors, validateThreadExternalRefsValue);
  validateRequired(value, "taskIds", path, errors, validateThreadTaskIdsValue);
  validateRequired(value, "createdBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "createdAt", path, errors, validateDateValue);
  validateRequired(value, "updatedAt", path, errors, validateDateValue);
  validateOptional(value, "closedAt", path, errors, validateDateValue);

  const id = recordValue(value, "id");
  const spec = recordValue(value, "spec");
  if (typeof id === "string" && isRecord(spec) && recordValue(spec, "threadId") !== id) {
    addError(errors, pointer(pointer(path, "spec"), "threadId"), "must match thread id");
  }
  const closedAt = recordValue(value, "closedAt");
  const status = recordValue(value, "status");
  if (closedAt !== undefined && !isTerminalThreadStatus(status)) {
    addError(errors, pointer(path, "closedAt"), "must be absent unless status is terminal");
  }
}

function validateThreadSpecRevisionValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_SPEC_REVISION_KEYS, errors);
  validateRequired(value, "revisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateOptional(value, "baseRevisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateRequired(value, "content", path, errors, validateThreadSpecContentValue);
  validateRequired(value, "contentHash", path, errors, validateSha256HexValue);
  validateRequired(value, "authoredBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "authoredAt", path, errors, validateDateValue);
  const content = recordValue(value, "content");
  const contentHash = recordValue(value, "contentHash");
  if (isSha256Hex(contentHash)) {
    const derived = deriveThreadSpecContentHash(content);
    if (derived.ok && derived.hash !== contentHash) {
      addError(errors, pointer(path, "contentHash"), "must match sha256(canonical(content))");
    }
  }
}

function validateThreadExternalRefsValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_EXTERNAL_REFS_KEYS, errors);
  validateRequired(value, "sourceUrls", path, errors, validateExternalRefArrayValue);
  validateRequired(value, "entityIds", path, errors, validateExternalRefArrayValue);
}

function validateThreadCreatedAuditPayloadValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_CREATED_AUDIT_PAYLOAD_KEYS, errors);
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateRequired(value, "title", path, errors, validateThreadTitleValue);
  validateRequired(value, "createdBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "createdAt", path, errors, validateDateValue);
  validateRequired(value, "externalRefs", path, errors, validateThreadExternalRefsValue);
}

function validateThreadSpecEditedAuditPayloadValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_SPEC_EDITED_AUDIT_PAYLOAD_KEYS, errors);
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateRequired(value, "revisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateOptional(value, "baseRevisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateRequired(value, "content", path, errors, validateThreadSpecContentValue);
  validateRequired(value, "contentHash", path, errors, validateSha256HexValue);
  validateRequired(value, "authoredBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "authoredAt", path, errors, validateDateValue);
  const content = recordValue(value, "content");
  const contentHash = recordValue(value, "contentHash");
  if (isSha256Hex(contentHash)) {
    const derived = deriveThreadSpecContentHash(content);
    if (derived.ok && derived.hash !== contentHash) {
      addError(errors, pointer(path, "contentHash"), "must match sha256(canonical(content))");
    }
  }
}

function validateThreadStatusChangedAuditPayloadValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isRecord(value)) {
    addError(errors, path, "must be an object");
    return;
  }
  validateKnownKeys(value, path, THREAD_STATUS_CHANGED_AUDIT_PAYLOAD_KEYS, errors);
  validateThreadStatusChangedFields(value, path, errors);
}

function validateThreadStatusChangedFields(
  value: Readonly<Record<string, unknown>>,
  path: string,
  errors: ThreadValidationError[],
): void {
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateRequired(value, "fromStatus", path, errors, validateThreadStatusValue);
  validateRequired(value, "toStatus", path, errors, validateThreadStatusValue);
  validateRequired(value, "changedBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "changedAt", path, errors, validateDateValue);
  const fromStatus = recordValue(value, "fromStatus");
  const toStatus = recordValue(value, "toStatus");
  if (isTerminalThreadStatus(fromStatus)) {
    addError(errors, pointer(path, "fromStatus"), "must not be terminal");
  }
  if (
    isThreadStatus(fromStatus) &&
    isThreadStatus(toStatus) &&
    !isAllowedThreadStatusTransition(fromStatus, toStatus)
  ) {
    addError(errors, pointer(path, "toStatus"), `transition not allowed from ${fromStatus}`);
  }
}

function threadExternalRefsToCamelJsonValue(refs: ThreadExternalRefs): Record<string, unknown> {
  return {
    sourceUrls: [...refs.sourceUrls],
    entityIds: [...refs.entityIds],
  };
}

function threadExternalRefsFromCamelJsonValue(value: unknown, path: string): ThreadExternalRefs {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, THREAD_EXTERNAL_REFS_KEYS);
  const refs: ThreadExternalRefs = {
    sourceUrls: requiredStringArrayFromJson(record, "sourceUrls", path),
    entityIds: requiredStringArrayFromJson(record, "entityIds", path),
  };
  const validation = validateThreadExternalRefs(refs, pathToPointer(path));
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return refs;
}

function threadCreatedAuditPayloadFromJsonValue(value: unknown): ThreadCreatedAuditPayload {
  const record = requireRecord(value, "threadCreatedAuditPayload");
  assertKnownKeys(record, "threadCreatedAuditPayload", THREAD_CREATED_AUDIT_PAYLOAD_KEYS);
  const payload: ThreadCreatedAuditPayload = {
    threadId: asThreadIdAt(
      requiredStringFromJson(record, "threadId", "threadCreatedAuditPayload"),
      "threadCreatedAuditPayload.threadId",
    ),
    title: requiredStringFromJson(record, "title", "threadCreatedAuditPayload"),
    createdBy: asSignerIdentityAt(
      requiredStringFromJson(record, "createdBy", "threadCreatedAuditPayload"),
      "threadCreatedAuditPayload.createdBy",
    ),
    createdAt: requiredDateFromJson(record, "createdAt", "threadCreatedAuditPayload"),
    externalRefs: threadExternalRefsFromCamelJsonValue(
      requiredFieldFromJson(record, "externalRefs", "threadCreatedAuditPayload"),
      "threadCreatedAuditPayload.externalRefs",
    ),
  };
  const validation = validateThreadCreatedAuditPayload(payload);
  if (!validation.ok) throw new Error(formatValidationErrors(validation.errors));
  return payload;
}

function threadSpecEditedAuditPayloadFromJsonValue(value: unknown): ThreadSpecEditedAuditPayload {
  const record = requireRecord(value, "threadSpecEditedAuditPayload");
  assertKnownKeys(record, "threadSpecEditedAuditPayload", THREAD_SPEC_EDITED_AUDIT_PAYLOAD_KEYS);
  const baseRevisionId = optionalStringFromJson(
    record,
    "baseRevisionId",
    "threadSpecEditedAuditPayload",
  );
  const payload: ThreadSpecEditedAuditPayload = {
    threadId: asThreadIdAt(
      requiredStringFromJson(record, "threadId", "threadSpecEditedAuditPayload"),
      "threadSpecEditedAuditPayload.threadId",
    ),
    revisionId: asThreadSpecRevisionIdAt(
      requiredStringFromJson(record, "revisionId", "threadSpecEditedAuditPayload"),
      "threadSpecEditedAuditPayload.revisionId",
    ),
    ...(baseRevisionId === undefined
      ? {}
      : {
          baseRevisionId: asThreadSpecRevisionIdAt(
            baseRevisionId,
            "threadSpecEditedAuditPayload.baseRevisionId",
          ),
        }),
    content: jsonValueFromUnknown(
      requiredFieldFromJson(record, "content", "threadSpecEditedAuditPayload"),
      "threadSpecEditedAuditPayload.content",
    ),
    contentHash: asSha256HexAt(
      requiredStringFromJson(record, "contentHash", "threadSpecEditedAuditPayload"),
      "threadSpecEditedAuditPayload.contentHash",
    ),
    authoredBy: asSignerIdentityAt(
      requiredStringFromJson(record, "authoredBy", "threadSpecEditedAuditPayload"),
      "threadSpecEditedAuditPayload.authoredBy",
    ),
    authoredAt: requiredDateFromJson(record, "authoredAt", "threadSpecEditedAuditPayload"),
  };
  const validation = validateThreadSpecEditedAuditPayload(payload);
  if (!validation.ok) throw new Error(formatValidationErrors(validation.errors));
  return payload;
}

function threadStatusChangedAuditPayloadFromJsonValue(
  value: unknown,
): ThreadStatusChangedAuditPayload {
  const record = requireRecord(value, "threadStatusChangedAuditPayload");
  assertKnownKeys(
    record,
    "threadStatusChangedAuditPayload",
    THREAD_STATUS_CHANGED_AUDIT_PAYLOAD_KEYS,
  );
  const payload: ThreadStatusChangedAuditPayload = {
    threadId: asThreadIdAt(
      requiredStringFromJson(record, "threadId", "threadStatusChangedAuditPayload"),
      "threadStatusChangedAuditPayload.threadId",
    ),
    fromStatus: threadStatusFromJson(
      requiredStringFromJson(record, "fromStatus", "threadStatusChangedAuditPayload"),
      "threadStatusChangedAuditPayload.fromStatus",
    ),
    toStatus: threadStatusFromJson(
      requiredStringFromJson(record, "toStatus", "threadStatusChangedAuditPayload"),
      "threadStatusChangedAuditPayload.toStatus",
    ),
    changedBy: asSignerIdentityAt(
      requiredStringFromJson(record, "changedBy", "threadStatusChangedAuditPayload"),
      "threadStatusChangedAuditPayload.changedBy",
    ),
    changedAt: requiredDateFromJson(record, "changedAt", "threadStatusChangedAuditPayload"),
  };
  const validation = validateThreadStatusChangedAuditPayload(payload);
  if (!validation.ok) throw new Error(formatValidationErrors(validation.errors));
  return payload;
}

function validateThreadCreateCommandValue(
  value: Readonly<Record<string, unknown>>,
  path: string,
  errors: ThreadValidationError[],
): void {
  validateRequired(value, "idempotencyKey", path, errors, validateIdempotencyKeyValue);
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateRequired(value, "title", path, errors, validateThreadTitleValue);
  validateRequired(value, "createdBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "createdAt", path, errors, validateDateValue);
  validateRequired(value, "externalRefs", path, errors, validateThreadExternalRefsValue);
  validateRequired(value, "content", path, errors, validateThreadSpecContentValue);
}

function validateThreadSpecEditCommandValue(
  value: Readonly<Record<string, unknown>>,
  path: string,
  errors: ThreadValidationError[],
): void {
  validateRequired(value, "idempotencyKey", path, errors, validateIdempotencyKeyValue);
  validateRequired(value, "threadId", path, errors, validateThreadIdValue);
  validateRequired(value, "revisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateOptional(value, "baseRevisionId", path, errors, validateThreadSpecRevisionIdValue);
  validateRequired(value, "content", path, errors, validateThreadSpecContentValue);
  validateRequired(value, "contentHash", path, errors, validateSha256HexValue);
  validateRequired(value, "authoredBy", path, errors, validateSignerIdentityValue);
  validateRequired(value, "authoredAt", path, errors, validateDateValue);
  const content = recordValue(value, "content");
  const contentHash = recordValue(value, "contentHash");
  if (isSha256Hex(contentHash)) {
    const derived = deriveThreadSpecContentHash(content);
    if (derived.ok && derived.hash !== contentHash) {
      addError(errors, pointer(path, "contentHash"), "must match sha256(canonical(content))");
    }
  }
}

function validateThreadStatusChangeCommandValue(
  value: Readonly<Record<string, unknown>>,
  path: string,
  errors: ThreadValidationError[],
): void {
  validateRequired(value, "idempotencyKey", path, errors, validateIdempotencyKeyValue);
  validateThreadStatusChangedFields(value, path, errors);
}

function validateKnownKeys(
  record: Readonly<Record<string, unknown>>,
  basePath: string,
  allowed: ReadonlySet<string>,
  errors: ThreadValidationError[],
): void {
  for (const key of Object.keys(record)) {
    if (!allowed.has(key)) {
      addError(errors, pointer(basePath, key), "is not allowed");
    }
  }
}

function validateRequired(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ThreadValidationError[],
  validator: (value: unknown, path: string, errors: ThreadValidationError[]) => void,
): void {
  const fieldPath = pointer(basePath, key);
  const value = recordValue(record, key);
  if (!hasOwn(record, key) || value === undefined) {
    addError(errors, fieldPath, "is required");
    return;
  }
  validator(value, fieldPath, errors);
}

function validateOptional(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
  errors: ThreadValidationError[],
  validator: (value: unknown, path: string, errors: ThreadValidationError[]) => void,
): void {
  if (!hasOwn(record, key)) return;
  const value = recordValue(record, key);
  if (value === undefined) return;
  validator(value, pointer(basePath, key), errors);
}

function validateThreadIdValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isThreadId(value)) addError(errors, path, "must be an uppercase ULID ThreadId");
}

function validateThreadSpecRevisionIdValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isThreadSpecRevisionId(value)) {
    addError(errors, path, "must be an uppercase ULID ThreadSpecRevisionId");
  }
}

function validateSignerIdentityValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isSignerIdentity(value)) {
    addError(errors, path, "must be a bounded non-empty SignerIdentity");
  }
}

function validateIdempotencyKeyValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isIdempotencyKey(value)) {
    addError(errors, path, "must match /^[A-Za-z0-9_-]{1,128}$/");
  }
}

function validateThreadStatusValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (typeof value !== "string" || !THREAD_STATUS_SET.has(value)) {
    addError(errors, path, "must be a valid thread status");
  }
}

function validateSha256HexValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!isSha256Hex(value)) addError(errors, path, "must be a sha256 hex digest");
}

function validateDateValue(value: unknown, path: string, errors: ThreadValidationError[]): void {
  if (!(value instanceof Date) || Number.isNaN(value.getTime())) {
    addError(errors, path, "must be a valid Date");
  }
}

function validateThreadTitleValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (typeof value !== "string" || value.length === 0) {
    addError(errors, path, "must be a non-empty string");
    return;
  }
  const budget = validateThreadTitleBudget(value);
  if (!budget.ok) addError(errors, path, budget.reason);
}

function validateThreadSpecContentValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  const derived = deriveThreadSpecContentHash(value);
  if (!derived.ok) {
    addError(errors, path, derived.reason);
  }
}

function validateThreadTaskIdsValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!Array.isArray(value)) {
    addError(errors, path, "must be an array");
    return;
  }
  if (value.length > MAX_THREAD_TASK_IDS) {
    addError(
      errors,
      path,
      `length exceeds MAX_THREAD_TASK_IDS: ${value.length} > ${MAX_THREAD_TASK_IDS}`,
    );
  }
  const seen = new Set<string>();
  for (let i = 0; i < value.length; i += 1) {
    const item = arrayDataValue(value, i);
    const itemPath = pointer(path, String(i));
    if (!isTaskId(item)) {
      addError(errors, itemPath, "must be an uppercase ULID TaskId");
      continue;
    }
    if (seen.has(item)) {
      addError(errors, itemPath, "must be unique");
      continue;
    }
    seen.add(item);
  }
}

function validateExternalRefArrayValue(
  value: unknown,
  path: string,
  errors: ThreadValidationError[],
): void {
  if (!Array.isArray(value)) {
    addError(errors, path, "must be an array");
    return;
  }
  if (value.length > MAX_THREAD_EXTERNAL_REFS) {
    addError(
      errors,
      path,
      `length exceeds MAX_THREAD_EXTERNAL_REFS: ${value.length} > ${MAX_THREAD_EXTERNAL_REFS}`,
    );
  }
  const seen = new Set<string>();
  for (let i = 0; i < value.length; i += 1) {
    const item = arrayDataValue(value, i);
    const itemPath = pointer(path, String(i));
    if (typeof item !== "string" || item.length === 0) {
      addError(errors, itemPath, "must be a non-empty string");
      continue;
    }
    const budget = validateThreadExternalRefBudget(item);
    if (!budget.ok) addError(errors, itemPath, budget.reason);
    if (seen.has(item)) addError(errors, itemPath, "must be unique");
    seen.add(item);
  }
}

function deriveThreadSpecContentHash(
  content: unknown,
): { ok: true; hash: Sha256Hex } | { ok: false; reason: string } {
  let canonical: string;
  try {
    canonical = canonicalJSON(content);
  } catch (err) {
    return {
      ok: false,
      reason: err instanceof Error ? err.message : "must be canonical JSON content",
    };
  }
  const budget = validateThreadSpecContentBudget(canonical);
  if (!budget.ok) return { ok: false, reason: budget.reason };
  return { ok: true, hash: sha256Hex(TEXT_ENCODER.encode(canonical)) };
}

function jsonValueFromUnknown(value: unknown, path: string): JsonValue {
  try {
    canonicalJSON(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
  return value as JsonValue;
}

function isTerminalThreadStatus(value: unknown): value is "merged" | "closed" {
  return value === "merged" || value === "closed";
}

function isThreadStatus(value: unknown): value is ThreadStatus {
  return typeof value === "string" && THREAD_STATUS_SET.has(value);
}

function isAllowedThreadStatusTransition(
  fromStatus: ThreadStatus,
  toStatus: ThreadStatus,
): boolean {
  if (fromStatus === "open") return toStatus === "in_progress" || toStatus === "closed";
  if (fromStatus === "in_progress") {
    return toStatus === "needs_review" || toStatus === "closed";
  }
  if (fromStatus === "needs_review") return toStatus === "merged" || toStatus === "closed";
  return false;
}

function prefixErrors(
  errors: readonly ThreadValidationError[],
  basePath: string,
): ThreadValidationError[] {
  return errors.map((error) => ({
    path: `${basePath}${error.path}`,
    message: error.message,
  }));
}

function arrayDataValue(value: readonly unknown[], index: number): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(value, String(index));
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function asThreadIdAt(value: string, path: string): ThreadId {
  return decodeBrandAt(value, path, asThreadId);
}

function asThreadSpecRevisionIdAt(value: string, path: string): ThreadSpecRevisionId {
  return decodeBrandAt(value, path, asThreadSpecRevisionId);
}

function asSignerIdentityAt(value: string, path: string): SignerIdentity {
  return decodeBrandAt(value, path, asSignerIdentity);
}

function asTaskIdAt(value: string, path: string): TaskId {
  return decodeBrandAt(value, path, asTaskId);
}

function asSha256HexAt(value: string, path: string): Sha256Hex {
  return decodeBrandAt(value, path, asSha256Hex);
}

function decodeBrandAt<T>(value: string, path: string, decode: (value: string) => T): T {
  try {
    return decode(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function threadStatusFromJson(value: string, path: string): ThreadStatus {
  if (!THREAD_STATUS_SET.has(value)) {
    throw new Error(`${path}: must be one of ${THREAD_STATUS_VALUES.join(", ")}`);
  }
  return value as ThreadStatus;
}

function requiredFieldFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): unknown {
  if (!hasOwn(record, key) || record[key] === undefined) {
    throw new Error(`${basePath}.${key}: is required`);
  }
  return record[key];
}

function requiredStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string {
  const value = requiredFieldFromJson(record, key, basePath);
  if (typeof value !== "string") {
    throw new Error(`${basePath}.${key}: must be a string`);
  }
  return value;
}

function requiredStringArrayFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): readonly string[] {
  const value = requiredFieldFromJson(record, key, basePath);
  if (!Array.isArray(value)) {
    throw new Error(`${basePath}.${key}: must be an array`);
  }
  return value.map((item, index) => {
    if (typeof item !== "string") {
      throw new Error(`${basePath}.${key}.${index}: must be a string`);
    }
    return item;
  });
}

function optionalStringFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): string | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${basePath}.${key}: must be a string`);
  }
  return value;
}

function requiredDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date {
  const value = requiredStringFromJson(record, key, basePath);
  return dateFromJson(value, `${basePath}.${key}`);
}

function optionalDateFromJson(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): Date | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = record[key];
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${basePath}.${key}: must be an ISO 8601 string`);
  }
  return dateFromJson(value, `${basePath}.${key}`);
}

function dateFromJson(value: string, path: string): Date {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(value)) {
    throw new Error(`${path}: must be an ISO 8601 string`);
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.toISOString() !== value) {
    throw new Error(`${path}: must be a valid ISO 8601 instant`);
  }
  return date;
}

function pathToPointer(path: string): string {
  return `/${path
    .split(".")
    .map((segment) => segment.replace(/~/g, "~0").replace(/\//g, "~1"))
    .join("/")}`;
}
