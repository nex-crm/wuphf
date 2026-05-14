import type { Brand } from "./brand.ts";
import { PROVIDER_KIND_VALUES } from "./receipt-types.ts";
import { assertKnownKeys, hasOwn, requireRecord } from "./receipt-utils.ts";

export const CREDENTIAL_SCOPE_VALUES = [...PROVIDER_KIND_VALUES, "github"] as const;

export type AgentId = Brand<string, "AgentId">;
export type CredentialHandleId = Brand<string, "CredentialHandleId">;
export type CredentialScope = Brand<(typeof CREDENTIAL_SCOPE_VALUES)[number], "CredentialScope">;

export interface CredentialHandleJson {
  readonly version: 1;
  readonly id: CredentialHandleId;
}

export interface CredentialHandleFromJsonContext {
  readonly broker: BrokerIdentity;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}

const AGENT_ID_RE = /^[a-z0-9][a-z0-9_-]{0,127}$/;
const CREDENTIAL_HANDLE_ID_RE = /^cred_[A-Za-z0-9_-]{22,128}$/;
const CREDENTIAL_SCOPE_SET: ReadonlySet<string> = new Set(CREDENTIAL_SCOPE_VALUES);
const CREDENTIAL_HANDLE_JSON_KEYS_TUPLE = [
  "version",
  "id",
] as const satisfies readonly (keyof CredentialHandleJson)[];
const CREDENTIAL_HANDLE_JSON_KEYS: ReadonlySet<string> = new Set(CREDENTIAL_HANDLE_JSON_KEYS_TUPLE);
const CREDENTIAL_HANDLE_CONSTRUCTOR_SECRET = Symbol("wuphf.credential-handle.constructor");
const BROKER_IDENTITY_CONSTRUCTOR_SECRET = Symbol("wuphf.broker-identity.constructor");
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

export class BrokerIdentity {
  #agentId: AgentId;
  #revocationToken: symbol;

  constructor(
    secret: typeof BROKER_IDENTITY_CONSTRUCTOR_SECRET,
    input: {
      readonly agentId: AgentId;
      readonly revocationToken: symbol;
    },
  ) {
    if (secret !== BROKER_IDENTITY_CONSTRUCTOR_SECRET) {
      throw new Error("BrokerIdentity constructor is internal");
    }
    if (!isAgentId(input.agentId)) {
      throw new Error("BrokerIdentity.agentId: not an AgentId");
    }
    this.#agentId = input.agentId;
    this.#revocationToken = input.revocationToken;
    Object.freeze(this);
  }

  static hasSlots(value: unknown): value is BrokerIdentity {
    return typeof value === "object" && value !== null && #agentId in value;
  }

  static agentId(identity: BrokerIdentity): AgentId {
    return identity.#agentId;
  }

  toString(): string {
    return "BrokerIdentity(<redacted>)";
  }

  [Symbol.toPrimitive](): string {
    return this.toString();
  }

  [NODE_INSPECT_CUSTOM](): string {
    return "BrokerIdentity { agentId: <redacted> }";
  }
}

Object.freeze(BrokerIdentity.prototype);
Object.freeze(BrokerIdentity);

export class CredentialHandle {
  #id: CredentialHandleId;
  #agentId: AgentId;
  #scope: CredentialScope;

  constructor(
    secret: typeof CREDENTIAL_HANDLE_CONSTRUCTOR_SECRET,
    input: {
      readonly id: CredentialHandleId;
      readonly agentId: AgentId;
      readonly scope: CredentialScope;
    },
  ) {
    if (secret !== CREDENTIAL_HANDLE_CONSTRUCTOR_SECRET) {
      throw new Error("CredentialHandle constructor is internal");
    }
    if (!isCredentialHandleId(input.id)) {
      throw new Error("CredentialHandle.id: not a CredentialHandleId");
    }
    if (!isAgentId(input.agentId)) {
      throw new Error("CredentialHandle.agentId: not an AgentId");
    }
    if (!isCredentialScope(input.scope)) {
      throw new Error("CredentialHandle.scope: not a CredentialScope");
    }
    this.#id = input.id;
    this.#agentId = input.agentId;
    this.#scope = input.scope;
    Object.freeze(this);
  }

  static hasSlots(value: unknown): value is CredentialHandle {
    return typeof value === "object" && value !== null && #id in value;
  }

  static toJson(handle: CredentialHandle): CredentialHandleJson {
    return { version: 1, id: handle.#id };
  }

  static agentId(handle: CredentialHandle): AgentId {
    return handle.#agentId;
  }

  static scope(handle: CredentialHandle): CredentialScope {
    return handle.#scope;
  }

  toJSON(): CredentialHandleJson {
    return CredentialHandle.toJson(this);
  }

  toString(): string {
    return "CredentialHandle(<redacted>)";
  }

  [Symbol.toPrimitive](): string {
    return this.toString();
  }

  [NODE_INSPECT_CUSTOM](): string {
    return "CredentialHandle { id: <redacted> }";
  }
}

Object.freeze(CredentialHandle.prototype);
Object.freeze(CredentialHandle);

export function createCredentialHandle(input: {
  readonly id: CredentialHandleId;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): CredentialHandle {
  return new CredentialHandle(CREDENTIAL_HANDLE_CONSTRUCTOR_SECRET, input);
}

export function isCredentialHandle(value: unknown): value is CredentialHandle {
  return CredentialHandle.hasSlots(value);
}

export function isBrokerIdentity(value: unknown): value is BrokerIdentity {
  return BrokerIdentity.hasSlots(value);
}

export function brokerIdentityAgentId(identity: BrokerIdentity): AgentId {
  if (!isBrokerIdentity(identity)) {
    throw new Error("not a BrokerIdentity");
  }
  return BrokerIdentity.agentId(identity);
}

export function credentialHandleToJson(handle: CredentialHandle): CredentialHandleJson {
  if (!isCredentialHandle(handle)) {
    throw new Error("not a CredentialHandle");
  }
  return CredentialHandle.toJson(handle);
}

export function credentialHandleFromJson(
  json: unknown,
  context: CredentialHandleFromJsonContext,
): CredentialHandle {
  if (!isBrokerIdentity(context.broker)) {
    throw new Error("credentialHandleFromJson: BrokerIdentity is required");
  }
  if (!isAgentId(context.agentId)) {
    throw new Error("credentialHandleFromJson.agentId: not an AgentId");
  }
  if (!isCredentialScope(context.scope)) {
    throw new Error("credentialHandleFromJson.scope: not a CredentialScope");
  }
  if (brokerIdentityAgentId(context.broker) !== context.agentId) {
    throw new Error("credentialHandleFromJson: BrokerIdentity agentId mismatch");
  }
  return createCredentialHandle({
    id: credentialHandleJsonFromJson(json).id,
    agentId: context.agentId,
    scope: context.scope,
  });
}

export function credentialHandleJsonFromJson(json: unknown): CredentialHandleJson {
  const record = requireRecord(json, "credentialHandle");
  assertKnownKeys(record, "credentialHandle", CREDENTIAL_HANDLE_JSON_KEYS);
  const version = requiredDataProperty(record, "version", "credentialHandle.version");
  if (version !== 1) {
    throw new Error("credentialHandle.version: must be 1");
  }
  const id = requiredDataProperty(record, "id", "credentialHandle.id");
  if (typeof id !== "string") {
    throw new Error("credentialHandle.id: must be a string");
  }
  return { version, id: asCredentialHandleId(id) };
}

export function createBrokerIdentityForTesting(input: {
  readonly agentId: AgentId;
  readonly revocationToken?: symbol | undefined;
}): BrokerIdentity {
  return new BrokerIdentity(BROKER_IDENTITY_CONSTRUCTOR_SECRET, {
    agentId: input.agentId,
    revocationToken: input.revocationToken ?? Symbol("wuphf.broker-identity.testing"),
  });
}

function requiredDataProperty(
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
