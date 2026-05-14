import type { Brand } from "./brand.ts";
import {
  MAX_RUNNER_CWD_BYTES,
  MAX_RUNNER_ERROR_BYTES,
  MAX_RUNNER_MODEL_BYTES,
  MAX_RUNNER_PROMPT_BYTES,
  MAX_RUNNER_STDIO_CHUNK_BYTES,
} from "./budgets.ts";
import {
  type CostEventAuditPayload,
  costAuditPayloadFromJsonValue,
  costAuditPayloadToJsonValue,
  isMicroUsd,
  type MicroUsd,
} from "./cost.ts";
import {
  type AgentId,
  asAgentId,
  asCredentialScope,
  type CredentialHandleJson,
  type CredentialScope,
  credentialHandleJsonFromJson,
  isCredentialScope,
} from "./credential-handle.ts";
import {
  asProviderKind,
  asTaskId,
  isProviderKind,
  isTaskId,
  type ProviderKind,
  type ReceiptId,
  type TaskId,
} from "./receipt-types.ts";
import { assertKnownKeys, hasOwn, requireRecord } from "./receipt-utils.ts";

export type RunnerId = Brand<string, "RunnerId">;
export type RunnerKind = (typeof RUNNER_KIND_VALUES)[number];
export type CostLedgerEntry = CostEventAuditPayload;
export type RunnerSchemaVersion = 1;
export type RunnerFailureCode = (typeof RUNNER_FAILURE_CODE_VALUES)[number];

export const RUNNER_KIND_VALUES = ["claude-cli", "codex-cli", "openai-compat"] as const;
export const RUNNER_SCHEMA_VERSION = 1 satisfies RunnerSchemaVersion;
export const RUNNER_FAILURE_CODE_VALUES = [
  "spawn_failed",
  "receipt_write_failed",
  "event_log_write_failed",
  "cost_ledger_write_failed",
  "cost_ceiling_exceeded",
  "credential_ownership_mismatch",
  "subprocess_crashed",
  "subprocess_timed_out",
  "terminated_by_request",
  "network_failed",
  "provider_returned_error",
  "unrecognized_provider_response",
  "subscriber_backpressure_exceeded",
] as const;

export interface RunnerProviderRoute {
  readonly credentialScope: CredentialScope;
  readonly providerKind: ProviderKind;
}

export interface RunnerSpawnRequest {
  readonly schemaVersion?: RunnerSchemaVersion | undefined;
  readonly kind: RunnerKind;
  readonly agentId: AgentId;
  readonly credential: CredentialHandleJson;
  readonly providerRoute?: RunnerProviderRoute | undefined;
  readonly prompt: string;
  readonly model?: string | undefined;
  readonly cwd?: string | undefined;
  readonly taskId?: TaskId | undefined;
  readonly costCeilingMicroUsd?: MicroUsd | undefined;
}

export type RunnerEvent =
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "started";
      readonly runnerId: RunnerId;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "stdout";
      readonly runnerId: RunnerId;
      readonly chunk: string;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "stderr";
      readonly runnerId: RunnerId;
      readonly chunk: string;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "cost";
      readonly runnerId: RunnerId;
      readonly entry: CostLedgerEntry;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "receipt";
      readonly runnerId: RunnerId;
      readonly receiptId: ReceiptId;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "finished";
      readonly runnerId: RunnerId;
      readonly exitCode: number;
      readonly at: string;
    }
  | {
      readonly schemaVersion?: RunnerSchemaVersion | undefined;
      readonly kind: "failed";
      readonly runnerId: RunnerId;
      readonly error: string;
      readonly code?: RunnerFailureCode | undefined;
      readonly at: string;
    };

export type RunnerEventJson = ReturnType<typeof runnerEventToJsonValue>;

const RUNNER_ID_RE = /^run_[A-Za-z0-9_-]{22,128}$/;
const RUNNER_KIND_SET: ReadonlySet<string> = new Set(RUNNER_KIND_VALUES);
const RUNNER_FAILURE_CODE_SET: ReadonlySet<string> = new Set(RUNNER_FAILURE_CODE_VALUES);
const ISO_8601_UTC_RE = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/;

const RUNNER_SPAWN_REQUEST_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "agentId",
  "credential",
  "providerRoute",
  "prompt",
  "model",
  "cwd",
  "taskId",
  "costCeilingMicroUsd",
] as const satisfies readonly (keyof RunnerSpawnRequest)[];
const RUNNER_SPAWN_REQUEST_KEYS: ReadonlySet<string> = new Set(RUNNER_SPAWN_REQUEST_KEYS_TUPLE);

const RUNNER_PROVIDER_ROUTE_KEYS_TUPLE = [
  "credentialScope",
  "providerKind",
] as const satisfies readonly (keyof RunnerProviderRoute)[];
const RUNNER_PROVIDER_ROUTE_KEYS: ReadonlySet<string> = new Set(RUNNER_PROVIDER_ROUTE_KEYS_TUPLE);

const STARTED_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "started" }>)[];
const CHUNK_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "chunk",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "stdout" }>)[];
const COST_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "entry",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "cost" }>)[];
const RECEIPT_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "receiptId",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "receipt" }>)[];
const FINISHED_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "exitCode",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "finished" }>)[];
const FAILED_EVENT_KEYS_TUPLE = [
  "schemaVersion",
  "kind",
  "runnerId",
  "error",
  "code",
  "at",
] as const satisfies readonly (keyof Extract<RunnerEvent, { kind: "failed" }>)[];

const STARTED_EVENT_KEYS: ReadonlySet<string> = new Set(STARTED_EVENT_KEYS_TUPLE);
const CHUNK_EVENT_KEYS: ReadonlySet<string> = new Set(CHUNK_EVENT_KEYS_TUPLE);
const COST_EVENT_KEYS: ReadonlySet<string> = new Set(COST_EVENT_KEYS_TUPLE);
const RECEIPT_EVENT_KEYS: ReadonlySet<string> = new Set(RECEIPT_EVENT_KEYS_TUPLE);
const FINISHED_EVENT_KEYS: ReadonlySet<string> = new Set(FINISHED_EVENT_KEYS_TUPLE);
const FAILED_EVENT_KEYS: ReadonlySet<string> = new Set(FAILED_EVENT_KEYS_TUPLE);

export function asRunnerId(value: string): RunnerId {
  if (!RUNNER_ID_RE.test(value)) {
    throw new Error("not a RunnerId");
  }
  return value as RunnerId;
}

export function isRunnerId(value: unknown): value is RunnerId {
  return typeof value === "string" && RUNNER_ID_RE.test(value);
}

export function isRunnerKind(value: unknown): value is RunnerKind {
  return typeof value === "string" && RUNNER_KIND_SET.has(value);
}

export function isRunnerFailureCode(value: unknown): value is RunnerFailureCode {
  return typeof value === "string" && RUNNER_FAILURE_CODE_SET.has(value);
}

export function runnerSpawnRequestFromJson(value: unknown): RunnerSpawnRequest {
  const record = requireRecord(value, "runnerSpawnRequest");
  assertKnownKeys(record, "runnerSpawnRequest", RUNNER_SPAWN_REQUEST_KEYS);
  const schemaVersion = optionalRunnerSchemaVersion(
    record,
    "schemaVersion",
    "runnerSpawnRequest.schemaVersion",
  );
  const kind = runnerKindFromJson(requiredString(record, "kind", "runnerSpawnRequest.kind"));
  const agentId = agentIdFromJson(requiredString(record, "agentId", "runnerSpawnRequest.agentId"));
  const providerRoute = optionalProviderRoute(
    record,
    "providerRoute",
    "runnerSpawnRequest.providerRoute",
  );
  const prompt = boundedString(
    requiredString(record, "prompt", "runnerSpawnRequest.prompt"),
    "runnerSpawnRequest.prompt",
    MAX_RUNNER_PROMPT_BYTES,
  );
  const model = optionalBoundedString(
    record,
    "model",
    "runnerSpawnRequest.model",
    MAX_RUNNER_MODEL_BYTES,
  );
  const cwd = optionalBoundedString(record, "cwd", "runnerSpawnRequest.cwd", MAX_RUNNER_CWD_BYTES);
  const taskId = optionalTaskId(record, "taskId", "runnerSpawnRequest.taskId");
  const costCeilingMicroUsd = optionalMicroUsd(
    record,
    "costCeilingMicroUsd",
    "runnerSpawnRequest.costCeilingMicroUsd",
  );
  return {
    schemaVersion,
    kind,
    agentId,
    credential: credentialHandleJsonFromJson(
      requiredValue(record, "credential", "runnerSpawnRequest.credential"),
    ),
    ...(providerRoute === undefined ? {} : { providerRoute }),
    prompt,
    ...(model === undefined ? {} : { model }),
    ...(cwd === undefined ? {} : { cwd }),
    ...(taskId === undefined ? {} : { taskId }),
    ...(costCeilingMicroUsd === undefined ? {} : { costCeilingMicroUsd }),
  };
}

export function runnerSpawnRequestToJsonValue(
  request: RunnerSpawnRequest,
): Readonly<Record<string, unknown>> {
  return omitUndefined({
    schemaVersion: RUNNER_SCHEMA_VERSION,
    kind: request.kind,
    agentId: request.agentId,
    credential: request.credential,
    providerRoute:
      request.providerRoute === undefined
        ? undefined
        : {
            credentialScope: request.providerRoute.credentialScope,
            providerKind: request.providerRoute.providerKind,
          },
    prompt: request.prompt,
    model: request.model,
    cwd: request.cwd,
    taskId: request.taskId,
    costCeilingMicroUsd: request.costCeilingMicroUsd,
  });
}

export function runnerEventFromJson(value: unknown): RunnerEvent {
  const record = requireRecord(value, "runnerEvent");
  const kind = requiredString(record, "kind", "runnerEvent.kind");
  switch (kind) {
    case "started":
      assertKnownKeys(record, "runnerEvent", STARTED_EVENT_KEYS);
      return baseEvent(record, kind);
    case "stdout":
    case "stderr":
      assertKnownKeys(record, "runnerEvent", CHUNK_EVENT_KEYS);
      return {
        ...baseEvent(record, kind),
        chunk: boundedString(
          requiredString(record, "chunk", "runnerEvent.chunk"),
          "runnerEvent.chunk",
          MAX_RUNNER_STDIO_CHUNK_BYTES,
        ),
      };
    case "cost":
      assertKnownKeys(record, "runnerEvent", COST_EVENT_KEYS);
      return {
        ...baseEvent(record, kind),
        entry: costAuditPayloadFromJsonValue(
          "cost_event",
          requiredValue(record, "entry", "runnerEvent.entry"),
        ),
      };
    case "receipt":
      assertKnownKeys(record, "runnerEvent", RECEIPT_EVENT_KEYS);
      return {
        ...baseEvent(record, kind),
        receiptId: receiptIdFromJson(requiredString(record, "receiptId", "runnerEvent.receiptId")),
      };
    case "finished":
      assertKnownKeys(record, "runnerEvent", FINISHED_EVENT_KEYS);
      return {
        ...baseEvent(record, kind),
        exitCode: exitCodeFromJson(requiredValue(record, "exitCode", "runnerEvent.exitCode")),
      };
    case "failed":
      assertKnownKeys(record, "runnerEvent", FAILED_EVENT_KEYS);
      return {
        ...baseEvent(record, kind),
        error: boundedString(
          requiredString(record, "error", "runnerEvent.error"),
          "runnerEvent.error",
          MAX_RUNNER_ERROR_BYTES,
        ),
        ...optionalFailureCode(record, "code", "runnerEvent.code"),
      };
    default:
      throw new Error("runnerEvent.kind: unsupported RunnerEvent kind");
  }
}

export function runnerEventToJsonValue(event: RunnerEvent): Readonly<Record<string, unknown>> {
  switch (event.kind) {
    case "started":
      return {
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        at: event.at,
      };
    case "stdout":
    case "stderr":
      return {
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        chunk: event.chunk,
        at: event.at,
      };
    case "cost":
      return {
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        entry: costAuditPayloadToJsonValue("cost_event", event.entry),
        at: event.at,
      };
    case "receipt":
      return {
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        receiptId: event.receiptId,
        at: event.at,
      };
    case "finished":
      return {
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        exitCode: event.exitCode,
        at: event.at,
      };
    case "failed":
      return omitUndefined({
        schemaVersion: RUNNER_SCHEMA_VERSION,
        kind: event.kind,
        runnerId: event.runnerId,
        error: event.error,
        code: event.code,
        at: event.at,
      });
  }
}

function baseEvent<K extends RunnerEvent["kind"]>(
  record: Readonly<Record<string, unknown>>,
  kind: K,
): {
  readonly schemaVersion: RunnerSchemaVersion;
  readonly kind: K;
  readonly runnerId: RunnerId;
  readonly at: string;
} {
  return {
    schemaVersion: optionalRunnerSchemaVersion(
      record,
      "schemaVersion",
      "runnerEvent.schemaVersion",
    ),
    kind,
    runnerId: runnerIdFromJson(requiredString(record, "runnerId", "runnerEvent.runnerId")),
    at: isoUtcFromJson(requiredString(record, "at", "runnerEvent.at"), "runnerEvent.at"),
  };
}

function requiredValue(
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

function requiredString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): string {
  const value = requiredValue(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return value;
}

function optionalBoundedString(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
  maxBytes: number,
): string | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = requiredValue(record, key, path);
  if (typeof value !== "string") {
    throw new Error(`${path}: must be a string`);
  }
  return boundedString(value, path, maxBytes);
}

function boundedString(value: string, path: string, maxBytes: number): string {
  const bytes = Buffer.byteLength(value, "utf8");
  if (bytes > maxBytes) {
    throw new Error(`${path}: exceeds ${maxBytes} UTF-8 bytes`);
  }
  return value;
}

function optionalTaskId(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): TaskId | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = requiredString(record, key, path);
  if (!isTaskId(value)) {
    throw new Error(`${path}: not a TaskId`);
  }
  return asTaskId(value);
}

function optionalRunnerSchemaVersion(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): RunnerSchemaVersion {
  if (!hasOwn(record, key)) return RUNNER_SCHEMA_VERSION;
  const value = requiredValue(record, key, path);
  if (typeof value !== "number" || !Number.isInteger(value)) {
    throw new Error(`${path}: must be an integer`);
  }
  if (value > RUNNER_SCHEMA_VERSION) {
    throw new Error(`${path}: unsupported schemaVersion`);
  }
  if (value !== RUNNER_SCHEMA_VERSION) {
    throw new Error(`${path}: must be ${RUNNER_SCHEMA_VERSION}`);
  }
  return RUNNER_SCHEMA_VERSION;
}

function optionalProviderRoute(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): RunnerProviderRoute | undefined {
  if (!hasOwn(record, key)) return undefined;
  const route = requireRecord(requiredValue(record, key, path), path);
  assertKnownKeys(route, path, RUNNER_PROVIDER_ROUTE_KEYS);
  const credentialScope = requiredString(route, "credentialScope", `${path}.credentialScope`);
  const providerKind = requiredString(route, "providerKind", `${path}.providerKind`);
  if (!isCredentialScope(credentialScope)) {
    throw new Error(`${path}.credentialScope: not a supported CredentialScope`);
  }
  if (!isProviderKind(providerKind)) {
    throw new Error(`${path}.providerKind: not a supported ProviderKind`);
  }
  return {
    credentialScope: asCredentialScope(credentialScope),
    providerKind: asProviderKind(providerKind),
  };
}

function optionalMicroUsd(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): MicroUsd | undefined {
  if (!hasOwn(record, key)) return undefined;
  const value = requiredValue(record, key, path);
  if (!isMicroUsd(value)) {
    throw new Error(`${path}: not a MicroUsd`);
  }
  return value;
}

function optionalFailureCode(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): { readonly code?: RunnerFailureCode | undefined } {
  if (!hasOwn(record, key)) return {};
  const value = requiredString(record, key, path);
  if (!isRunnerFailureCode(value)) {
    throw new Error(`${path}: unsupported RunnerFailureCode`);
  }
  return { code: value };
}

function runnerKindFromJson(value: string): RunnerKind {
  if (!isRunnerKind(value)) {
    throw new Error("runnerSpawnRequest.kind: unsupported RunnerKind");
  }
  return value;
}

function agentIdFromJson(value: string): AgentId {
  try {
    return asAgentId(value);
  } catch (err) {
    throw new Error(
      `runnerSpawnRequest.agentId: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

function runnerIdFromJson(value: string): RunnerId {
  try {
    return asRunnerId(value);
  } catch (err) {
    throw new Error(`runnerEvent.runnerId: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function receiptIdFromJson(value: string): ReceiptId {
  if (/^[0-9A-HJKMNP-TV-Z]{26}$/.test(value)) {
    return value as ReceiptId;
  }
  throw new Error("runnerEvent.receiptId: not a ReceiptId ULID");
}

function isoUtcFromJson(value: string, path: string): string {
  if (!ISO_8601_UTC_RE.test(value)) {
    throw new Error(`${path}: must be an ISO8601 UTC millisecond timestamp`);
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime()) || parsed.toISOString() !== value) {
    throw new Error(`${path}: must be a valid ISO8601 UTC millisecond timestamp`);
  }
  return value;
}

function exitCodeFromJson(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 0 || value > 255) {
    throw new Error("runnerEvent.exitCode: must be an integer from 0 to 255");
  }
  return value;
}

function omitUndefined(input: Record<string, unknown>): Readonly<Record<string, unknown>> {
  const output: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    if (value !== undefined) output[key] = value;
  }
  return output;
}
