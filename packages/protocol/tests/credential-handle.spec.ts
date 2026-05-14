import { inspect } from "node:util";
import { describe, expect, it } from "vitest";
import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  CREDENTIAL_SCOPE_VALUES,
  createCredentialHandle,
  isAgentId,
  isCredentialHandle,
  isCredentialHandleId,
  isCredentialScope,
  PROVIDER_KIND_VALUES,
} from "../src/index.ts";

describe("CredentialHandle", () => {
  const agentId = asAgentId("agent_alpha");
  const scope = asCredentialScope("openai");
  const id = asCredentialHandleId("cred_0123456789ABCDEFGHIJKLMNOPQRSTUV");
  const fixtureSecret = "fixture-secret-value-do-not-use-0000";

  it("serializes only the opaque handle id", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(JSON.stringify(handle)).toBe(JSON.stringify({ id }));
    expect(JSON.stringify(handle)).not.toContain(fixtureSecret);
    expect(JSON.stringify(handle)).not.toContain(agentId);
    expect(JSON.stringify(handle)).not.toContain(scope);
  });

  it("redacts string and util.inspect output", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(String(handle)).toBe("CredentialHandle(<redacted>)");
    expect(String(handle)).not.toContain(fixtureSecret);
    expect(String(handle)).not.toContain(id);

    const inspected = inspect(handle);
    expect(inspected).toBe("CredentialHandle { id: <redacted> }");
    expect(inspected).not.toContain(fixtureSecret);
    expect(inspected).not.toContain(id);
  });

  it("guards branded ids and closed credential scopes", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(isCredentialHandle(handle)).toBe(true);
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

  // TODO(batch-b): unskip when the redesign moves handles to private slots.
  it.skip("does not expose agent or scope through object spread clones", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect({ ...handle }).toEqual({});
    expect(Object.assign({}, handle)).toEqual({});
  });

  // TODO(batch-b): unskip when the redesign moves handles to private slots.
  it.skip("does not expose agent or scope through structuredClone", () => {
    const handle = createCredentialHandle({ id, agentId, scope });

    expect(structuredClone(handle)).toEqual({});
  });
});
