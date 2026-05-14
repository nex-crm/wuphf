import { describe, expect, it } from "vitest";
import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  credentialDeleteRequestFromJson,
  credentialDeleteResponseFromJson,
  credentialReadRequestFromJson,
  credentialReadResponseFromJson,
  credentialWriteRequestFromJson,
  credentialWriteResponseFromJson,
} from "../src/index.ts";

type Decoder = (value: unknown) => unknown;

function recordWithAccessor(
  base: Readonly<Record<string, unknown>>,
  key: string,
): Record<string, unknown> {
  const record: Record<string, unknown> = { ...base };
  Object.defineProperty(record, key, {
    enumerable: true,
    get() {
      throw new Error("accessor should not be invoked");
    },
  });
  return record;
}

describe("credential IPC validator error branches", () => {
  const agentId = asAgentId("agent_alpha");
  const handleId = asCredentialHandleId("cred_ipc0123456789ABCDEFGHIJKLMNOP");
  const scope = asCredentialScope("openai");
  const credentialPayload = "opaque-test-value-0000";
  const handle = () => ({ version: 1, id: handleId });

  const cases: readonly [name: string, decode: Decoder, input: () => unknown, error: RegExp][] = [
    [
      "read request missing agentId",
      credentialReadRequestFromJson,
      () => ({ handleId }),
      /credentialReadRequest.agentId: is required/,
    ],
    [
      "read request invalid agentId",
      credentialReadRequestFromJson,
      () => ({ agentId: "Agent Alpha", handleId }),
      /credentialReadRequest.agentId: not an AgentId/,
    ],
    [
      "read request invalid handleId",
      credentialReadRequestFromJson,
      () => ({ agentId, handleId: "cred_short" }),
      /credentialReadRequest.handleId: not a CredentialHandleId/,
    ],
    [
      "read response missing secret",
      credentialReadResponseFromJson,
      () => ({}),
      /credentialReadResponse.secret: is required/,
    ],
    [
      "read response accessor secret",
      credentialReadResponseFromJson,
      () => recordWithAccessor({}, "secret"),
      /credentialReadResponse.secret: must be a data property/,
    ],
    [
      "read response undefined secret",
      credentialReadResponseFromJson,
      () => ({ secret: undefined }),
      /credentialReadResponse.secret: is required/,
    ],
    [
      "read response non-string secret",
      credentialReadResponseFromJson,
      () => ({ secret: 42 }),
      /credentialReadResponse.secret: must be a string/,
    ],
    [
      "write request missing scope",
      credentialWriteRequestFromJson,
      () => ({ agentId, secret: credentialPayload }),
      /credentialWriteRequest.scope: is required/,
    ],
    [
      "write request invalid scope",
      credentialWriteRequestFromJson,
      () => ({ agentId, scope: "unsupported", secret: credentialPayload }),
      /credentialWriteRequest.scope: not a supported CredentialScope/,
    ],
    [
      "write request non-string secret",
      credentialWriteRequestFromJson,
      () => ({ agentId, scope, secret: 42 }),
      /credentialWriteRequest.secret: must be a string/,
    ],
    [
      "write response missing handle",
      credentialWriteResponseFromJson,
      () => ({}),
      /credentialWriteResponse.handle: is required/,
    ],
    [
      "write response accessor handle",
      credentialWriteResponseFromJson,
      () => recordWithAccessor({}, "handle"),
      /credentialWriteResponse.handle: must be a data property/,
    ],
    [
      "write response invalid handle id",
      credentialWriteResponseFromJson,
      () => ({ handle: { version: 1, id: "cred_short" } }),
      /not a CredentialHandleId/,
    ],
    [
      "delete request missing handleId",
      credentialDeleteRequestFromJson,
      () => ({ agentId }),
      /credentialDeleteRequest.handleId: is required/,
    ],
    [
      "delete request invalid agentId",
      credentialDeleteRequestFromJson,
      () => ({ agentId: "Agent Alpha", handleId }),
      /credentialDeleteRequest.agentId: not an AgentId/,
    ],
    [
      "delete request invalid handleId",
      credentialDeleteRequestFromJson,
      () => ({ agentId, handleId: "cred_short" }),
      /credentialDeleteRequest.handleId: not a CredentialHandleId/,
    ],
    [
      "delete response missing deleted",
      credentialDeleteResponseFromJson,
      () => ({}),
      /credentialDeleteResponse.deleted: is required/,
    ],
    [
      "delete response accessor deleted",
      credentialDeleteResponseFromJson,
      () => recordWithAccessor({}, "deleted"),
      /credentialDeleteResponse.deleted: must be a data property/,
    ],
    [
      "delete response undefined deleted",
      credentialDeleteResponseFromJson,
      () => ({ deleted: undefined }),
      /credentialDeleteResponse.deleted: is required/,
    ],
    [
      "delete response false deleted",
      credentialDeleteResponseFromJson,
      () => ({ deleted: false }),
      /credentialDeleteResponse.deleted: must be true/,
    ],
    [
      "write response handle accessor id",
      credentialWriteResponseFromJson,
      () => ({ handle: recordWithAccessor({ version: 1 }, "id") }),
      /credentialHandle.id: must be a data property/,
    ],
    [
      "write response handle undefined id",
      credentialWriteResponseFromJson,
      () => ({ handle: { version: 1, id: undefined } }),
      /credentialHandle.id: is required/,
    ],
  ];

  for (const [name, decode, input, error] of cases) {
    it(`rejects ${name}`, () => {
      expect(() => decode(input())).toThrow(error);
    });
  }

  it("accepts canonical read, write, and delete credential envelopes", () => {
    expect(credentialReadRequestFromJson({ agentId, handleId })).toEqual({ agentId, handleId });
    expect(credentialReadResponseFromJson({ secret: credentialPayload })).toEqual({
      secret: credentialPayload,
    });
    expect(credentialWriteRequestFromJson({ agentId, scope, secret: credentialPayload })).toEqual({
      agentId,
      scope,
      secret: credentialPayload,
    });
    expect(credentialWriteResponseFromJson({ handle: handle() })).toEqual({ handle: handle() });
    expect(credentialDeleteRequestFromJson({ agentId, handleId })).toEqual({ agentId, handleId });
    expect(credentialDeleteResponseFromJson({ deleted: true })).toEqual({ deleted: true });
  });
});
