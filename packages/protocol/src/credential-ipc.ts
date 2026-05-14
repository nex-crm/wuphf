import { validateCredentialSecretBudget } from "./budgets.ts";
import {
  type AgentId,
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  type CredentialHandleId,
  type CredentialHandleJson,
  type CredentialScope,
  credentialHandleJsonFromJson,
} from "./credential-handle.ts";
import { assertKnownKeys, hasOwn, requireRecord } from "./receipt-utils.ts";

function assertSecretWithinBudget(secret: string, label: string): void {
  const budget = validateCredentialSecretBudget(secret);
  if (!budget.ok) {
    throw new Error(`${label}: ${budget.reason}`);
  }
}

export interface CredentialReadRequest {
  readonly agentId: AgentId;
  readonly handleId: CredentialHandleId;
}

export interface CredentialReadResponse {
  readonly secret: string;
}

export interface CredentialWriteRequest {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
  readonly secret: string;
}

export interface CredentialWriteResponse {
  readonly handle: CredentialHandleJson;
}

export interface CredentialDeleteRequest {
  readonly agentId: AgentId;
  readonly handleId: CredentialHandleId;
}

export interface CredentialDeleteResponse {
  readonly deleted: true;
}

const CREDENTIAL_READ_REQUEST_KEYS_TUPLE = [
  "agentId",
  "handleId",
] as const satisfies readonly (keyof CredentialReadRequest)[];
const CREDENTIAL_READ_REQUEST_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_READ_REQUEST_KEYS_TUPLE,
);
const CREDENTIAL_READ_RESPONSE_KEYS_TUPLE = [
  "secret",
] as const satisfies readonly (keyof CredentialReadResponse)[];
const CREDENTIAL_READ_RESPONSE_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_READ_RESPONSE_KEYS_TUPLE,
);
const CREDENTIAL_WRITE_REQUEST_KEYS_TUPLE = [
  "agentId",
  "scope",
  "secret",
] as const satisfies readonly (keyof CredentialWriteRequest)[];
const CREDENTIAL_WRITE_REQUEST_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_WRITE_REQUEST_KEYS_TUPLE,
);
const CREDENTIAL_WRITE_RESPONSE_KEYS_TUPLE = [
  "handle",
] as const satisfies readonly (keyof CredentialWriteResponse)[];
const CREDENTIAL_WRITE_RESPONSE_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_WRITE_RESPONSE_KEYS_TUPLE,
);
const CREDENTIAL_DELETE_REQUEST_KEYS_TUPLE = [
  "agentId",
  "handleId",
] as const satisfies readonly (keyof CredentialDeleteRequest)[];
const CREDENTIAL_DELETE_REQUEST_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_DELETE_REQUEST_KEYS_TUPLE,
);
const CREDENTIAL_DELETE_RESPONSE_KEYS_TUPLE = [
  "deleted",
] as const satisfies readonly (keyof CredentialDeleteResponse)[];
const CREDENTIAL_DELETE_RESPONSE_KEYS: ReadonlySet<string> = new Set(
  CREDENTIAL_DELETE_RESPONSE_KEYS_TUPLE,
);

export function credentialReadRequestFromJson(value: unknown): CredentialReadRequest {
  const record = requireRecord(value, "credentialReadRequest");
  assertKnownKeys(record, "credentialReadRequest", CREDENTIAL_READ_REQUEST_KEYS);
  return {
    agentId: agentIdFromJson(
      requiredStringJsonField(record, "agentId", "credentialReadRequest.agentId"),
      "credentialReadRequest.agentId",
    ),
    handleId: credentialHandleIdFromJson(
      requiredStringJsonField(record, "handleId", "credentialReadRequest.handleId"),
      "credentialReadRequest.handleId",
    ),
  };
}

export function credentialReadResponseFromJson(value: unknown): CredentialReadResponse {
  const record = requireRecord(value, "credentialReadResponse");
  assertKnownKeys(record, "credentialReadResponse", CREDENTIAL_READ_RESPONSE_KEYS);
  const secret = requiredStringJsonField(record, "secret", "credentialReadResponse.secret");
  assertSecretWithinBudget(secret, "credentialReadResponse.secret");
  return { secret };
}

export function credentialWriteRequestFromJson(value: unknown): CredentialWriteRequest {
  const record = requireRecord(value, "credentialWriteRequest");
  assertKnownKeys(record, "credentialWriteRequest", CREDENTIAL_WRITE_REQUEST_KEYS);
  const secret = requiredStringJsonField(record, "secret", "credentialWriteRequest.secret");
  assertSecretWithinBudget(secret, "credentialWriteRequest.secret");
  return {
    agentId: agentIdFromJson(
      requiredStringJsonField(record, "agentId", "credentialWriteRequest.agentId"),
      "credentialWriteRequest.agentId",
    ),
    scope: credentialScopeFromJson(
      requiredStringJsonField(record, "scope", "credentialWriteRequest.scope"),
      "credentialWriteRequest.scope",
    ),
    secret,
  };
}

export function credentialWriteResponseFromJson(value: unknown): CredentialWriteResponse {
  const record = requireRecord(value, "credentialWriteResponse");
  assertKnownKeys(record, "credentialWriteResponse", CREDENTIAL_WRITE_RESPONSE_KEYS);
  return {
    handle: credentialHandleJsonFromJson(
      requiredJsonField(record, "handle", "credentialWriteResponse.handle"),
    ),
  };
}

export function credentialDeleteRequestFromJson(value: unknown): CredentialDeleteRequest {
  const record = requireRecord(value, "credentialDeleteRequest");
  assertKnownKeys(record, "credentialDeleteRequest", CREDENTIAL_DELETE_REQUEST_KEYS);
  return {
    agentId: agentIdFromJson(
      requiredStringJsonField(record, "agentId", "credentialDeleteRequest.agentId"),
      "credentialDeleteRequest.agentId",
    ),
    handleId: credentialHandleIdFromJson(
      requiredStringJsonField(record, "handleId", "credentialDeleteRequest.handleId"),
      "credentialDeleteRequest.handleId",
    ),
  };
}

export function credentialDeleteResponseFromJson(value: unknown): CredentialDeleteResponse {
  const record = requireRecord(value, "credentialDeleteResponse");
  assertKnownKeys(record, "credentialDeleteResponse", CREDENTIAL_DELETE_RESPONSE_KEYS);
  const deleted = requiredJsonField(record, "deleted", "credentialDeleteResponse.deleted");
  if (deleted !== true) {
    throw new Error("credentialDeleteResponse.deleted: must be true");
  }
  return { deleted };
}

function requiredJsonField(record: Readonly<Record<string, unknown>>, key: string, path: string) {
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

function requiredStringJsonField(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredJsonField(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return value;
}

function agentIdFromJson(value: string, path: string): AgentId {
  try {
    return asAgentId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function credentialHandleIdFromJson(value: string, path: string): CredentialHandleId {
  try {
    return asCredentialHandleId(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function credentialScopeFromJson(value: string, path: string): CredentialScope {
  try {
    return asCredentialScope(value);
  } catch (err) {
    throw new Error(`${path}: ${err instanceof Error ? err.message : String(err)}`);
  }
}
