import { type ExecFileException, execFile } from "node:child_process";
import { promisify } from "node:util";

import type { AgentId, CredentialHandle, CredentialScope } from "@wuphf/protocol";

import { LinuxCredentialStore } from "./adapters/linux.ts";
import { MacOSCredentialStore } from "./adapters/macos.ts";
import { WindowsCredentialStore } from "./adapters/windows.ts";
import { AdapterNotSupported } from "./errors.ts";
import { DEFAULT_CREDENTIAL_SERVICE } from "./handle.ts";

export interface SpawnOptions {
  readonly input?: string | undefined;
  readonly env?: NodeJS.ProcessEnv;
  readonly timeoutMs?: number | undefined;
}

export interface SpawnResult {
  readonly stdout: string;
  readonly stderr: string;
  readonly code: number;
}

export type Spawner = (
  cmd: string,
  args: readonly string[],
  options?: SpawnOptions,
) => Promise<SpawnResult>;

export interface CredentialWriteRequest {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
  readonly secret: string;
}

export interface CredentialStore {
  write(input: CredentialWriteRequest): Promise<CredentialHandle>;
  read(handle: CredentialHandle): Promise<string>;
  delete(handle: CredentialHandle): Promise<void>;
}

export interface CredentialStoreOptions {
  readonly platform?: NodeJS.Platform;
  readonly serviceName?: string;
  readonly spawner?: Spawner;
  readonly timeoutMs?: number | undefined;
}

const execFileAsync = promisify(execFile);

export function open(options: CredentialStoreOptions = {}): CredentialStore {
  const platform = options.platform ?? process.platform;
  const storeOptions = {
    serviceName: options.serviceName ?? DEFAULT_CREDENTIAL_SERVICE,
    spawner: options.spawner ?? execFileSpawner,
    timeoutMs: options.timeoutMs,
  };

  switch (platform) {
    case "darwin":
      return new MacOSCredentialStore(storeOptions);
    case "linux":
      return new LinuxCredentialStore(storeOptions);
    case "win32":
      return new WindowsCredentialStore(storeOptions);
    default:
      throw new AdapterNotSupported(platform);
  }
}

export const openCredentialStore = open;

export async function execFileSpawner(
  cmd: string,
  args: readonly string[],
  options: SpawnOptions = {},
): Promise<SpawnResult> {
  if (options.input !== undefined) {
    return execFileWithInput(cmd, args, options);
  }

  try {
    const result = await execFileAsync(cmd, [...args], {
      encoding: "utf8",
      env: options.env,
      timeout: options.timeoutMs,
      windowsHide: true,
    });
    return {
      stdout: normalizeOutput(result.stdout),
      stderr: normalizeOutput(result.stderr),
      code: 0,
    };
  } catch (error) {
    const result = spawnResultFromError(error);
    if (result !== null) return result;
    throw error;
  }
}

function execFileWithInput(
  cmd: string,
  args: readonly string[],
  options: SpawnOptions,
): Promise<SpawnResult> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      cmd,
      [...args],
      {
        encoding: "utf8",
        env: options.env,
        timeout: options.timeoutMs,
        windowsHide: true,
      },
      (error: ExecFileException | null, stdout: string | Buffer, stderr: string | Buffer) => {
        const result = {
          stdout: normalizeOutput(stdout),
          stderr: normalizeOutput(stderr),
          code: error === null ? 0 : exitCodeFromError(error),
        };
        resolve(result);
      },
    );

    child.on("error", reject);
    child.stdin?.end(options.input);
  });
}

function spawnResultFromError(error: unknown): SpawnResult | null {
  if (typeof error !== "object" || error === null) return null;
  const record = error as {
    readonly code?: unknown;
    readonly stderr?: unknown;
    readonly stdout?: unknown;
  };
  if (record.stdout === undefined && record.stderr === undefined && record.code === undefined) {
    return null;
  }

  return {
    stdout: normalizeOutput(record.stdout),
    stderr: normalizeOutput(record.stderr),
    code: typeof record.code === "number" ? record.code : 1,
  };
}

function exitCodeFromError(error: ExecFileException): number {
  return typeof error.code === "number" ? error.code : 1;
}

function normalizeOutput(value: unknown): string {
  if (typeof value === "string") return value;
  if (Buffer.isBuffer(value)) return value.toString("utf8");
  if (value === undefined || value === null) return "";
  return String(value);
}
