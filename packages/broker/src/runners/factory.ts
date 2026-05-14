import { isIP } from "node:net";

import {
  type AgentRunner,
  EndpointNotAllowed,
  ProviderKindMismatch,
  type Receipt,
  RunnerOptionsRequired,
  type SpawnAgentRunner,
} from "@wuphf/agent-runners";
import { CredentialOwnershipMismatch, type CredentialStore } from "@wuphf/credentials";
import type {
  BrokerIdentity,
  CostLedgerEntry,
  CredentialScope,
  ProviderKind,
  RunnerEvent,
  RunnerKind,
  RunnerProviderRoute,
  RunnerSpawnRequest,
} from "@wuphf/protocol";
import {
  asCredentialScope,
  asProviderKind,
  credentialHandleFromJson,
  credentialHandleToJson,
} from "@wuphf/protocol";

import type { AgentProviderRoutingStore } from "../agent-provider-routing/types.ts";
import type { ReceiptStore } from "../receipt-store.ts";

export interface RunnerCostLedger {
  record(entry: CostLedgerEntry): Promise<void>;
}

export interface RunnerEventLog {
  append(event: RunnerEvent): Promise<number>;
}

export interface AgentRunnerFactoryDeps {
  readonly credentialStore: CredentialStore;
  readonly costLedger: RunnerCostLedger;
  readonly receiptStore: ReceiptStore;
  readonly eventLog: RunnerEventLog;
  readonly spawnRunner: SpawnAgentRunner;
  readonly endpointAllowlist?: readonly string[] | undefined;
  readonly agentProviderRoutingStore?: AgentProviderRoutingStore | undefined;
}

export async function createAgentRunnerForBroker(
  request: RunnerSpawnRequest,
  brokerIdentity: BrokerIdentity,
  deps: AgentRunnerFactoryDeps,
): Promise<AgentRunner> {
  // 1. Validate endpoint policy before invoking an OpenAI-compatible runner.
  validateOpenAICompatEndpointForBroker(request, deps.endpointAllowlist ?? []);
  const effectiveProviderRoute =
    request.providerRoute ??
    (await providerRouteFromStore(request, deps.agentProviderRoutingStore));
  const effectiveRequest =
    request.providerRoute === undefined && effectiveProviderRoute !== undefined
      ? { ...request, providerRoute: effectiveProviderRoute }
      : request;
  const credentialScope =
    effectiveProviderRoute?.credentialScope ?? credentialScopeForRunnerKind(request.kind);
  const resolvedProviderKind =
    effectiveProviderRoute?.providerKind ?? runnerKindToProviderKind(request.kind);
  // 2. Validate provider kind against the credential scope resolved for this spawn,
  // AND that the runner kind itself can use that scope. The second check
  // prevents a confused-deputy where, for example, a `claude-cli → openai/openai`
  // route passes the equality check and the claude-cli adapter then exports
  // the OpenAI secret as `ANTHROPIC_API_KEY`.
  if (!isCompatibleRunnerProviderRoute(request.kind, credentialScope, resolvedProviderKind)) {
    throw new ProviderKindMismatch(
      `providerKind ${resolvedProviderKind} / scope ${credentialScope} not compatible with runner kind ${request.kind}`,
    );
  }
  const credential = credentialHandleFromJson(request.credential, {
    broker: brokerIdentity,
    agentId: request.agentId,
    scope: credentialScope,
  });

  return deps.spawnRunner(effectiveRequest, {
    credential,
    resolvedProviderKind,
    // 3. Validate credential ownership immediately before exposing secret material.
    secretReader: async (handle) => {
      const resolved = await deps.credentialStore.readWithOwnership({
        broker: brokerIdentity,
        handleId: credentialHandleToJson(handle).id,
        expectedAgentId: request.agentId,
        expectedScope: credentialScope,
      });
      if (resolved.agentId !== request.agentId || resolved.scope !== credentialScope) {
        throw new CredentialOwnershipMismatch();
      }
      return resolved.secret;
    },
    costLedger: deps.costLedger,
    receiptStore: {
      put: async (receipt: Receipt) => {
        const result = await deps.receiptStore.put(receipt);
        return { stored: !result.existed };
      },
    },
    eventLog: deps.eventLog,
  });
}

async function providerRouteFromStore(
  request: RunnerSpawnRequest,
  store: AgentProviderRoutingStore | undefined,
): Promise<RunnerProviderRoute | undefined> {
  if (store === undefined) return undefined;
  const entry = await store.getEntry(request.agentId, request.kind);
  if (entry === null) return undefined;
  return {
    credentialScope: entry.credentialScope,
    providerKind: entry.providerKind,
  };
}

function validateOpenAICompatEndpointForBroker(
  request: RunnerSpawnRequest,
  endpointAllowlist: readonly string[],
): void {
  if (request.kind !== "openai-compat") return;
  if (request.options?.kind !== "openai-compat") {
    throw new RunnerOptionsRequired("OpenAI-compatible runner requires options.kind=openai-compat");
  }
  const endpoint = request.options.endpoint;
  const endpointUrl = parseEndpointUrl(endpoint, endpointAllowlist);
  if (endpointUrl.protocol !== "https:" && endpointUrl.protocol !== "http:") {
    throw new EndpointNotAllowed(endpoint, endpointAllowlist);
  }
  const exactAllowed = endpointAllowlist.some((entry) => exactOriginMatches(entry, endpointUrl));
  const allowed = endpointAllowlist.some((entry) => allowlistEntryMatches(entry, endpointUrl));
  const dangerous = endpointIsDangerous(endpointUrl);
  if (!allowed || (dangerous && !exactAllowed)) {
    throw new EndpointNotAllowed(endpoint, endpointAllowlist);
  }
}

function parseEndpointUrl(endpoint: string, allowedOrigins: readonly string[]): URL {
  try {
    return new URL(endpoint);
  } catch (error) {
    throw new EndpointNotAllowed(endpoint, allowedOrigins, { cause: error });
  }
}

// Cap allowlist entries before regex construction. The matcher uses bounded
// `[^/]*` (not `.*`) so the immediate ReDoS surface is small, but an
// admin-controlled config containing a wildcard-heavy pattern could still
// degrade match performance. These caps bound both pattern length and
// wildcard count so a malformed allowlist entry fails closed.
const MAX_ALLOWLIST_PATTERN_BYTES = 256;
const MAX_ALLOWLIST_PATTERN_WILDCARDS = 10;

function allowlistEntryMatches(entry: string, endpointUrl: URL): boolean {
  if (!entry.includes("*")) return exactOriginMatches(entry, endpointUrl);
  if (!isAllowlistPatternBounded(entry)) return false;
  return globOriginRegExp(entry).test(endpointUrl.origin);
}

function exactOriginMatches(entry: string, endpointUrl: URL): boolean {
  if (entry.includes("*")) return false;
  if (entry.length > MAX_ALLOWLIST_PATTERN_BYTES) return false;
  try {
    return new URL(entry).origin === endpointUrl.origin;
  } catch {
    return false;
  }
}

function isAllowlistPatternBounded(pattern: string): boolean {
  if (pattern.length > MAX_ALLOWLIST_PATTERN_BYTES) return false;
  let wildcards = 0;
  for (const char of pattern) {
    if (char === "*") {
      wildcards += 1;
      if (wildcards > MAX_ALLOWLIST_PATTERN_WILDCARDS) return false;
    }
  }
  return true;
}

function globOriginRegExp(pattern: string): RegExp {
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replaceAll("*", "[^/]*");
  return new RegExp(`^${escaped}$`, "i");
}

function endpointIsDangerous(endpointUrl: URL): boolean {
  if (endpointUrl.protocol !== "https:") return true;
  const host = normalizedHostname(endpointUrl);
  return hostIsLoopback(host) || hostIsPrivateOrLinkLocal(host);
}

function normalizedHostname(endpointUrl: URL): string {
  return endpointUrl.hostname
    .toLowerCase()
    .replace(/^\[|\]$/g, "")
    .replace(/%.+$/, "");
}

function hostIsLoopback(host: string): boolean {
  if (host === "localhost" || host.endsWith(".localhost")) return true;
  const family = isIP(host);
  if (family === 4) {
    return ipv4Octets(host)?.[0] === 127;
  }
  if (family === 6) {
    const bytes = ipv6Bytes(host);
    if (bytes === null) return false;
    return bytes.slice(0, 15).every((byte) => byte === 0) && bytes[15] === 1;
  }
  return false;
}

function hostIsPrivateOrLinkLocal(host: string): boolean {
  const family = isIP(host);
  if (family === 4) {
    const octets = ipv4Octets(host);
    if (octets === null) return false;
    const [first, second] = octets;
    return (
      first === 10 ||
      (first === 172 && second >= 16 && second <= 31) ||
      (first === 192 && second === 168) ||
      (first === 169 && second === 254)
    );
  }
  if (family === 6) {
    const bytes = ipv6Bytes(host);
    if (bytes === null) return false;
    if (isIpv4Mapped(bytes)) {
      const mapped = bytes.slice(12);
      return ipv4BytesArePrivateOrLinkLocal(mapped) || mapped[0] === 127;
    }
    const first = bytes[0] ?? 0;
    const second = bytes[1] ?? 0;
    return (first === 0xfe && (second & 0xc0) === 0x80) || (first & 0xfe) === 0xfc;
  }
  return false;
}

function ipv4Octets(host: string): readonly [number, number, number, number] | null {
  const parts = host.split(".");
  if (parts.length !== 4) return null;
  const octets = parts.map((part) => Number.parseInt(part, 10));
  if (
    octets.length !== 4 ||
    octets.some((octet, index) => String(octet) !== parts[index] || octet < 0 || octet > 255)
  ) {
    return null;
  }
  return [octets[0] ?? 0, octets[1] ?? 0, octets[2] ?? 0, octets[3] ?? 0];
}

function ipv4BytesArePrivateOrLinkLocal(bytes: readonly number[]): boolean {
  const [first, second] = bytes;
  return (
    first === 10 ||
    (first === 172 && second !== undefined && second >= 16 && second <= 31) ||
    (first === 192 && second === 168) ||
    (first === 169 && second === 254)
  );
}

function ipv6Bytes(host: string): readonly number[] | null {
  const ipv4Tail = host.match(/(.+):(\d+\.\d+\.\d+\.\d+)$/);
  let normalized = host;
  if (ipv4Tail !== null) {
    const prefix = ipv4Tail[1];
    const octets = ipv4Octets(ipv4Tail[2] ?? "");
    if (prefix === undefined || octets === null) return null;
    const groups = octets
      .map((byte) => byte.toString(16).padStart(2, "0"))
      .join("")
      .match(/.{1,4}/g);
    if (groups === null) return null;
    normalized = `${prefix}:${groups.join(":")}`;
  }
  const halves = normalized.split(":".repeat(2));
  if (halves.length > 2) return null;
  const left = parseIpv6Groups(halves[0] ?? "");
  const right = parseIpv6Groups(halves[1] ?? "");
  if (left === null || right === null) return null;
  const missing = 8 - left.length - right.length;
  if (halves.length === 1 && missing !== 0) return null;
  if (halves.length === 2 && missing < 1) return null;
  const groups = [...left, ...Array.from({ length: Math.max(0, missing) }, () => 0), ...right];
  if (groups.length !== 8) return null;
  return groups.flatMap((group) => [(group >> 8) & 0xff, group & 0xff]);
}

function parseIpv6Groups(part: string): readonly number[] | null {
  if (part.length === 0) return [];
  const groups = part.split(":").map((group) => Number.parseInt(group, 16));
  if (groups.some((group) => !Number.isInteger(group) || group < 0 || group > 0xffff)) {
    return null;
  }
  return groups;
}

function isIpv4Mapped(bytes: readonly number[]): boolean {
  return (
    bytes.length === 16 &&
    bytes.slice(0, 10).every((byte) => byte === 0) &&
    bytes[10] === 0xff &&
    bytes[11] === 0xff
  );
}

function credentialScopeForRunnerKind(kind: RunnerKind): CredentialScope {
  switch (kind) {
    case "claude-cli":
      return asCredentialScope("anthropic");
    case "codex-cli":
      return asCredentialScope("openai");
    case "openai-compat":
      return asCredentialScope("openai-compat");
  }
}

function runnerKindToProviderKind(kind: RunnerKind): ProviderKind {
  switch (kind) {
    case "claude-cli":
      return asProviderKind("anthropic");
    case "codex-cli":
      return asProviderKind("openai");
    case "openai-compat":
      return asProviderKind("openai-compat");
  }
}

// Kind → set of credential scopes the adapter actually knows how to use.
// Without this allowlist, `providerKindMatchesCredentialScope` would happily
// accept any (kind, scope, scope) — e.g. `claude-cli → openai/openai` — and
// the claude-cli adapter would unconditionally export the secret as
// `ANTHROPIC_API_KEY` (see packages/agent-runners/src/adapters/claude-cli.ts).
// That misroutes the OpenAI key to Anthropic's endpoint. The matrix mirrors
// each adapter's own env-var dispatch (e.g. codex-cli's `secretEnvVarForScope`).
const SUPPORTED_SCOPES_BY_KIND: Readonly<Record<RunnerKind, readonly string[]>> = {
  "claude-cli": ["anthropic"],
  "codex-cli": ["openai", "openai-compat", "anthropic"],
  "openai-compat": ["openai-compat"],
};

export function isCompatibleRunnerProviderRoute(
  kind: RunnerKind,
  credentialScope: CredentialScope,
  providerKind: ProviderKind,
): boolean {
  if (!providerKindMatchesCredentialScope(credentialScope, providerKind)) return false;
  const supported = SUPPORTED_SCOPES_BY_KIND[kind];
  return supported.includes(String(credentialScope));
}

function providerKindMatchesCredentialScope(
  credentialScope: CredentialScope,
  providerKind: ProviderKind,
): boolean {
  return String(credentialScope) === String(providerKind);
}
