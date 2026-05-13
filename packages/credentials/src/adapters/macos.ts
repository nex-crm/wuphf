import type { CredentialHandle } from "@wuphf/protocol";

import { KeychainCommandFailed, NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialLabel,
  newCredentialHandle,
} from "../handle.ts";
import type { CredentialStore, CredentialWriteRequest, Spawner } from "../store.ts";

export interface MacOSCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly timeoutMs?: number | undefined;
}

export class MacOSCredentialStore implements CredentialStore {
  constructor(private readonly options: MacOSCredentialStoreOptions) {}

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    const handle = newCredentialHandle(input);
    const parts = handleParts(handle);
    const result = await this.options.spawner(
      "security",
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
      { input: input.secret, timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0) {
      throw new KeychainCommandFailed("security add-generic-password", result.code, result.stderr);
    }
    return handle;
  }

  async read(handle: CredentialHandle): Promise<string> {
    const parts = handleParts(handle);
    const result = await this.options.spawner(
      "security",
      [
        "find-generic-password",
        "-a",
        credentialAccount(parts),
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

  async delete(handle: CredentialHandle): Promise<void> {
    const parts = handleParts(handle);
    const result = await this.options.spawner(
      "security",
      ["delete-generic-password", "-a", credentialAccount(parts), "-s", this.options.serviceName],
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

function handleParts(handle: CredentialHandle): CredentialHandleParts {
  return { id: handle.id, agentId: handle.agentId, scope: handle.scope };
}

function isSecurityNotFound(stderr: string): boolean {
  return /could not be found|item not found|The specified item could not be found/i.test(stderr);
}

function stripOneTrailingNewline(value: string): string {
  return value.endsWith("\n") ? value.slice(0, -1) : value;
}
