import { randomBytes } from "node:crypto";

import {
  type AgentId,
  asCredentialHandleId,
  type CredentialHandle,
  type CredentialHandleId,
  type CredentialScope,
  createCredentialHandle,
} from "@wuphf/protocol";

export const DEFAULT_CREDENTIAL_SERVICE = "wuphf.credentials.v1";

export interface CredentialHandleParts {
  readonly id: CredentialHandleId;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}

export function newCredentialHandle(input: {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): CredentialHandle {
  return credentialHandleFromParts({
    id: newCredentialHandleId(),
    agentId: input.agentId,
    scope: input.scope,
  });
}

export function credentialHandleFromParts(parts: CredentialHandleParts): CredentialHandle {
  return createCredentialHandle(parts);
}

export function credentialAccount(input: {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}): string {
  return `agent:${input.agentId}:scope:${input.scope}`;
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
