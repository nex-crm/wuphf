import { isIP } from "node:net";

import {
  type AgentRunner,
  EndpointNotAllowed,
  type Receipt,
  RunnerOptionsRequired,
  type SpawnAgentRunner,
} from "@wuphf/agent-runners";
import { CredentialOwnershipMismatch, type CredentialStore } from "@wuphf/credentials";
import type {
  BrokerIdentity,
  CostLedgerEntry,
  CredentialScope,
  RunnerEvent,
  RunnerKind,
  RunnerSpawnRequest,
} from "@wuphf/protocol";
import {
  asCredentialScope,
  credentialHandleFromJson,
  credentialHandleToJson,
} from "@wuphf/protocol";

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
  readonly endpointAllowlist?: readonly string[] | undefined; // pass-z1
}

export async function createAgentRunnerForBroker(
  request: RunnerSpawnRequest,
  brokerIdentity: BrokerIdentity,
  deps: AgentRunnerFactoryDeps,
): Promise<AgentRunner> {
  validateOpenAICompatEndpointForBroker(request, deps.endpointAllowlist ?? []); // pass-z1
  const credentialScope =
    request.providerRoute?.credentialScope ?? credentialScopeForRunnerKind(request.kind);
  const credential = credentialHandleFromJson(request.credential, {
    broker: brokerIdentity,
    agentId: request.agentId,
    scope: credentialScope,
  });

  return deps.spawnRunner(request, {
    credential,
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

function validateOpenAICompatEndpointForBroker(
  // pass-z1
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

function allowlistEntryMatches(entry: string, endpointUrl: URL): boolean {
  if (!entry.includes("*")) return exactOriginMatches(entry, endpointUrl);
  return globOriginRegExp(entry).test(endpointUrl.origin);
}

function exactOriginMatches(entry: string, endpointUrl: URL): boolean {
  if (entry.includes("*")) return false;
  try {
    return new URL(entry).origin === endpointUrl.origin;
  } catch {
    return false;
  }
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
  const halves = normalized.split("::");
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
