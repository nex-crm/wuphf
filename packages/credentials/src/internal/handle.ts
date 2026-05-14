import { randomBytes } from "node:crypto";

import {
  type AgentId,
  asCredentialHandleId,
  type CredentialHandle,
  type CredentialHandleId,
  type CredentialScope,
  createCredentialHandle,
  credentialHandleToJson,
} from "@wuphf/protocol";

export const DEFAULT_CREDENTIAL_SERVICE = "wuphf.credentials.v1";

export interface CredentialHandleParts {
  readonly id: CredentialHandleId;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}

export interface CredentialLookupParts {
  readonly handleId: CredentialHandleId;
  readonly agentId: AgentId;
}

export function newCredentialHandle(input: {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): CredentialHandle {
  return createCredentialHandle({
    id: newCredentialHandleId(),
    agentId: input.agentId,
    scope: input.scope,
  });
}

export function credentialHandleParts(
  handle: CredentialHandle,
  input: {
    readonly agentId: AgentId;
    readonly scope: CredentialScope;
  },
): CredentialHandleParts {
  return {
    id: credentialHandleToJson(handle).id,
    agentId: input.agentId,
    scope: input.scope,
  };
}

export function credentialAccount(input: { readonly handleId: CredentialHandleId }): string {
  return input.handleId;
}

export function credentialLabel(input: {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): string {
  return `WUPHF ${input.scope} credential for ${input.agentId}`;
}

function newCredentialHandleId(): CredentialHandleId {
  return asCredentialHandleId(`cred_${randomBytes(24).toString("base64url")}`);
}
