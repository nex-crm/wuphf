import type { CredentialHandle } from "@wuphf/protocol";

import { NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialHandleParts,
  credentialLabel,
  newCredentialHandle,
} from "../internal/handle.ts";
import {
  assertBrokerIdentityForAgent,
  assertValidCredentialPayload,
  type CredentialDeleteRequest,
  type CredentialReadRequest,
  type CredentialStore,
  type CredentialWriteRequest,
  keychainCommandFailure,
  operationTimeoutMs,
  resolveTrustedCommand,
  runKeychainCommand,
  type Spawner,
  type TrustedCommand,
} from "../store.ts";

export interface MacOSCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly enforceTrustedCommand?: boolean | undefined;
  readonly timeoutMs?: number | undefined;
}

export class MacOSCredentialStore implements CredentialStore {
  private readonly security: TrustedCommand;

  constructor(private readonly options: MacOSCredentialStoreOptions) {
    this.security = resolveTrustedCommand({
      candidates: ["/usr/bin/security"],
      commandName: "security",
      enforce: options.enforceTrustedCommand ?? true,
      platform: "darwin",
      recoveryHint: "Ensure /usr/bin/security exists and the login keychain is unlocked",
    });
  }

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    assertValidCredentialPayload(input.secret);
    const handle = newCredentialHandle(input);
    const parts = credentialHandleParts(handle, input);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
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
      {
        action: "write",
        commandName: "security add-generic-password",
        input: input.secret,
        platform: "darwin",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      throw keychainCommandFailure("security add-generic-password", result, {
        action: "write",
        platform: "darwin",
      });
    }
    return handle;
  }

  async read(input: CredentialReadRequest): Promise<string> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
      [
        "find-generic-password",
        "-a",
        credentialAccount({ handleId: input.handleId }),
        "-s",
        this.options.serviceName,
        "-w",
      ],
      {
        action: "read",
        commandName: "security find-generic-password",
        platform: "darwin",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      if (isSecurityNotFound(result.stderr)) throw new NotFound();
      throw keychainCommandFailure("security find-generic-password", result, {
        action: "read",
        platform: "darwin",
      });
    }
    return stripOneTrailingNewline(result.stdout);
  }

  async delete(input: CredentialDeleteRequest): Promise<void> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
      [
        "delete-generic-password",
        "-a",
        credentialAccount({ handleId: input.handleId }),
        "-s",
        this.options.serviceName,
      ],
      {
        action: "delete",
        commandName: "security delete-generic-password",
        platform: "darwin",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0 && !isSecurityNotFound(result.stderr)) {
      throw keychainCommandFailure("security delete-generic-password", result, {
        action: "delete",
        platform: "darwin",
      });
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
