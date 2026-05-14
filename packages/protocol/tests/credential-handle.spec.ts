import { inspect } from "node:util";
import { describe, expect, it } from "vitest";
import { createBrokerIdentityForTesting } from "../src/credential-handle.ts";
import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  BrokerIdentity,
  brokerIdentityAgentId,
  CREDENTIAL_SCOPE_VALUES,
  CredentialHandle,
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
  });

  it("includes provider kinds as credential scopes", () => {
    for (const providerKind of PROVIDER_KIND_VALUES) {
      expect(CREDENTIAL_SCOPE_VALUES).toContain(providerKind);
      expect(isCredentialScope(providerKind)).toBe(true);
    }
  });
});
