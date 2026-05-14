import type { CredentialHandle } from "@wuphf/protocol";

import { BasicTextRejected, NoKeyringAvailable, NotFound } from "../errors.ts";
import {
  type CredentialHandleParts,
  type CredentialLookupParts,
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
  probeTimeoutMs,
  resolveOptionalTrustedCommand,
  resolveTrustedCommand,
  runKeychainCommand,
  type Spawner,
  type SpawnResult,
  type TrustedCommand,
} from "../store.ts";

export interface LinuxCredentialStoreOptions {
  readonly serviceName: string;
  readonly spawner: Spawner;
  readonly enforceTrustedCommand?: boolean | undefined;
  readonly timeoutMs?: number | undefined;
}

export class LinuxCredentialStore implements CredentialStore {
  private readonly gdbus: TrustedCommand | null;
  private readonly secretTool: TrustedCommand;

  constructor(private readonly options: LinuxCredentialStoreOptions) {
    const enforce = options.enforceTrustedCommand ?? true;
    this.secretTool = resolveTrustedCommand({
      candidates: ["/usr/bin/secret-tool", "/usr/local/bin/secret-tool"],
      commandName: "secret-tool",
      enforce,
      platform: "linux",
      recoveryHint: "Install libsecret-tools: sudo apt install libsecret-tools",
      rejectHomeLocalBin: true,
    });
    this.gdbus = resolveOptionalTrustedCommand({
      candidates: ["/usr/bin/gdbus", "/usr/local/bin/gdbus"],
      commandName: "gdbus",
      enforce,
      platform: "linux",
      recoveryHint: "Install libglib2.0-bin so gdbus can verify the Secret Service backend",
      rejectHomeLocalBin: true,
    });
  }

  async write(input: CredentialWriteRequest): Promise<CredentialHandle> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    assertValidCredentialPayload(input.secret);
    await this.ensureSecretToolReady();
    const handle = newCredentialHandle(input);
    const parts = credentialHandleParts(handle, input);
    const result = await runKeychainCommand(
      this.options.spawner,
      this.secretTool,
      ["store", "--label", credentialLabel(parts), ...attributes(this.options.serviceName, parts)],
      {
        action: "write",
        commandName: "secret-tool store",
        input: input.secret,
        platform: "linux",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0) {
      throw commandErrorOrNoKeyring("secret-tool store", result, "write");
    }

    try {
      await this.assertPostWriteReadback(parts);
    } catch (error) {
      await this.clearAfterFailedWrite(parts);
      throw error;
    }
    return handle;
  }

  async read(input: CredentialReadRequest): Promise<string> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    await this.ensureSecretToolReady();
    const result = await runKeychainCommand(
      this.options.spawner,
      this.secretTool,
      ["lookup", ...lookupAttributes(this.options.serviceName, input)],
      {
        action: "read",
        commandName: "secret-tool lookup",
        platform: "linux",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );
    assertEncryptedLibsecretCollection({ stderr: result.stderr });

    if (result.code !== 0) {
      if (isNoKeyringMessage(result.stderr)) throw new NoKeyringAvailable(result.stderr);
      if (isNotFoundMessage(result.stderr)) throw new NotFound();
      throw commandErrorOrNoKeyring("secret-tool lookup", result, "read");
    }

    const secret = stripOneTrailingNewline(result.stdout);
    if (secret.length === 0) throw new NotFound();
    return secret;
  }

  async delete(input: CredentialDeleteRequest): Promise<void> {
    assertBrokerIdentityForAgent(input.broker, input.agentId);
    await this.ensureSecretToolReady();
    const result = await runKeychainCommand(
      this.options.spawner,
      this.secretTool,
      ["clear", ...lookupAttributes(this.options.serviceName, input)],
      {
        action: "delete",
        commandName: "secret-tool clear",
        platform: "linux",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );

    if (result.code !== 0 && !isNotFoundMessage(result.stderr)) {
      throw commandErrorOrNoKeyring("secret-tool clear", result, "delete");
    }
  }

  private async ensureSecretToolReady(): Promise<void> {
    const version = await runKeychainCommand(this.options.spawner, this.secretTool, ["--version"], {
      action: "probe",
      commandName: "secret-tool --version",
      platform: "linux",
      timeoutMs: probeTimeoutMs(this.options.timeoutMs),
    });
    if (version.code !== 0) {
      throw commandErrorOrNoKeyring("secret-tool --version", version, "probe");
    }

    const probe = await runKeychainCommand(
      this.options.spawner,
      this.secretTool,
      ["search", "--all", "wuphf_collection_probe", "active"],
      {
        action: "probe",
        commandName: "secret-tool search",
        platform: "linux",
        timeoutMs: probeTimeoutMs(this.options.timeoutMs),
      },
    );
    assertEncryptedLibsecretCollection(probe);
    if (probe.code !== 0 && isNoKeyringMessage(probe.stderr)) {
      throw new NoKeyringAvailable(probe.stderr);
    }
    await this.assertDefaultCollectionProperties();
  }

  private async assertDefaultCollectionProperties(): Promise<void> {
    if (this.gdbus === null) throw new BasicTextRejected();

    const result = await runKeychainCommand(
      this.options.spawner,
      this.gdbus,
      [
        "call",
        "--session",
        "--dest=org.freedesktop.secrets",
        "--object-path=/org/freedesktop/secrets/aliases/default",
        "--method=org.freedesktop.DBus.Properties.GetAll",
        "org.freedesktop.Secret.Collection",
      ],
      {
        action: "probe",
        commandName: "gdbus default collection properties",
        platform: "linux",
        timeoutMs: probeTimeoutMs(this.options.timeoutMs),
      },
    );
    assertEncryptedLibsecretCollection({ stderr: result.stderr });
    if (result.code === 0) return;
    if (isNoKeyringMessage(result.stderr)) throw new NoKeyringAvailable(result.stderr);
    throw new BasicTextRejected();
  }

  private async assertPostWriteReadback(parts: CredentialHandleParts): Promise<void> {
    const result = await runKeychainCommand(
      this.options.spawner,
      this.secretTool,
      ["lookup", ...attributes(this.options.serviceName, parts)],
      {
        action: "post-write-readback",
        commandName: "secret-tool lookup",
        platform: "linux",
        timeoutMs: operationTimeoutMs(this.options.timeoutMs),
      },
    );
    assertEncryptedLibsecretCollection({ stderr: result.stderr });
    if (result.code !== 0) {
      throw commandErrorOrNoKeyring("secret-tool lookup", result, "post-write-readback");
    }
  }

  private async clearAfterFailedWrite(parts: CredentialHandleParts): Promise<void> {
    try {
      await runKeychainCommand(
        this.options.spawner,
        this.secretTool,
        ["clear", ...attributes(this.options.serviceName, parts)],
        {
          action: "delete",
          commandName: "secret-tool clear",
          platform: "linux",
          timeoutMs: operationTimeoutMs(this.options.timeoutMs),
        },
      );
    } catch {
      // Preserve the original rejection; cleanup is best-effort after a failed readback.
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
    credentialAccount({ handleId: parts.id }),
    "wuphf_agent_id",
    parts.agentId,
    "wuphf_scope",
    parts.scope,
  ];
}

function lookupAttributes(serviceName: string, parts: CredentialLookupParts): string[] {
  return [
    "wuphf_service",
    serviceName,
    "wuphf_account",
    credentialAccount({ handleId: parts.handleId }),
    "wuphf_agent_id",
    parts.agentId,
  ];
}

function commandErrorOrNoKeyring(command: string, result: SpawnResult, action: string): Error {
  if (isNoKeyringMessage(result.stderr)) return new NoKeyringAvailable(result.stderr);
  return keychainCommandFailure(command, result, { action, platform: "linux" });
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
  return /secret service is not available|cannot autolaunch d-bus|d-bus|no such interface|no keyring|command not found|not executable|no such file/i.test(
    stderr,
  );
}

function isNotFoundMessage(stderr: string): boolean {
  return /\bno such secret\b|\bno matching items\b|\bnot found\b/i.test(stderr);
}

function stripOneTrailingNewline(value: string): string {
  return value.endsWith("\n") ? value.slice(0, -1) : value;
}
