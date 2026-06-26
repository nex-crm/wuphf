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
  isSignerIdentity,
  isTaskId,
  isThreadId,
  isThreadSpecRevisionId,
  type SignerIdentity,
  type TaskId,
  type ThreadId,
  type ThreadSpecRevisionId,
} from "./receipt-types.ts";
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
import { asSha256Hex, isSha256Hex, type Sha256Hex } from "./sha256.ts";

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

export type ThreadValidationError = { path: string; message: string };
export type ThreadValidationResult = { ok: true } | { ok: false; errors: ThreadValidationError[] };

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

export function threadToJson(thread: Thread): string {
  const validation = validateThread(thread);
  if (!validation.ok) {
    throw new Error(formatValidationErrors(validation.errors));
  }
  return canonicalJSON(threadToJsonValue(thread));
}

export function threadFromJson(json: string): Thread {
  return threadFromJsonValue(JSON.parse(json));
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
  let canonical: string;
  try {
    canonical = canonicalJSON(value);
  } catch (err) {
    addError(errors, path, err instanceof Error ? err.message : "must be canonical JSON content");
    return;
  }
  const budget = validateThreadSpecContentBudget(canonical);
  if (!budget.ok) addError(errors, path, budget.reason);
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
