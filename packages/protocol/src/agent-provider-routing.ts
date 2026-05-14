// Branch 10 — per-agent provider routing.
//
// The wire shape that lets a host configure "agent X, runner kind Y →
// credential scope Z, provider kind W". When a RunnerSpawnRequest arrives
// without an inline `providerRoute`, the broker consults this config and
// fills it in before reaching @wuphf/agent-runners.
//
// The single-entry shape `RunnerProviderRoute` lives in runner.ts and is
// reused verbatim; this module adds the persistence + IPC surface that lets
// a UI or admin tool persist routing per agent.

import {
  type AgentId,
  asAgentId,
  asCredentialScope,
  type CredentialScope,
} from "./credential-handle.ts";
import {
  asProviderKind,
  isProviderKind,
  PROVIDER_KIND_VALUES,
  type ProviderKind,
} from "./receipt.ts";
import { assertKnownKeys, hasOwn, requireRecord } from "./receipt-utils.ts";
import { isRunnerKind, RUNNER_KIND_VALUES, type RunnerKind } from "./runner.ts";

/**
 * One agent can configure at most one route per RunnerKind. The cap
 * exists so a hostile renderer cannot push an unbounded list and so the
 * broker's resolution path is O(1) per spawn. Sized to RunnerKind cardinality
 * with substantial headroom for future kinds added by branches 11+.
 */
export const MAX_AGENT_PROVIDER_ROUTES = 16;

export interface AgentProviderRoutingEntry {
  readonly kind: RunnerKind;
  readonly credentialScope: CredentialScope;
  readonly providerKind: ProviderKind;
}

export interface AgentProviderRouting {
  readonly agentId: AgentId;
  readonly routes: readonly AgentProviderRoutingEntry[];
}

export interface AgentProviderRoutingReadRequest {
  readonly agentId: AgentId;
}

export type AgentProviderRoutingReadResponse = AgentProviderRouting;

export interface AgentProviderRoutingWriteRequest {
  readonly agentId: AgentId;
  readonly routes: readonly AgentProviderRoutingEntry[];
}

export interface AgentProviderRoutingWriteResponse {
  readonly applied: true;
}

const AGENT_PROVIDER_ROUTING_ENTRY_KEYS_TUPLE = [
  "kind",
  "credentialScope",
  "providerKind",
] as const satisfies readonly (keyof AgentProviderRoutingEntry)[];
const AGENT_PROVIDER_ROUTING_ENTRY_KEYS: ReadonlySet<string> = new Set(
  AGENT_PROVIDER_ROUTING_ENTRY_KEYS_TUPLE,
);

const AGENT_PROVIDER_ROUTING_KEYS_TUPLE = [
  "agentId",
  "routes",
] as const satisfies readonly (keyof AgentProviderRouting)[];
const AGENT_PROVIDER_ROUTING_KEYS: ReadonlySet<string> = new Set(AGENT_PROVIDER_ROUTING_KEYS_TUPLE);

const AGENT_PROVIDER_ROUTING_READ_REQUEST_KEYS_TUPLE = [
  "agentId",
] as const satisfies readonly (keyof AgentProviderRoutingReadRequest)[];
const AGENT_PROVIDER_ROUTING_READ_REQUEST_KEYS: ReadonlySet<string> = new Set(
  AGENT_PROVIDER_ROUTING_READ_REQUEST_KEYS_TUPLE,
);

const AGENT_PROVIDER_ROUTING_WRITE_REQUEST_KEYS_TUPLE = [
  "agentId",
  "routes",
] as const satisfies readonly (keyof AgentProviderRoutingWriteRequest)[];
const AGENT_PROVIDER_ROUTING_WRITE_REQUEST_KEYS: ReadonlySet<string> = new Set(
  AGENT_PROVIDER_ROUTING_WRITE_REQUEST_KEYS_TUPLE,
);

const AGENT_PROVIDER_ROUTING_WRITE_RESPONSE_KEYS_TUPLE = [
  "applied",
] as const satisfies readonly (keyof AgentProviderRoutingWriteResponse)[];
const AGENT_PROVIDER_ROUTING_WRITE_RESPONSE_KEYS: ReadonlySet<string> = new Set(
  AGENT_PROVIDER_ROUTING_WRITE_RESPONSE_KEYS_TUPLE,
);

export function agentProviderRoutingFromJson(value: unknown): AgentProviderRouting {
  const record = requireRecord(value, "agentProviderRouting");
  assertKnownKeys(record, "agentProviderRouting", AGENT_PROVIDER_ROUTING_KEYS);
  const agentId = agentIdFromJson(
    requiredStringField(record, "agentId", "agentProviderRouting.agentId"),
    "agentProviderRouting.agentId",
  );
  const routes = parseRoutesArray(
    requiredField(record, "routes", "agentProviderRouting.routes"),
    "agentProviderRouting.routes",
  );
  return { agentId, routes };
}

export function agentProviderRoutingToJsonValue(value: AgentProviderRouting): unknown {
  return {
    agentId: value.agentId as string,
    routes: value.routes.map((entry) => agentProviderRoutingEntryToJsonValue(entry)),
  };
}

export function agentProviderRoutingEntryFromJson(
  value: unknown,
  path: string,
): AgentProviderRoutingEntry {
  const record = requireRecord(value, path);
  assertKnownKeys(record, path, AGENT_PROVIDER_ROUTING_ENTRY_KEYS);
  const kind = requiredStringField(record, "kind", `${path}.kind`);
  if (!isRunnerKind(kind)) {
    throw new Error(`${path}.kind: not a supported RunnerKind (got ${JSON.stringify(kind)})`);
  }
  const credentialScope = credentialScopeFromJson(
    requiredStringField(record, "credentialScope", `${path}.credentialScope`),
    `${path}.credentialScope`,
  );
  const providerKindRaw = requiredStringField(record, "providerKind", `${path}.providerKind`);
  if (!isProviderKind(providerKindRaw)) {
    throw new Error(
      `${path}.providerKind: not a supported ProviderKind (got ${JSON.stringify(providerKindRaw)})`,
    );
  }
  return {
    kind: kind as RunnerKind,
    credentialScope,
    providerKind: asProviderKind(providerKindRaw),
  };
}

export function agentProviderRoutingEntryToJsonValue(value: AgentProviderRoutingEntry): unknown {
  return {
    kind: value.kind as string,
    credentialScope: value.credentialScope as string,
    providerKind: value.providerKind as string,
  };
}

export function agentProviderRoutingReadRequestFromJson(
  value: unknown,
): AgentProviderRoutingReadRequest {
  const record = requireRecord(value, "agentProviderRoutingReadRequest");
  assertKnownKeys(
    record,
    "agentProviderRoutingReadRequest",
    AGENT_PROVIDER_ROUTING_READ_REQUEST_KEYS,
  );
  return {
    agentId: agentIdFromJson(
      requiredStringField(record, "agentId", "agentProviderRoutingReadRequest.agentId"),
      "agentProviderRoutingReadRequest.agentId",
    ),
  };
}

export function agentProviderRoutingWriteRequestFromJson(
  value: unknown,
): AgentProviderRoutingWriteRequest {
  const record = requireRecord(value, "agentProviderRoutingWriteRequest");
  assertKnownKeys(
    record,
    "agentProviderRoutingWriteRequest",
    AGENT_PROVIDER_ROUTING_WRITE_REQUEST_KEYS,
  );
  return {
    agentId: agentIdFromJson(
      requiredStringField(record, "agentId", "agentProviderRoutingWriteRequest.agentId"),
      "agentProviderRoutingWriteRequest.agentId",
    ),
    routes: parseRoutesArray(
      requiredField(record, "routes", "agentProviderRoutingWriteRequest.routes"),
      "agentProviderRoutingWriteRequest.routes",
    ),
  };
}

export function agentProviderRoutingWriteResponseFromJson(
  value: unknown,
): AgentProviderRoutingWriteResponse {
  const record = requireRecord(value, "agentProviderRoutingWriteResponse");
  assertKnownKeys(
    record,
    "agentProviderRoutingWriteResponse",
    AGENT_PROVIDER_ROUTING_WRITE_RESPONSE_KEYS,
  );
  const applied = requiredField(record, "applied", "agentProviderRoutingWriteResponse.applied");
  if (applied !== true) {
    throw new Error("agentProviderRoutingWriteResponse.applied: must be true");
  }
  return { applied };
}

function parseRoutesArray(value: unknown, path: string): readonly AgentProviderRoutingEntry[] {
  if (!Array.isArray(value)) {
    throw new Error(`${path}: must be an array`);
  }
  if (value.length > MAX_AGENT_PROVIDER_ROUTES) {
    throw new Error(`${path}: exceeds ${MAX_AGENT_PROVIDER_ROUTES} entries (got ${value.length})`);
  }
  const seenKinds = new Set<RunnerKind>();
  const entries: AgentProviderRoutingEntry[] = [];
  for (let index = 0; index < value.length; index += 1) {
    const entry = agentProviderRoutingEntryFromJson(value[index], `${path}/${index}`);
    if (seenKinds.has(entry.kind)) {
      throw new Error(`${path}/${index}.kind: duplicate route for kind "${entry.kind}"`);
    }
    seenKinds.add(entry.kind);
    entries.push(entry);
  }
  // Stable sort by RunnerKind enum order so the on-wire byte layout is
  // deterministic regardless of caller insertion order. Equality comparisons
  // by JSON.stringify (used in tests and golden vectors) require this.
  const kindOrder = new Map(RUNNER_KIND_VALUES.map((k, i) => [k, i] as const));
  entries.sort((a, b) => (kindOrder.get(a.kind) ?? 0) - (kindOrder.get(b.kind) ?? 0));
  return Object.freeze(entries);
}

function requiredField(record: Readonly<Record<string, unknown>>, key: string, path: string) {
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

function requiredStringField(
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

function agentIdFromJson(value: string, path: string): AgentId {
  try {
    return asAgentId(value);
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

// Re-export PROVIDER_KIND_VALUES so the demo/tests can pin against the same
// canonical list this module validates against without a separate import.
export { PROVIDER_KIND_VALUES };
