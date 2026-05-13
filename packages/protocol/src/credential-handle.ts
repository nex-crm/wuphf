import type { Brand } from "./brand.ts";
import { PROVIDER_KIND_VALUES } from "./receipt-types.ts";

export const CREDENTIAL_SCOPE_VALUES = [...PROVIDER_KIND_VALUES, "github"] as const;

export type AgentId = Brand<string, "AgentId">;
export type CredentialHandleId = Brand<string, "CredentialHandleId">;
export type CredentialScope = Brand<(typeof CREDENTIAL_SCOPE_VALUES)[number], "CredentialScope">;

export type CredentialHandle = Brand<
  {
    readonly id: CredentialHandleId;
    readonly agentId: AgentId;
    readonly scope: CredentialScope;
    toJSON(): CredentialHandleJson;
    toString(): string;
  },
  "CredentialHandle"
>;

export interface CredentialHandleJson {
  readonly id: CredentialHandleId;
}

const AGENT_ID_RE = /^[a-z0-9][a-z0-9_-]{0,127}$/;
const CREDENTIAL_HANDLE_ID_RE = /^cred_[A-Za-z0-9_-]{22,128}$/;
const CREDENTIAL_SCOPE_SET: ReadonlySet<string> = new Set(CREDENTIAL_SCOPE_VALUES);
const NODE_INSPECT_CUSTOM = Symbol.for("nodejs.util.inspect.custom");

export function asAgentId(value: string): AgentId {
  if (!AGENT_ID_RE.test(value)) throw new Error("not an AgentId");
  return value as AgentId;
}

export function isAgentId(value: unknown): value is AgentId {
  return typeof value === "string" && AGENT_ID_RE.test(value);
}

export function asCredentialHandleId(value: string): CredentialHandleId {
  if (!CREDENTIAL_HANDLE_ID_RE.test(value)) {
    throw new Error("not a CredentialHandleId");
  }
  return value as CredentialHandleId;
}

export function isCredentialHandleId(value: unknown): value is CredentialHandleId {
  return typeof value === "string" && CREDENTIAL_HANDLE_ID_RE.test(value);
}

export function asCredentialScope(value: string): CredentialScope {
  if (!CREDENTIAL_SCOPE_SET.has(value)) throw new Error("not a supported CredentialScope");
  return value as CredentialScope;
}

export function isCredentialScope(value: unknown): value is CredentialScope {
  return typeof value === "string" && CREDENTIAL_SCOPE_SET.has(value);
}

export function createCredentialHandle(input: {
  readonly id: CredentialHandleId;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): CredentialHandle {
  return new OpaqueCredentialHandle(
    input.id,
    input.agentId,
    input.scope,
  ) as unknown as CredentialHandle;
}

export function isCredentialHandle(value: unknown): value is CredentialHandle {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as {
    readonly id?: unknown;
    readonly agentId?: unknown;
    readonly scope?: unknown;
    readonly toJSON?: unknown;
    readonly toString?: unknown;
  };
  return (
    isCredentialHandleId(candidate.id) &&
    isAgentId(candidate.agentId) &&
    isCredentialScope(candidate.scope) &&
    typeof candidate.toJSON === "function" &&
    typeof candidate.toString === "function"
  );
}

class OpaqueCredentialHandle {
  constructor(
    readonly id: CredentialHandleId,
    readonly agentId: AgentId,
    readonly scope: CredentialScope,
  ) {}

  toJSON(): CredentialHandleJson {
    return { id: this.id };
  }

  toString(): string {
    return "CredentialHandle(<redacted>)";
  }

  [NODE_INSPECT_CUSTOM](): string {
    return "CredentialHandle { id: <redacted> }";
  }
}
