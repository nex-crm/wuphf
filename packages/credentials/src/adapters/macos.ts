import type { CredentialHandle } from "@wuphf/protocol";

import { KeychainCommandFailed, NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialHandleParts,
  credentialLabel,
  newCredentialHandle,
} from "../internal/handle.ts";
import {
  assertBrokerIdentityForAgent,
  type CredentialDeleteRequest,
  type CredentialReadRequest,
  type CredentialStore,
  type CredentialWriteRequest,
  type Spawner,
} from "../store.ts";

export interface MacOSCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly timeoutMs?: number | undefined;
}

export class MacOSCredentialStore implements CredentialStore {
  constructor(private readonly options: MacOSCredentialStoreOptions) {}

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const handle = newCredentialHandle(input);
    const parts = credentialHandleParts(handle, input);
    const result = await this.options.spawner(
      "security",
      [
        "add-generic-password",
        "-U",
        "-a",
        credentialAccount({ handleId: parts.id }),
        "-s",
        this.options.serviceName,
        "-l",
        credentialLabel(parts),
        "-j",
        credentialMetadata(parts),
        "-w",
      ],
      { input: input.secret, timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0) {
      throw new KeychainCommandFailed("security add-generic-password", result.code, result.stderr);
    }
    return handle;
  }

  async read(input: CredentialReadRequest): Promise<string> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const result = await this.options.spawner(
      "security",
      [
        "find-generic-password",
        "-a",
        credentialAccount({ handleId: input.handleId }),
        "-s",
        this.options.serviceName,
        "-w",
      ],
      { timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0) {
      if (isSecurityNotFound(result.stderr)) throw new NotFound();
      throw new KeychainCommandFailed("security find-generic-password", result.code, result.stderr);
    }
    return stripOneTrailingNewline(result.stdout);
  }

  async delete(input: CredentialDeleteRequest): Promise<void> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const result = await this.options.spawner(
      "security",
      [
        "delete-generic-password",
        "-a",
        credentialAccount({ handleId: input.handleId }),
        "-s",
        this.options.serviceName,
      ],
      { timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0 && !isSecurityNotFound(result.stderr)) {
      throw new KeychainCommandFailed(
        "security delete-generic-password",
        result.code,
        result.stderr,
      );
    }
  }
}

function credentialMetadata(parts: CredentialHandleParts): string {
  return JSON.stringify({ agentId: parts.agentId, scope: parts.scope });
}

function isSecurityNotFound(stderr: string): boolean {
  return /could not be found|item not found|The specified item could not be found/i.test(stderr);
}

function stripOneTrailingNewline(value: string): string {
  return value.endsWith("\n") ? value.slice(0, -1) : value;
}
