import { inspect } from "node:util";
import { describe, expect, it } from "vitest";
import { createBrokerIdentityForTesting } from "../src/credential-handle.ts";
import {
  type AgentId,
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  BrokerIdentity,
  brokerIdentityAgentId,
  CREDENTIAL_SCOPE_VALUES,
  CredentialHandle,
  type CredentialHandleId,
  type CredentialScope,
  createCredentialHandle,
  credentialHandleFromJson,
  credentialHandleJsonFromJson,
  credentialHandleToJson,
  isAgentId,
  isBrokerIdentity,
  isCredentialHandle,
  isCredentialHandleId,
  isCredentialScope,
  PROVIDER_KIND_VALUES,
} from "../src/index.ts";
import credentialHandleVectors from "../testdata/credential-handle-vectors.json";

interface CredentialHandleVector {
  readonly name: string;
  readonly input: {
    readonly agentId: string;
    readonly scope: string;
    readonly json: {
      readonly version: 1;
      readonly id: string;
    };
  };
  readonly expected: {
    readonly string: string;
    readonly inspect: string;
  };
}

describe("CredentialHandle", () => {
  const agentId = asAgentId("agent_alpha");
  const otherAgentId = asAgentId("agent_beta");
  const scope = asCredentialScope("openai");
  const id = asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV");
  const fixtureSecret = "fixture-secret-value-do-not-use-0000";
  const broker = createBrokerIdentityForTesting({ agentId });

  function credentialHandleRecordWithAccessor(key: "version" | "id"): Record<string, unknown> {
    const record: Record<string, unknown> = key === "version" ? { id } : { version: 1 };
    Object.defineProperty(record, key, {
      enumerable: true,
      get() {
        throw new Error("accessor should not be invoked");
      },
    });
    return record;
  }

  it("serializes only the versioned opaque handle id", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(JSON.stringify(handle)).toBe(JSON.stringify({ version: 1, id }));
    expect(credentialHandleToJson(handle)).toEqual({ version: 1, id });
    expect(credentialHandleJsonFromJson({ version: 1, id })).toEqual({ version: 1, id });
    expect(JSON.stringify(handle)).not.toContain(fixtureSecret);
    expect(JSON.stringify(handle)).not.toContain(agentId);
    expect(JSON.stringify(handle)).not.toContain(scope);
  });

  it("keeps private slots out of spreads and object enumeration", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect({ ...handle }).toEqual({});
    expect(Object.assign({}, handle)).toEqual({});
    expect(Object.keys(handle)).toEqual([]);
    expect("id" in handle).toBe(false);
    expect("agentId" in handle).toBe(false);
    expect("scope" in handle).toBe(false);
  });

  it("redacts string, primitive, and util.inspect output", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(String(handle)).toBe("CredentialHandle(<redacted>)");
    expect(`${handle}`).toBe("CredentialHandle(<redacted>)");
    expect(String(handle)).not.toContain(fixtureSecret);
    expect(String(handle)).not.toContain(id);

    const inspected = inspect(handle);
    expect(inspected).toBe("CredentialHandle { id: <redacted> }");
    expect(inspected).not.toContain(fixtureSecret);
    expect(inspected).not.toContain(id);
  });

  it("structuredClone produces a redacted non-handle plain object", () => {
    const handle = createCredentialHandle({ id, agentId, scope });
    const cloned = structuredClone(handle);

    expect(cloned).toEqual({});
    expect(Object.getPrototypeOf(cloned)).toBe(Object.prototype);
    expect(isCredentialHandle(cloned)).toBe(false);
    expect(cloned).not.toBeInstanceOf(CredentialHandle);
  });

  it("keeps broker identities redacted and opaque", () => {
    const cloned = structuredClone(broker);

    expect(String(broker)).toBe("BrokerIdentity(<redacted>)");
    expect(`${broker}`).toBe("BrokerIdentity(<redacted>)");
    expect(inspect(broker)).toBe("BrokerIdentity { agentId: <redacted> }");
    expect({ ...broker }).toEqual({});
    expect(Object.assign({}, broker)).toEqual({});
    expect(isBrokerIdentity(cloned)).toBe(false);
  });

  it("exposes trusted handle context only through static accessors", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(CredentialHandle.agentId(handle)).toBe(agentId);
    expect(CredentialHandle.scope(handle)).toBe(scope);
  });

  it("round-trips golden fixture handles through broker-trusted context", () => {
    const vectors = credentialHandleVectors.vectors as readonly CredentialHandleVector[];

    for (const vector of vectors) {
      const vectorAgentId = asAgentId(vector.input.agentId);
      const vectorScope = asCredentialScope(vector.input.scope);
      const vectorBroker = createBrokerIdentityForTesting({ agentId: vectorAgentId });
      const handle = credentialHandleFromJson(vector.input.json, {
        broker: vectorBroker,
        agentId: vectorAgentId,
        scope: vectorScope,
      });

      expect(credentialHandleToJson(handle), vector.name).toEqual(vector.input.json);
      expect(String(handle), vector.name).toBe(vector.expected.string);
      expect(inspect(handle), vector.name).toBe(vector.expected.inspect);
    }
  });

  it("refuses to rehydrate a handle when the broker identity does not match", () => {
    const json = { version: 1, id };
    const otherBroker = createBrokerIdentityForTesting({ agentId: otherAgentId });

    expect(() => credentialHandleFromJson(json, { broker, agentId, scope })).not.toThrow();
    expect(() => credentialHandleFromJson(json, { broker: otherBroker, agentId, scope })).toThrow(
      /agentId mismatch/,
    );
  });

  it("rejects handle rehydration without valid trusted context", () => {
    const json = { version: 1, id };

    expect(() =>
      credentialHandleFromJson(json, { broker: {} as BrokerIdentity, agentId, scope }),
    ).toThrow(/BrokerIdentity is required/);
    expect(() =>
      credentialHandleFromJson(json, { broker, agentId: "Agent Alpha" as AgentId, scope }),
    ).toThrow(/credentialHandleFromJson.agentId: not an AgentId/);
    expect(() =>
      credentialHandleFromJson(json, { broker, agentId, scope: "unsupported" as CredentialScope }),
    ).toThrow(/credentialHandleFromJson.scope: not a CredentialScope/);
  });

  it("rejects forged runtime credential objects", () => {
    expect(() => brokerIdentityAgentId({} as BrokerIdentity)).toThrow(/not a BrokerIdentity/);
    expect(() => credentialHandleToJson({} as CredentialHandle)).toThrow(/not a CredentialHandle/);
  });

  it("guards internal constructors and constructor inputs", () => {
    expect(
      () =>
        new BrokerIdentity(Symbol("external") as never, {
          agentId,
          revocationToken: Symbol("external"),
        }),
    ).toThrow(/constructor is internal/);
    expect(() => createBrokerIdentityForTesting({ agentId: "Agent Alpha" as AgentId })).toThrow(
      /BrokerIdentity.agentId: not an AgentId/,
    );
    expect(() => new CredentialHandle(Symbol("external") as never, { id, agentId, scope })).toThrow(
      /constructor is internal/,
    );
    expect(() =>
      createCredentialHandle({ id: "cred_short" as CredentialHandleId, agentId, scope }),
    ).toThrow(/CredentialHandle.id: not a CredentialHandleId/);
    expect(() => createCredentialHandle({ id, agentId: "Agent Alpha" as AgentId, scope })).toThrow(
      /CredentialHandle.agentId: not an AgentId/,
    );
    expect(() =>
      createCredentialHandle({ id, agentId, scope: "unsupported" as CredentialScope }),
    ).toThrow(/CredentialHandle.scope: not a CredentialScope/);
  });

  it("guards branded ids, broker identities, and closed credential scopes", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(isCredentialHandle(handle)).toBe(true);
    expect(isBrokerIdentity(broker)).toBe(true);
    expect(brokerIdentityAgentId(broker)).toBe(agentId);
    expect(broker).toBeInstanceOf(BrokerIdentity);
    expect(isAgentId("agent_alpha")).toBe(true);
    expect(isAgentId("Agent Alpha")).toBe(false);
    expect(isCredentialHandleId(id)).toBe(true);
    expect(isCredentialHandleId("cred_short")).toBe(false);
    expect(isCredentialScope("github")).toBe(true);
    expect(isCredentialScope("unsupported")).toBe(false);
    expect(() => asAgentId("Agent Alpha")).toThrow(/not an AgentId/);
    expect(() => asCredentialHandleId("cred_short")).toThrow(/not a CredentialHandleId/);
    expect(() => asCredentialScope("unsupported")).toThrow(/not a supported CredentialScope/);
  });

  it("includes provider kinds as credential scopes", () => {
    for (const providerKind of PROVIDER_KIND_VALUES) {
      expect(CREDENTIAL_SCOPE_VALUES).toContain(providerKind);
      expect(isCredentialScope(providerKind)).toBe(true);
    }
  });

  it.each([
    ["non-object", null, /credentialHandle: must be an object/],
    ["unknown key", { version: 1, id, extra: true }, /credentialHandle\/extra: is not allowed/],
    ["missing version", { id }, /credentialHandle.version: is required/],
    [
      "accessor version",
      credentialHandleRecordWithAccessor("version"),
      /credentialHandle.version: must be a data property/,
    ],
    ["undefined version", { version: undefined, id }, /credentialHandle.version: is required/],
    ["wrong version", { version: 2, id }, /credentialHandle.version: must be 1/],
    ["missing id", { version: 1 }, /credentialHandle.id: is required/],
    [
      "accessor id",
      credentialHandleRecordWithAccessor("id"),
      /credentialHandle.id: must be a data property/,
    ],
    ["undefined id", { version: 1, id: undefined }, /credentialHandle.id: is required/],
    ["non-string id", { version: 1, id: 42 }, /credentialHandle.id: must be a string/],
    ["invalid id", { version: 1, id: "cred_short" }, /not a CredentialHandleId/],
  ])("rejects malformed handle JSON: %s", (_name, json, error) => {
    expect(() => credentialHandleJsonFromJson(json)).toThrow(error);
  });
});
