import { type ExecFileException, execFile, execFileSync } from "node:child_process";
import { realpathSync, statSync } from "node:fs";
import path from "node:path";
import { promisify } from "node:util";

import {
  type AgentId,
  asAgentId,
  asCredentialScope,
  type BrokerIdentity,
  brokerIdentityAgentId,
  type CredentialHandle,
  type CredentialHandleId,
  type CredentialScope,
  isBrokerIdentity,
} from "@wuphf/protocol";

import { LinuxCredentialStore } from "./adapters/linux.ts";
import { MacOSCredentialStore } from "./adapters/macos.ts";
import { WindowsCredentialStore } from "./adapters/windows.ts";
import {
  AdapterNotSupported,
  BrokerIdentityRequired,
  CredentialOwnershipMismatch,
  InvalidCredentialPayload,
  KeychainCommandFailed,
  KeychainCommandTimedOut,
  NoKeyringAvailable,
} from "./errors.ts";
import { DEFAULT_CREDENTIAL_SERVICE } from "./internal/handle.ts";

export interface SpawnOptions {
  readonly input?: string | undefined;
  readonly env?: NodeJS.ProcessEnv;
  readonly timeoutMs?: number | undefined;
}

export interface SpawnResult {
  readonly stdout: string;
  readonly stderr: string;
  readonly code: number;
  readonly systemCode?: string | undefined;
  readonly signal?: NodeJS.Signals | string | undefined;
  readonly killed?: boolean | undefined;
  readonly stderrSnippet?: string | undefined;
  readonly timedOut?: boolean | undefined;
}

export type Spawner = (
  cmd: string,
  args: readonly string[],
  options?: SpawnOptions,
) => Promise<SpawnResult>;

export interface CredentialWriteRequest {
  readonly broker: BrokerIdentity;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
  /**
   * Secret bytes encoded as UTF-8 text. Adapters store the value verbatim and
   * return the same text on read; NUL bytes and invalid UTF-16 surrogate pairs
   * are rejected because OS keychain CLIs do not round-trip them consistently.
   */
  readonly secret: string;
}

export interface CredentialReadRequest {
  readonly broker: BrokerIdentity;
  readonly handleId: CredentialHandleId;
  readonly agentId: AgentId;
}

export interface CredentialReadWithOwnershipRequest {
  readonly broker: BrokerIdentity;
  readonly handleId: CredentialHandleId;
  readonly expectedAgentId: AgentId;
  readonly expectedScope: CredentialScope;
}

export interface CredentialReadWithOwnershipResult {
  readonly secret: string;
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
}

export interface CredentialDeleteRequest {
  readonly broker: BrokerIdentity;
  readonly handleId: CredentialHandleId;
  readonly agentId: AgentId;
}

export interface CredentialStore {
  write(input: CredentialWriteRequest): Promise<CredentialHandle>;
  read(input: CredentialReadRequest): Promise<string>;
  readWithOwnership(
    input: CredentialReadWithOwnershipRequest,
  ): Promise<CredentialReadWithOwnershipResult>;
  delete(input: CredentialDeleteRequest): Promise<void>;
}

export interface CredentialStoreOptions {
  readonly platform?: NodeJS.Platform;
  readonly serviceName?: string;
  readonly spawner?: Spawner;
  readonly timeoutMs?: number | undefined;
}

const execFileAsync = promisify(execFile);
export const DEFAULT_KEYCHAIN_PROBE_TIMEOUT_MS = 5_000;
export const DEFAULT_KEYCHAIN_OPERATION_TIMEOUT_MS = 15_000;

export function open(options: CredentialStoreOptions = {}): CredentialStore {
  const platform = options.platform ?? process.platform;
  const spawner = options.spawner ?? execFileSpawner;
  const storeOptions = {
    serviceName: options.serviceName ?? DEFAULT_CREDENTIAL_SERVICE,
    spawner,
    timeoutMs: options.timeoutMs,
    enforceTrustedCommand: options.spawner === undefined,
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

export function assertBrokerIdentityForAgent(broker: unknown, agentId: AgentId): void {
  if (!isBrokerIdentity(broker) || brokerIdentityAgentId(broker) !== agentId) {
    throw new BrokerIdentityRequired();
  }
}

export function parseCredentialOwnershipMetadata(value: string): {
  readonly agentId: AgentId;
  readonly scope: CredentialScope;
} {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    throw new CredentialOwnershipMismatch();
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new CredentialOwnershipMismatch();
  }
  const record = parsed as Readonly<{
    agentId?: unknown;
    scope?: unknown;
  }>;
  if (
    Object.keys(record).length !== 2 ||
    !hasOwn(record, "agentId") ||
    !hasOwn(record, "scope") ||
    typeof record.agentId !== "string" ||
    typeof record.scope !== "string"
  ) {
    throw new CredentialOwnershipMismatch();
  }
  try {
    return {
      agentId: asAgentId(record.agentId),
      scope: asCredentialScope(record.scope),
    };
  } catch {
    throw new CredentialOwnershipMismatch();
  }
}

function hasOwn(record: Readonly<Record<string, unknown>>, key: string): boolean {
  return Object.hasOwn(record, key);
}

export function assertCredentialOwnership(
  actual: { readonly agentId: AgentId; readonly scope: CredentialScope },
  expected: { readonly agentId: AgentId; readonly scope: CredentialScope },
): void {
  if (actual.agentId !== expected.agentId || actual.scope !== expected.scope) {
    throw new CredentialOwnershipMismatch();
  }
}

export interface TrustedCommand {
  readonly path: string;
  readonly env: NodeJS.ProcessEnv;
}

export interface TrustedCommandSpec {
  readonly candidates: readonly string[];
  readonly commandName: string;
  readonly platform: NodeJS.Platform;
  readonly recoveryHint: string;
  readonly enforce: boolean;
  readonly rejectHomeLocalBin?: boolean;
  readonly requireWindowsAdministratorsOwner?: boolean;
}

export function resolveTrustedCommand(spec: TrustedCommandSpec): TrustedCommand {
  const resolvedPath = resolveTrustedCommandPath(spec);
  return { path: resolvedPath, env: sanitizedCommandEnv(resolvedPath, spec.platform) };
}

export function resolveOptionalTrustedCommand(spec: TrustedCommandSpec): TrustedCommand | null {
  try {
    return resolveTrustedCommand(spec);
  } catch (error) {
    if (error instanceof NoKeyringAvailable && missingTrustedCommand(spec)) return null;
    throw error;
  }
}

export function operationTimeoutMs(override: number | undefined): number {
  return override ?? DEFAULT_KEYCHAIN_OPERATION_TIMEOUT_MS;
}

export function probeTimeoutMs(override: number | undefined): number {
  return override ?? DEFAULT_KEYCHAIN_PROBE_TIMEOUT_MS;
}

export async function runKeychainCommand(
  spawner: Spawner,
  command: TrustedCommand,
  args: readonly string[],
  options: {
    readonly action: string;
    readonly commandName: string;
    readonly input?: string | undefined;
    readonly platform: NodeJS.Platform;
    readonly timeoutMs: number;
  },
): Promise<SpawnResult> {
  let timeout: NodeJS.Timeout | undefined;
  const commandPromise = spawner(command.path, args, {
    input: options.input,
    env: command.env,
    timeoutMs: options.timeoutMs,
  }).catch((error: unknown) => {
    const result = spawnResultFromError(error, options.timeoutMs);
    if (result !== null) return result;
    throw error;
  });
  const timeoutPromise = new Promise<never>((_, reject) => {
    timeout = setTimeout(() => {
      reject(
        new KeychainCommandTimedOut(
          options.commandName,
          options.timeoutMs,
          options.platform,
          options.action,
        ),
      );
    }, options.timeoutMs);
    timeout.unref();
  });

  try {
    const result = await Promise.race([commandPromise, timeoutPromise]);
    if (result.timedOut === true) {
      throw new KeychainCommandTimedOut(
        options.commandName,
        options.timeoutMs,
        options.platform,
        options.action,
        { killed: result.killed, signal: result.signal },
      );
    }
    return result;
  } finally {
    if (timeout !== undefined) clearTimeout(timeout);
  }
}

export function keychainCommandFailure(
  command: string,
  result: SpawnResult,
  context: {
    readonly action: string;
    readonly platform: NodeJS.Platform;
    readonly recoveryHint?: string | undefined;
  },
): Error {
  if (result.timedOut === true) {
    return new KeychainCommandTimedOut(command, 0, context.platform, context.action, {
      killed: result.killed,
      signal: result.signal,
    });
  }
  if (result.systemCode === "ENOENT") {
    return new NoKeyringAvailable(`${command} is unavailable`, {
      recoveryHint: context.recoveryHint ?? recoveryHintForPlatform(context.platform),
    });
  }
  return new KeychainCommandFailed(command, result.code, result.stderr, {
    killed: result.killed,
    recoveryHint: context.recoveryHint,
    signal: result.signal,
    stderrSnippet: result.stderrSnippet,
    systemCode: result.systemCode,
  });
}

export function assertValidCredentialPayload(secret: string): void {
  if (secret.length === 0 || secret.includes("\0") || hasUnpairedSurrogate(secret)) {
    throw new InvalidCredentialPayload();
  }
}

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
    const result = spawnResultFromError(error, options.timeoutMs);
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
        const normalizedStderr = normalizeOutput(stderr);
        const result =
          error === null
            ? { stdout: normalizeOutput(stdout), stderr: normalizedStderr, code: 0 }
            : withFailureMetadata(
                {
                  stdout: normalizeOutput(stdout),
                  stderr: normalizedStderr,
                  code: exitCodeFromError(error),
                },
                error,
                options.timeoutMs,
              );
        resolve(result);
      },
    );

    child.on("error", reject);
    child.stdin?.end(options.input);
  });
}

function resolveTrustedCommandPath(spec: TrustedCommandSpec): string {
  const fallback = spec.candidates[0];
  if (fallback === undefined) {
    throw new NoKeyringAvailable(`${spec.commandName} has no trusted candidate path`, {
      recoveryHint: spec.recoveryHint,
    });
  }
  if (!spec.enforce) return fallback;

  for (const candidate of spec.candidates) {
    let resolved: string;
    try {
      resolved = realpathSync(candidate);
    } catch {
      continue;
    }
    assertTrustedResolvedPath(resolved, spec);
    return resolved;
  }

  throw new NoKeyringAvailable(`${spec.commandName} was not found at a trusted system path`, {
    recoveryHint: spec.recoveryHint,
  });
}

function missingTrustedCommand(spec: TrustedCommandSpec): boolean {
  if (!spec.enforce) return false;
  return spec.candidates.every((candidate) => {
    try {
      realpathSync(candidate);
      return false;
    } catch {
      return true;
    }
  });
}

function assertTrustedResolvedPath(resolvedPath: string, spec: TrustedCommandSpec): void {
  if (spec.platform === "win32") {
    if (spec.requireWindowsAdministratorsOwner === true) {
      assertWindowsAdministratorsOwner(resolvedPath, spec);
    }
    return;
  }

  const stats = statSync(resolvedPath);
  if (!stats.isFile()) {
    throw new NoKeyringAvailable(`${spec.commandName} does not resolve to a regular file`, {
      recoveryHint: spec.recoveryHint,
    });
  }
  if ((stats.mode & 0o022) !== 0) {
    throw new NoKeyringAvailable(`${spec.commandName} is writable by non-owner users`, {
      recoveryHint: spec.recoveryHint,
    });
  }

  // Parent directories must not be group/world-writable, or another user could
  // swap the executable between this stat() and the eventual spawn().
  let parent = path.dirname(resolvedPath);
  while (parent !== path.dirname(parent)) {
    const parentStats = statSync(parent);
    if ((parentStats.mode & 0o022) !== 0) {
      throw new NoKeyringAvailable(
        `${spec.commandName} parent directory "${parent}" is writable by non-owner users`,
        { recoveryHint: spec.recoveryHint },
      );
    }
    parent = path.dirname(parent);
  }

  if (spec.rejectHomeLocalBin === true && isUnderHomeLocalBin(resolvedPath)) {
    throw new NoKeyringAvailable(`${spec.commandName} resolved under ~/.local/bin`, {
      recoveryHint: spec.recoveryHint,
    });
  }
}

function assertWindowsAdministratorsOwner(resolvedPath: string, spec: TrustedCommandSpec): void {
  // icacls prints the DACL (access-control entries), not the owner principal;
  // matching "BUILTIN\Administrators" in its output is false-positive-prone
  // because an Administrators ACE can exist even when the owner is someone
  // else. Use PowerShell `Get-Acl` and read the canonical `.Owner` field.
  const systemRoot = processEnvValue("SystemRoot") ?? "C:\\Windows";
  const powershell = path.win32.join(
    systemRoot,
    "System32",
    "WindowsPowerShell",
    "v1.0",
    "powershell.exe",
  );
  try {
    const escapedPath = resolvedPath.replace(/'/g, "''");
    const owner = execFileSync(
      powershell,
      ["-NoProfile", "-Command", `(Get-Acl -LiteralPath '${escapedPath}').Owner`],
      { encoding: "utf8", windowsHide: true },
    ).trim();
    if (!/^BUILTIN\\Administrators$/i.test(owner)) {
      throw new Error(`owner is "${owner}", expected "BUILTIN\\Administrators"`);
    }
  } catch (error) {
    throw new NoKeyringAvailable(`${spec.commandName} failed Windows ownership validation`, {
      cause: error,
      recoveryHint: spec.recoveryHint,
    });
  }
}

function isUnderHomeLocalBin(resolvedPath: string): boolean {
  const home = processEnvValue("HOME");
  if (home === undefined || home.length === 0) return false;
  const homeLocalBin = path.resolve(home, ".local", "bin");
  const normalized = path.resolve(resolvedPath);
  return normalized === homeLocalBin || normalized.startsWith(`${homeLocalBin}${path.sep}`);
}

function sanitizedCommandEnv(commandPath: string, platform: NodeJS.Platform): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    // Force deterministic English diagnostics and prevent PATH shadowing.
    LC_ALL: "C",
    PATH: commandDir(commandPath, platform),
  };
  const home = processEnvValue("HOME") ?? processEnvValue("USERPROFILE");
  const user = processEnvValue("USER") ?? processEnvValue("USERNAME");
  if (home !== undefined && home.length > 0) setEnvValue(env, "HOME", home);
  if (user !== undefined && user.length > 0) setEnvValue(env, "USER", user);
  return env;
}

function processEnvValue(name: string): string | undefined {
  return process.env[name];
}

function setEnvValue(env: NodeJS.ProcessEnv, name: string, value: string): void {
  env[name] = value;
}

function commandDir(commandPath: string, platform: NodeJS.Platform): string {
  return platform === "win32" ? path.win32.dirname(commandPath) : path.posix.dirname(commandPath);
}

function spawnResultFromError(error: unknown, timeoutMs?: number | undefined): SpawnResult | null {
  if (typeof error !== "object" || error === null) return null;
  const record = error as {
    readonly code?: unknown;
    readonly killed?: unknown;
    readonly signal?: unknown;
    readonly stderr?: unknown;
    readonly stdout?: unknown;
  };
  if (
    record.stdout === undefined &&
    record.stderr === undefined &&
    record.code === undefined &&
    record.killed === undefined &&
    record.signal === undefined
  ) {
    return null;
  }
  const stderr = normalizeOutput(record.stderr);

  return withFailureMetadata(
    {
      stdout: normalizeOutput(record.stdout),
      stderr,
      code: typeof record.code === "number" ? record.code : 1,
    },
    error,
    timeoutMs,
  );
}

function exitCodeFromError(error: ExecFileException): number {
  return typeof error.code === "number" ? error.code : 1;
}

function withFailureMetadata(
  result: SpawnResult,
  error: unknown,
  timeoutMs?: number | undefined,
): SpawnResult {
  const metadata = failureMetadata(error, result.stderr, timeoutMs);
  return { ...result, ...metadata };
}

function failureMetadata(
  error: unknown,
  stderr: string,
  timeoutMs?: number | undefined,
): SpawnResultMetadata {
  if (typeof error !== "object" || error === null) {
    return stderr.length > 0 ? { stderrSnippet: stderr.slice(0, 200) } : {};
  }
  const record = error as {
    readonly code?: unknown;
    readonly killed?: unknown;
    readonly signal?: unknown;
  };
  const metadata: SpawnResultMetadata = {};
  if (typeof record.code === "string") metadata.systemCode = record.code;
  if (typeof record.signal === "string") metadata.signal = record.signal;
  if (typeof record.killed === "boolean") metadata.killed = record.killed;
  if (stderr.length > 0) metadata.stderrSnippet = stderr.slice(0, 200);
  if (timeoutMs !== undefined && record.killed === true && record.signal === "SIGTERM") {
    metadata.timedOut = true;
  }
  return metadata;
}

interface SpawnResultMetadata {
  killed?: boolean | undefined;
  signal?: NodeJS.Signals | string | undefined;
  stderrSnippet?: string | undefined;
  systemCode?: string | undefined;
  timedOut?: boolean | undefined;
}

function recoveryHintForPlatform(platform: NodeJS.Platform): string {
  switch (platform) {
    case "linux":
      return "Install libsecret-tools: sudo apt install libsecret-tools";
    case "darwin":
      return "Ensure /usr/bin/security exists and the login keychain is unlocked";
    case "win32":
      return "Ensure Windows PowerShell exists under %SystemRoot%\\System32\\WindowsPowerShell\\v1.0";
    default:
      return "Install and unlock the OS keyring command for this platform";
  }
}

function hasUnpairedSurrogate(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) return true;
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      return true;
    }
  }
  return false;
}

function normalizeOutput(value: unknown): string {
  if (typeof value === "string") return value;
  if (Buffer.isBuffer(value)) return value.toString("utf8");
  if (value === undefined || value === null) return "";
  return String(value);
}
