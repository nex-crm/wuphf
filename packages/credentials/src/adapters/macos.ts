import type { CredentialHandle } from "@wuphf/protocol";

import { NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialLabel,
  newCredentialHandle,
} from "../handle.ts";
import {
  assertValidCredentialPayload,
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
    assertValidCredentialPayload(input.secret);
    const handle = newCredentialHandle(input);
    const parts = handleParts(handle);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
      [
        "add-generic-password",
        "-U",
        "-a",
        credentialAccount(parts),
        "-s",
        this.options.serviceName,
        "-l",
        credentialLabel(parts),
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

  async read(handle: CredentialHandle): Promise<string> {
    const parts = handleParts(handle);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
      [
        "find-generic-password",
        "-a",
        credentialAccount(parts),
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

  async delete(handle: CredentialHandle): Promise<void> {
    const parts = handleParts(handle);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.security,
      ["delete-generic-password", "-a", credentialAccount(parts), "-s", this.options.serviceName],
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

function handleParts(handle: CredentialHandle): CredentialHandleParts {
  return { id: handle.id, agentId: handle.agentId, scope: handle.scope };
}

function isSecurityNotFound(stderr: string): boolean {
  return /could not be found|item not found|The specified item could not be found/i.test(stderr);
}

function stripOneTrailingNewline(value: string): string {
  return value.endsWith("\n") ? value.slice(0, -1) : value;
}
