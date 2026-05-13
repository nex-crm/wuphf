import type { CredentialHandle } from "@wuphf/protocol";

import {
  BasicTextRejected,
  KeychainCommandFailed,
  NoKeyringAvailable,
  NotFound,
} from "../errors.ts";
import {
  type CredentialHandleParts,
  credentialAccount,
  credentialLabel,
  newCredentialHandle,
} from "../handle.ts";
import type { CredentialStore, CredentialWriteRequest, Spawner, SpawnResult } from "../store.ts";

export interface LinuxCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly timeoutMs?: number | undefined;
}

export class LinuxCredentialStore implements CredentialStore {
  constructor(private readonly options: LinuxCredentialStoreOptions) {}

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    await this.ensureSecretToolReady();
    const handle = newCredentialHandle(input);
    const parts = handleParts(handle);
    const result = await this.options.spawner(
      "secret-tool",
      ["store", "--label", credentialLabel(parts), ...attributes(this.options.serviceName, parts)],
      { input: input.secret, timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0) {
      throw commandErrorOrNoKeyring("secret-tool store", result);
    }
    return handle;
  }

  async read(handle: CredentialHandle): Promise<string> {
    await this.ensureSecretToolReady();
    const result = await this.options.spawner(
      "secret-tool",
      ["lookup", ...attributes(this.options.serviceName, handleParts(handle))],
      { timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0) {
      if (isNoKeyringMessage(result.stderr)) throw new NoKeyringAvailable(result.stderr);
      throw new NotFound();
    }

    const secret = stripOneTrailingNewline(result.stdout);
    if (secret.length === 0) throw new NotFound();
    return secret;
  }

  async delete(handle: CredentialHandle): Promise<void> {
    await this.ensureSecretToolReady();
    const result = await this.options.spawner(
      "secret-tool",
      ["clear", ...attributes(this.options.serviceName, handleParts(handle))],
      { timeoutMs: this.options.timeoutMs },
    );

    if (result.code !== 0 && !isNotFoundMessage(result.stderr)) {
      throw commandErrorOrNoKeyring("secret-tool clear", result);
    }
  }

  private async ensureSecretToolReady(): Promise<void> {
    const version = await this.options.spawner("secret-tool", ["--version"], {
      timeoutMs: this.options.timeoutMs,
    });
    if (version.code !== 0) {
      throw new NoKeyringAvailable(
        "secret-tool is not installed or not executable; install libsecret-tools and unlock an encrypted Secret Service keyring",
      );
    }

    const probe = await this.options.spawner(
      "secret-tool",
      ["search", "--all", "wuphf_collection_probe", "active"],
      { timeoutMs: this.options.timeoutMs },
    );
    if (probe.code === 0) {
      assertEncryptedLibsecretCollection(probe);
    } else if (isNoKeyringMessage(probe.stderr)) {
      throw new NoKeyringAvailable(probe.stderr);
    }
  }
}

export function assertEncryptedLibsecretCollection(probe: unknown): void {
  const text = probeText(probe).toLowerCase();
  if (/\bbasic[_-]?text\b|\bplain\s*text\b|\bplaintext\b|\bunencrypted\b/.test(text)) {
    throw new BasicTextRejected();
  }
}

function attributes(serviceName: string, parts: CredentialHandleParts): string[] {
  return [
    "wuphf_service",
    serviceName,
    "wuphf_account",
    credentialAccount(parts),
    "wuphf_agent_id",
    parts.agentId,
    "wuphf_scope",
    parts.scope,
  ];
}

function handleParts(handle: CredentialHandle): CredentialHandleParts {
  return { id: handle.id, agentId: handle.agentId, scope: handle.scope };
}

function commandErrorOrNoKeyring(command: string, result: SpawnResult): Error {
  if (isNoKeyringMessage(result.stderr)) return new NoKeyringAvailable(result.stderr);
  return new KeychainCommandFailed(command, result.code, result.stderr);
}

function probeText(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value !== "object" || value === null) return "";
  const record = value as { readonly stderr?: unknown; readonly stdout?: unknown };
  const stdout = typeof record.stdout === "string" ? record.stdout : "";
  const stderr = typeof record.stderr === "string" ? record.stderr : "";
  return `${stdout}\n${stderr}`;
}

function isNoKeyringMessage(stderr: string): boolean {
  return /secret service is not available|cannot autolaunch d-bus|no such interface|no keyring/i.test(
    stderr,
  );
}

function isNotFoundMessage(stderr: string): boolean {
  return /no such secret|not found|no matching items/i.test(stderr);
}

function stripOneTrailingNewline(value: string): string {
  return value.endsWith("\n") ? value.slice(0, -1) : value;
}
