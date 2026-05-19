import {
  type ApprovalDecision,
  type ApprovalRequest,
  approvalRequestFromJsonValue,
  approvalRequestToJsonValue,
} from "./approval-request.ts";
import {
  MAX_ROUTE_THREAD_LIST_ITEMS,
  validateRouteCursorBudget,
  validateRouteErrorCodeBudget,
  validateRouteErrorMessageBudget,
  validateThreadSpecContentBudget,
  validateThreadTitleBudget,
} from "./budgets.ts";
import { canonicalJSON, type JsonValue } from "./canonical-json.ts";
import { type EventLsn, parseLsn } from "./event-lsn.ts";
import {
  asIdempotencyKey,
  asReceiptId,
  asTaskId,
  asThreadId,
  asThreadSpecRevisionId,
  type IdempotencyKey,
  type ReceiptId,
  type RiskClass,
  type TaskId,
  type ThreadId,
  type ThreadSpecRevisionId,
} from "./receipt.ts";
import { APPROVAL_DECISION_VALUES, RISK_CLASS_VALUES } from "./receipt-literals.ts";
import { assertKnownKeys, hasOwn, omitUndefined, requireRecord } from "./receipt-utils.ts";
import { asSha256Hex, type Sha256Hex } from "./sha256.ts";
import {
  type ApprovalClaim,
  type ApprovalScope,
  approvalClaimFromJson,
  approvalClaimToJsonValue,
  approvalScopeFromJson,
  approvalScopeToJsonValue,
  type SignedApprovalToken,
  signedApprovalTokenFromJson,
  signedApprovalTokenToJsonValue,
} from "./signed-approval-token.ts";
import {
  THREAD_STATUS_VALUES,
  type Thread,
  type ThreadExternalRefs,
  type ThreadStatus,
  threadExternalRefsFromJsonValue,
  threadExternalRefsToJsonValue,
  threadFromJsonValue,
  threadToJsonValue,
} from "./thread.ts";

export type RouteEnvelopeSchemaVersion = 1;
export const ROUTE_ENVELOPE_SCHEMA_VERSION = 1 satisfies RouteEnvelopeSchemaVersion;

export interface ThreadCreateRequest {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly title: string;
  readonly specContent: JsonValue;
  readonly externalRefs?: ThreadExternalRefs | undefined;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ThreadSpecEditRequest {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly baseRevisionId: ThreadSpecRevisionId;
  readonly baseContentHash: Sha256Hex;
  readonly content: JsonValue;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ThreadStatusChangeRequest {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly fromStatus: ThreadStatus;
  readonly toStatus: ThreadStatus;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ThreadMutationResponse {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly threadId: ThreadId;
  readonly headLsn: EventLsn;
  readonly revisionId: ThreadSpecRevisionId;
  readonly contentHash: Sha256Hex;
}

export interface ThreadListResponse {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly threads: readonly Thread[];
  readonly nextCursor?: string | undefined;
}

export interface ThreadGetResponse {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly thread: Thread;
}

export interface ApprovalRequestCreateRequest {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly claim: ApprovalClaim;
  readonly scope: ApprovalScope;
  readonly riskClass: RiskClass;
  readonly threadId?: ThreadId | undefined;
  readonly taskId?: TaskId | undefined;
  readonly receiptId?: ReceiptId | undefined;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ApprovalDecisionRequest {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly decision: ApprovalDecision;
  readonly token?: SignedApprovalToken | undefined;
  readonly idempotencyKey: IdempotencyKey;
}

export interface ApprovalRequestCreateResponse {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly approvalRequest: ApprovalRequest;
  readonly headLsn: EventLsn;
}

export interface ApprovalDecisionResponse {
  readonly schemaVersion?: RouteEnvelopeSchemaVersion | undefined;
  readonly approvalRequest: ApprovalRequest;
  readonly headLsn: EventLsn;
}

export interface RouteError {
  readonly error: string;
  readonly message?: string | undefined;
  readonly retryAfterMs?: number | undefined;
}

type ThreadCreateRequestWire = Readonly<
  Record<"schemaVersion" | "title" | "specContent" | "externalRefs" | "idempotencyKey", unknown>
>;
type ThreadSpecEditRequestWire = Readonly<
  Record<
    "schemaVersion" | "baseRevisionId" | "baseContentHash" | "content" | "idempotencyKey",
    unknown
  >
>;
type ThreadStatusChangeRequestWire = Readonly<
  Record<"schemaVersion" | "fromStatus" | "toStatus" | "idempotencyKey", unknown>
>;
type ThreadMutationResponseWire = Readonly<
  Record<"schemaVersion" | "threadId" | "headLsn" | "revisionId" | "contentHash", unknown>
>;
type ThreadListResponseWire = Readonly<Record<"schemaVersion" | "threads" | "nextCursor", unknown>>;
type ThreadGetResponseWire = Readonly<Record<"schemaVersion" | "thread", unknown>>;
type ApprovalRequestCreateRequestWire = Readonly<
  Record<
    | "schemaVersion"
    | "claim"
    | "scope"
    | "riskClass"
    | "threadId"
    | "taskId"
    | "receiptId"
    | "idempotencyKey",
    unknown
  >
>;
type ApprovalDecisionRequestWire = Readonly<
  Record<"schemaVersion" | "decision" | "token" | "idempotencyKey", unknown>
>;
type ApprovalRequestCreateResponseWire = Readonly<
  Record<"schemaVersion" | "approvalRequest" | "headLsn", unknown>
>;
type RouteErrorWire = Readonly<Record<"error" | "message" | "retryAfterMs", unknown>>;

const THREAD_CREATE_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "title",
  "specContent",
  "externalRefs",
  "idempotencyKey",
] as const satisfies readonly (keyof ThreadCreateRequestWire)[];
const THREAD_SPEC_EDIT_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "baseRevisionId",
  "baseContentHash",
  "content",
  "idempotencyKey",
] as const satisfies readonly (keyof ThreadSpecEditRequestWire)[];
const THREAD_STATUS_CHANGE_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "fromStatus",
  "toStatus",
  "idempotencyKey",
] as const satisfies readonly (keyof ThreadStatusChangeRequestWire)[];
const THREAD_MUTATION_RESPONSE_KEYS_TUPLE = [
  "schemaVersion",
  "threadId",
  "headLsn",
  "revisionId",
  "contentHash",
] as const satisfies readonly (keyof ThreadMutationResponseWire)[];
const THREAD_LIST_RESPONSE_KEYS_TUPLE = [
  "schemaVersion",
  "threads",
  "nextCursor",
] as const satisfies readonly (keyof ThreadListResponseWire)[];
const THREAD_GET_RESPONSE_KEYS_TUPLE = [
  "schemaVersion",
  "thread",
] as const satisfies readonly (keyof ThreadGetResponseWire)[];
const APPROVAL_REQUEST_CREATE_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "claim",
  "scope",
  "riskClass",
  "threadId",
  "taskId",
  "receiptId",
  "idempotencyKey",
] as const satisfies readonly (keyof ApprovalRequestCreateRequestWire)[];
const APPROVAL_DECISION_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "decision",
  "token",
  "idempotencyKey",
] as const satisfies readonly (keyof ApprovalDecisionRequestWire)[];
const APPROVAL_RESPONSE_KEYS_TUPLE = [
  "schemaVersion",
  "approvalRequest",
  "headLsn",
] as const satisfies readonly (keyof ApprovalRequestCreateResponseWire)[];
const ROUTE_ERROR_KEYS_TUPLE = [
  "error",
  "message",
  "retryAfterMs",
] as const satisfies readonly (keyof RouteErrorWire)[];

const THREAD_CREATE_REQUEST_KEYS: ReadonlySet<string> = new Set(THREAD_CREATE_REQUEST_KEYS_TUPLE);
const THREAD_SPEC_EDIT_REQUEST_KEYS: ReadonlySet<string> = new Set(
  THREAD_SPEC_EDIT_REQUEST_KEYS_TUPLE,
);
const THREAD_STATUS_CHANGE_REQUEST_KEYS: ReadonlySet<string> = new Set(
  THREAD_STATUS_CHANGE_REQUEST_KEYS_TUPLE,
);
const THREAD_MUTATION_RESPONSE_KEYS: ReadonlySet<string> = new Set(
  THREAD_MUTATION_RESPONSE_KEYS_TUPLE,
);
const THREAD_LIST_RESPONSE_KEYS: ReadonlySet<string> = new Set(THREAD_LIST_RESPONSE_KEYS_TUPLE);
const THREAD_GET_RESPONSE_KEYS: ReadonlySet<string> = new Set(THREAD_GET_RESPONSE_KEYS_TUPLE);
const APPROVAL_REQUEST_CREATE_REQUEST_KEYS: ReadonlySet<string> = new Set(
  APPROVAL_REQUEST_CREATE_REQUEST_KEYS_TUPLE,
);
const APPROVAL_DECISION_REQUEST_KEYS: ReadonlySet<string> = new Set(
  APPROVAL_DECISION_REQUEST_KEYS_TUPLE,
);
const APPROVAL_RESPONSE_KEYS: ReadonlySet<string> = new Set(APPROVAL_RESPONSE_KEYS_TUPLE);
const ROUTE_ERROR_KEYS: ReadonlySet<string> = new Set(ROUTE_ERROR_KEYS_TUPLE);

const THREAD_STATUS_SET: ReadonlySet<string> = new Set<string>(THREAD_STATUS_VALUES);
const APPROVAL_DECISION_SET: ReadonlySet<string> = new Set<string>(APPROVAL_DECISION_VALUES);
const RISK_CLASS_SET: ReadonlySet<string> = new Set<string>(RISK_CLASS_VALUES);

export function threadCreateRequestFromJson(value: unknown): ThreadCreateRequest {
  const record = requireRecord(value, "threadCreateRequest");
  assertKnownKeys(record, "threadCreateRequest", THREAD_CREATE_REQUEST_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadCreateRequest");
  const externalRefs = optionalThreadExternalRefs(record, "externalRefs", "threadCreateRequest");
  return omitUndefined({
    schemaVersion,
    title: threadTitleFromJson(
      requiredNonEmptyString(record, "title", "threadCreateRequest.title"),
      "threadCreateRequest.title",
    ),
    specContent: threadSpecContentFromJson(
      requiredField(record, "specContent", "threadCreateRequest.specContent"),
      "threadCreateRequest.specContent",
    ),
    externalRefs,
    idempotencyKey: idempotencyKeyFromJson(
      requiredString(record, "idempotencyKey", "threadCreateRequest.idempotencyKey"),
      "threadCreateRequest.idempotencyKey",
    ),
  });
}

export function threadCreateRequestToJsonValue(
  request: ThreadCreateRequest,
): Readonly<Record<string, unknown>> {
  return omitUndefined({
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    title: request.title,
    specContent: request.specContent,
    externalRefs:
      request.externalRefs === undefined
        ? undefined
        : threadExternalRefsToJsonValue(request.externalRefs),
    idempotencyKey: request.idempotencyKey,
  });
}

export function threadSpecEditRequestFromJson(value: unknown): ThreadSpecEditRequest {
  const record = requireRecord(value, "threadSpecEditRequest");
  assertKnownKeys(record, "threadSpecEditRequest", THREAD_SPEC_EDIT_REQUEST_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadSpecEditRequest");
  return {
    schemaVersion,
    baseRevisionId: threadSpecRevisionIdFromJson(
      requiredString(record, "baseRevisionId", "threadSpecEditRequest.baseRevisionId"),
      "threadSpecEditRequest.baseRevisionId",
    ),
    baseContentHash: sha256HexFromJson(
      requiredString(record, "baseContentHash", "threadSpecEditRequest.baseContentHash"),
      "threadSpecEditRequest.baseContentHash",
    ),
    content: threadSpecContentFromJson(
      requiredField(record, "content", "threadSpecEditRequest.content"),
      "threadSpecEditRequest.content",
    ),
    idempotencyKey: idempotencyKeyFromJson(
      requiredString(record, "idempotencyKey", "threadSpecEditRequest.idempotencyKey"),
      "threadSpecEditRequest.idempotencyKey",
    ),
  };
}

export function threadSpecEditRequestToJsonValue(
  request: ThreadSpecEditRequest,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    baseRevisionId: request.baseRevisionId,
    baseContentHash: request.baseContentHash,
    content: request.content,
    idempotencyKey: request.idempotencyKey,
  };
}

export function threadStatusChangeRequestFromJson(value: unknown): ThreadStatusChangeRequest {
  const record = requireRecord(value, "threadStatusChangeRequest");
  assertKnownKeys(record, "threadStatusChangeRequest", THREAD_STATUS_CHANGE_REQUEST_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadStatusChangeRequest");
  return {
    schemaVersion,
    fromStatus: threadStatusFromJson(
      requiredString(record, "fromStatus", "threadStatusChangeRequest.fromStatus"),
      "threadStatusChangeRequest.fromStatus",
    ),
    toStatus: threadStatusFromJson(
      requiredString(record, "toStatus", "threadStatusChangeRequest.toStatus"),
      "threadStatusChangeRequest.toStatus",
    ),
    idempotencyKey: idempotencyKeyFromJson(
      requiredString(record, "idempotencyKey", "threadStatusChangeRequest.idempotencyKey"),
      "threadStatusChangeRequest.idempotencyKey",
    ),
  };
}

export function threadStatusChangeRequestToJsonValue(
  request: ThreadStatusChangeRequest,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    fromStatus: request.fromStatus,
    toStatus: request.toStatus,
    idempotencyKey: request.idempotencyKey,
  };
}

export function threadMutationResponseFromJson(value: unknown): ThreadMutationResponse {
  const record = requireRecord(value, "threadMutationResponse");
  assertKnownKeys(record, "threadMutationResponse", THREAD_MUTATION_RESPONSE_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadMutationResponse");
  return {
    schemaVersion,
    threadId: threadIdFromJson(
      requiredString(record, "threadId", "threadMutationResponse.threadId"),
      "threadMutationResponse.threadId",
    ),
    headLsn: eventLsnFromJson(
      requiredString(record, "headLsn", "threadMutationResponse.headLsn"),
      "threadMutationResponse.headLsn",
    ),
    revisionId: threadSpecRevisionIdFromJson(
      requiredString(record, "revisionId", "threadMutationResponse.revisionId"),
      "threadMutationResponse.revisionId",
    ),
    contentHash: sha256HexFromJson(
      requiredString(record, "contentHash", "threadMutationResponse.contentHash"),
      "threadMutationResponse.contentHash",
    ),
  };
}

export function threadMutationResponseToJsonValue(
  response: ThreadMutationResponse,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    threadId: response.threadId,
    headLsn: response.headLsn,
    revisionId: response.revisionId,
    contentHash: response.contentHash,
  };
}

export function threadListResponseFromJson(value: unknown): ThreadListResponse {
  const record = requireRecord(value, "threadListResponse");
  assertKnownKeys(record, "threadListResponse", THREAD_LIST_RESPONSE_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadListResponse");
  const nextCursor = optionalCursor(record, "nextCursor", "threadListResponse.nextCursor");
  return omitUndefined({
    schemaVersion,
    threads: threadArrayFromJson(
      requiredField(record, "threads", "threadListResponse.threads"),
      "threadListResponse.threads",
    ),
    nextCursor,
  });
}

export function threadListResponseToJsonValue(
  response: ThreadListResponse,
): Readonly<Record<string, unknown>> {
  return omitUndefined({
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    threads: response.threads.map((thread) => threadToJsonValue(thread)),
    nextCursor: response.nextCursor,
  });
}

export function threadGetResponseFromJson(value: unknown): ThreadGetResponse {
  const record = requireRecord(value, "threadGetResponse");
  assertKnownKeys(record, "threadGetResponse", THREAD_GET_RESPONSE_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "threadGetResponse");
  return {
    schemaVersion,
    thread: threadFromJsonValue(requiredField(record, "thread", "threadGetResponse.thread")),
  };
}

export function threadGetResponseToJsonValue(
  response: ThreadGetResponse,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    thread: threadToJsonValue(response.thread),
  };
}

export function approvalRequestCreateRequestFromJson(value: unknown): ApprovalRequestCreateRequest {
  const record = requireRecord(value, "approvalRequestCreateRequest");
  assertKnownKeys(record, "approvalRequestCreateRequest", APPROVAL_REQUEST_CREATE_REQUEST_KEYS);
  const schemaVersion = optionalSchemaVersion(
    record,
    "schemaVersion",
    "approvalRequestCreateRequest",
  );
  const claim = approvalClaimFromJson(
    requiredField(record, "claim", "approvalRequestCreateRequest.claim"),
    "approvalRequestCreateRequest.claim",
  );
  const scope = approvalScopeFromJson(
    requiredField(record, "scope", "approvalRequestCreateRequest.scope"),
    "approvalRequestCreateRequest.scope",
  );
  const riskClass = riskClassFromJson(
    requiredString(record, "riskClass", "approvalRequestCreateRequest.riskClass"),
    "approvalRequestCreateRequest.riskClass",
  );
  const threadId = optionalThreadId(record, "threadId", "approvalRequestCreateRequest.threadId");
  const taskId = optionalTaskId(record, "taskId", "approvalRequestCreateRequest.taskId");
  const receiptId = optionalReceiptId(
    record,
    "receiptId",
    "approvalRequestCreateRequest.receiptId",
  );
  validateApprovalRequestCreateBindings(claim, scope, riskClass, receiptId);
  return omitUndefined({
    schemaVersion,
    claim,
    scope,
    riskClass,
    threadId,
    taskId,
    receiptId,
    idempotencyKey: idempotencyKeyFromJson(
      requiredString(record, "idempotencyKey", "approvalRequestCreateRequest.idempotencyKey"),
      "approvalRequestCreateRequest.idempotencyKey",
    ),
  });
}

export function approvalRequestCreateRequestToJsonValue(
  request: ApprovalRequestCreateRequest,
): Readonly<Record<string, unknown>> {
  return omitUndefined({
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    claim: approvalClaimToJsonValue(request.claim),
    scope: approvalScopeToJsonValue(request.scope),
    riskClass: request.riskClass,
    threadId: request.threadId,
    taskId: request.taskId,
    receiptId: request.receiptId,
    idempotencyKey: request.idempotencyKey,
  });
}

export function approvalDecisionRequestFromJson(value: unknown): ApprovalDecisionRequest {
  const record = requireRecord(value, "approvalDecisionRequest");
  assertKnownKeys(record, "approvalDecisionRequest", APPROVAL_DECISION_REQUEST_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "approvalDecisionRequest");
  const decision = approvalDecisionFromJson(
    requiredString(record, "decision", "approvalDecisionRequest.decision"),
    "approvalDecisionRequest.decision",
  );
  const token = optionalSignedApprovalToken(record, "token", "approvalDecisionRequest.token");
  if (decision === "approve" && token === undefined) {
    throw new Error("approvalDecisionRequest.token: is required when decision is approve");
  }
  return omitUndefined({
    schemaVersion,
    decision,
    token,
    idempotencyKey: idempotencyKeyFromJson(
      requiredString(record, "idempotencyKey", "approvalDecisionRequest.idempotencyKey"),
      "approvalDecisionRequest.idempotencyKey",
    ),
  });
}

export function approvalDecisionRequestToJsonValue(
  request: ApprovalDecisionRequest,
): Readonly<Record<string, unknown>> {
  return omitUndefined({
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    decision: request.decision,
    token: request.token === undefined ? undefined : signedApprovalTokenToJsonValue(request.token),
    idempotencyKey: request.idempotencyKey,
  });
}

export function approvalRequestCreateResponseFromJson(
  value: unknown,
): ApprovalRequestCreateResponse {
  const record = requireRecord(value, "approvalRequestCreateResponse");
  assertKnownKeys(record, "approvalRequestCreateResponse", APPROVAL_RESPONSE_KEYS);
  const schemaVersion = optionalSchemaVersion(
    record,
    "schemaVersion",
    "approvalRequestCreateResponse",
  );
  return {
    schemaVersion,
    approvalRequest: approvalRequestFromJsonValue(
      requiredField(record, "approvalRequest", "approvalRequestCreateResponse.approvalRequest"),
    ),
    headLsn: eventLsnFromJson(
      requiredString(record, "headLsn", "approvalRequestCreateResponse.headLsn"),
      "approvalRequestCreateResponse.headLsn",
    ),
  };
}

export function approvalRequestCreateResponseToJsonValue(
  response: ApprovalRequestCreateResponse,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    approvalRequest: approvalRequestToJsonValue(response.approvalRequest),
    headLsn: response.headLsn,
  };
}

export function approvalDecisionResponseFromJson(value: unknown): ApprovalDecisionResponse {
  const record = requireRecord(value, "approvalDecisionResponse");
  assertKnownKeys(record, "approvalDecisionResponse", APPROVAL_RESPONSE_KEYS);
  const schemaVersion = optionalSchemaVersion(record, "schemaVersion", "approvalDecisionResponse");
  return {
    schemaVersion,
    approvalRequest: approvalRequestFromJsonValue(
      requiredField(record, "approvalRequest", "approvalDecisionResponse.approvalRequest"),
    ),
    headLsn: eventLsnFromJson(
      requiredString(record, "headLsn", "approvalDecisionResponse.headLsn"),
      "approvalDecisionResponse.headLsn",
    ),
  };
}

export function approvalDecisionResponseToJsonValue(
  response: ApprovalDecisionResponse,
): Readonly<Record<string, unknown>> {
  return {
    schemaVersion: ROUTE_ENVELOPE_SCHEMA_VERSION,
    approvalRequest: approvalRequestToJsonValue(response.approvalRequest),
    headLsn: response.headLsn,
  };
}

export function routeErrorFromJson(value: unknown): RouteError {
  const record = requireRecord(value, "routeError");
  assertKnownKeys(record, "routeError", ROUTE_ERROR_KEYS);
  const message = optionalStringWithBudget(
    record,
    "message",
    "routeError.message",
    validateRouteErrorMessageBudget,
  );
  const retryAfterMs = optionalRetryAfterMs(record, "retryAfterMs", "routeError.retryAfterMs");
  return omitUndefined({
    error: routeErrorCodeFromJson(requiredString(record, "error", "routeError.error")),
    message,
    retryAfterMs,
  });
}

export function routeErrorToJsonValue(error: RouteError): Readonly<Record<string, unknown>> {
  return omitUndefined({
    error: error.error,
    message: error.message,
    retryAfterMs: error.retryAfterMs,
  });
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

function optionalStringWithBudget(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  validateBudget: (value: string) => { readonly ok: true } | { readonly ok: false; reason: string },
): string | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  const budget = validateBudget(value);
  if (!budget.ok) throw new Error(`${path}: ${budget.reason}`);
  return value;
}

function idempotencyKeyFromJson(value: string, path: string): IdempotencyKey {
  try {
    return asIdempotencyKey(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
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

function optionalTaskId(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): TaskId | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${path}: must be a string`);
  try {
    return asTaskId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function optionalReceiptId(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): ReceiptId | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string") throw new Error(`${path}: must be a string`);
  try {
    return asReceiptId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function threadSpecRevisionIdFromJson(value: string, path: string): ThreadSpecRevisionId {
  try {
    return asThreadSpecRevisionId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function sha256HexFromJson(value: string, path: string): Sha256Hex {
  try {
    return asSha256Hex(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function eventLsnFromJson(value: string, path: string): EventLsn {
  try {
    parseLsn(value as EventLsn);
    return value as EventLsn;
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

function approvalDecisionFromJson(value: string, path: string): ApprovalDecision {
  if (!APPROVAL_DECISION_SET.has(value)) {
    throw new Error(`${path}: must be one of ${APPROVAL_DECISION_VALUES.join(", ")}`);
  }
  return value as ApprovalDecision;
}

function riskClassFromJson(value: string, path: string): RiskClass {
  if (!RISK_CLASS_SET.has(value)) {
    throw new Error(`${path}: must be one of ${RISK_CLASS_VALUES.join(", ")}`);
  }
  return value as RiskClass;
}

function threadSpecContentFromJson(value: unknown, path: string): JsonValue {
  let canonical: string;
  try {
    canonical = canonicalJSON(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
  const budget = validateThreadSpecContentBudget(canonical);
  if (!budget.ok) throw new Error(`${path}: ${budget.reason}`);
  return value as JsonValue;
}

function threadTitleFromJson(value: string, path: string): string {
  const budget = validateThreadTitleBudget(value);
  if (!budget.ok) throw new Error(`${path}: ${budget.reason}`);
  return value;
}

function optionalThreadExternalRefs(
  record: Readonly<Record<string, unknown>>,
  key: string,
  basePath: string,
): ThreadExternalRefs | undefined {
  const path = `${basePath}.${key}`;
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  return threadExternalRefsFromJsonValue(value, [basePath, key]);
}

function optionalSignedApprovalToken(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): SignedApprovalToken | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  return signedApprovalTokenFromJson(value, path);
}

function threadArrayFromJson(value: unknown, path: string): readonly Thread[] {
  if (!Array.isArray(value)) {
    throw new Error(`${path}: must be an array`);
  }
  if (value.length > MAX_ROUTE_THREAD_LIST_ITEMS) {
    throw new Error(
      `${path}: length exceeds MAX_ROUTE_THREAD_LIST_ITEMS: ${value.length} > ${MAX_ROUTE_THREAD_LIST_ITEMS}`,
    );
  }
  return value.map((item) => threadFromJsonValue(item));
}

function optionalCursor(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  if (value.length === 0) {
    throw new Error(`${path}: must be non-empty when present`);
  }
  const budget = validateRouteCursorBudget(value);
  if (!budget.ok) throw new Error(`${path}: ${budget.reason}`);
  return value;
}

function optionalRetryAfterMs(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): number | undefined {
  const value = optionalField(record, key, path);
  if (value === undefined) return undefined;
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path}: must be a non-negative safe integer`);
  }
  return value;
}

function routeErrorCodeFromJson(value: string): string {
  if (value.length === 0) {
    throw new Error("routeError.error: must be a non-empty string");
  }
  const budget = validateRouteErrorCodeBudget(value);
  if (!budget.ok) throw new Error(`routeError.error: ${budget.reason}`);
  return value;
}

function validateApprovalRequestCreateBindings(
  claim: ApprovalClaim,
  scope: ApprovalScope,
  riskClass: RiskClass,
  receiptId: ReceiptId | undefined,
): void {
  if (claim.claimId !== scope.claimId) {
    throw new Error("approvalRequestCreateRequest.scope.claimId: must match claim.claimId");
  }
  if (claim.kind !== scope.claimKind) {
    throw new Error("approvalRequestCreateRequest.scope.claimKind: must match claim.kind");
  }
  switch (claim.kind) {
    case "cost_spike_acknowledgement":
      if (scope.claimKind !== claim.kind) return;
      requireSame(scope.agentId, claim.agentId, "agentId");
      requireSame(scope.costCeilingId, claim.costCeilingId, "costCeilingId");
      return;
    case "endpoint_allowlist_extension":
      if (scope.claimKind !== claim.kind) return;
      requireSame(scope.agentId, claim.agentId, "agentId");
      requireSame(scope.providerKind, claim.providerKind, "providerKind");
      requireSame(scope.endpointOrigin, claim.endpointOrigin, "endpointOrigin");
      return;
    case "credential_grant_to_agent":
      if (scope.claimKind !== claim.kind) return;
      requireSame(scope.granteeAgentId, claim.granteeAgentId, "granteeAgentId");
      requireSame(scope.credentialHandleId, claim.credentialHandleId, "credentialHandleId");
      return;
    case "receipt_co_sign":
      if (scope.claimKind !== claim.kind) return;
      requireSame(scope.receiptId, claim.receiptId, "receiptId");
      requireSame(scope.writeId, claim.writeId, "writeId");
      requireSame(scope.frozenArgsHash, claim.frozenArgsHash, "frozenArgsHash");
      if (receiptId !== claim.receiptId) {
        throw new Error("approvalRequestCreateRequest.receiptId: must match claim.receiptId");
      }
      if (riskClass !== claim.riskClass) {
        throw new Error("approvalRequestCreateRequest.riskClass: must match claim.riskClass");
      }
      return;
  }
}

function requireSame(left: unknown, right: unknown, field: string): void {
  if (left !== right) {
    throw new Error(`approvalRequestCreateRequest.scope.${field}: must match claim.${field}`);
  }
}
