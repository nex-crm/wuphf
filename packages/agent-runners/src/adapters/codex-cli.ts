import { type ChildProcessWithoutNullStreams, type SpawnOptions, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { realpathSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import type { Readable } from "node:stream";

import {
  type AgentId,
  asAgentSlug,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asRunnerId,
  asTaskId,
  type CostLedgerEntry,
  type CostUnits,
  CredentialHandle,
  type CredentialScope,
  type MicroUsd,
  type ProviderKind,
  type ReceiptId,
  type ReceiptSnapshot,
  type RunnerEvent,
  type RunnerId,
  type RunnerSpawnRequest,
  SanitizedString,
  sha256Hex,
  type TaskId,
} from "@wuphf/protocol";

import { CodexCliNotAvailable, ReceiptWriteFailed, RunnerSpawnFailed } from "../errors.ts";
import { DEFAULT_MAX_EVENT_HISTORY, RunnerEventHub } from "../internal/event-hub.ts";
import { LifecycleStateMachine } from "../lifecycle.ts";
import type { AgentRunner, RunnerSpawnDeps, SpawnAgentRunner } from "../runner.ts";

export type CodexCliSandboxMode = "read-only" | "workspace-write" | "danger-full-access";

export interface CodexCliSpawnOptions {
  readonly env: NodeJS.ProcessEnv;
  readonly cwd?: string | undefined;
}

export interface CodexCliChildProcess {
  readonly stdout: Readable;
  readonly stderr: Readable;
  kill(signal?: NodeJS.Signals): boolean;
  once(event: "exit", listener: (code: number | null, signal: NodeJS.Signals | null) => void): this;
  once(event: "error", listener: (error: Error) => void): this;
}

export type CodexCliSpawner = (
  command: string,
  args: readonly string[],
  options: CodexCliSpawnOptions,
) => CodexCliChildProcess;

export interface CodexCliCostEstimateInput {
  readonly providerKind: ProviderKind;
  readonly model: string;
  readonly totalTokens: number;
  readonly units: CostUnits;
}

export interface CodexCliAdapterOptions {
  readonly binaryPath?: string | undefined;
  readonly candidatePaths?: readonly string[] | undefined;
  readonly enforceTrustedCommand?: boolean | undefined;
  readonly spawner?: CodexCliSpawner | undefined;
  readonly now?: (() => Date) | undefined;
  readonly runnerIdFactory?: (() => RunnerId) | undefined;
  readonly receiptIdFactory?: (() => ReceiptId) | undefined;
  readonly taskIdFactory?: (() => TaskId) | undefined;
  readonly maxEventHistory?: number | undefined;
  readonly sandbox?: CodexCliSandboxMode | undefined;
  readonly profile?: string | undefined;
  readonly outputLastMessagePath?: string | ((runnerId: RunnerId) => string) | undefined;
  readonly costEstimator?:
    | ((input: CodexCliCostEstimateInput) => MicroUsd | null | undefined)
    | undefined;
}

interface ExitResult {
  readonly code: number;
  readonly signal: NodeJS.Signals | null;
  readonly error?: Error | undefined;
}

const DEFAULT_CODEX_CANDIDATES = [
  "/opt/homebrew/bin/codex",
  "/usr/local/bin/codex",
  "/usr/bin/codex",
  "/home/linuxbrew/.linuxbrew/bin/codex",
] as const;
const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const DEFAULT_GRACE_PERIOD_MS = 5_000;
const DEFAULT_SANDBOX: CodexCliSandboxMode = "workspace-write";
const DEFAULT_PROFILE = "auto";
const STDOUT_CHUNK_BYTES = 256;

export function createCodexCliRunner(options: CodexCliAdapterOptions = {}): SpawnAgentRunner {
  const commandPath = resolveCodexCommand(options);
  const spawner = options.spawner ?? nodeSpawner;
  return async (request, deps) => {
    if (request.kind !== "codex-cli") {
      throw new RunnerSpawnFailed(`Codex CLI adapter cannot run ${request.kind}`);
    }
    const scope = CredentialHandle.scope(deps.credential);
    const secretEnvVar = secretEnvVarForScope(scope);
    const providerKind = providerKindForScope(scope);
    const secret = await deps.secretReader(deps.credential);
    const runnerId = options.runnerIdFactory?.() ?? randomRunnerId();
    const runner = new CodexCliAgentRunner({
      commandPath,
      costEstimator: options.costEstimator,
      deps,
      maxEventHistory: options.maxEventHistory ?? DEFAULT_MAX_EVENT_HISTORY,
      now: options.now ?? (() => new Date()),
      outputLastMessagePath: outputLastMessagePath(options.outputLastMessagePath, runnerId),
      profile: options.profile ?? DEFAULT_PROFILE,
      providerKind,
      receiptIdFactory: options.receiptIdFactory ?? randomReceiptId,
      request,
      runnerId,
      sandbox: options.sandbox ?? DEFAULT_SANDBOX,
      secret,
      secretEnvVar,
      spawner,
      taskIdFactory: options.taskIdFactory ?? randomTaskId,
    });
    runner.start();
    return runner;
  };
}

class CodexCliAgentRunner implements AgentRunner {
  readonly id: RunnerId;
  readonly kind = "codex-cli" as const;
  readonly agentId: AgentId;

  readonly #commandPath: string;
  readonly #costEstimator: CodexCliAdapterOptions["costEstimator"];
  readonly #deps: RunnerSpawnDeps;
  readonly #hub: RunnerEventHub;
  readonly #lifecycle: LifecycleStateMachine;
  readonly #now: () => Date;
  readonly #outputLastMessagePath: string;
  readonly #profile: string;
  readonly #providerKind: ProviderKind;
  readonly #receiptIdFactory: () => ReceiptId;
  readonly #request: RunnerSpawnRequest;
  readonly #sandbox: CodexCliSandboxMode;
  readonly #secret: string;
  readonly #secretEnvVar: "ANTHROPIC_API_KEY" | "OPENAI_API_KEY";
  readonly #spawner: CodexCliSpawner;
  readonly #taskIdFactory: () => TaskId;
  #child: CodexCliChildProcess | null = null;
  #done: Promise<void> | null = null;
  #failed = false;
  #finalText = "";
  #lastCostMicroUsd: MicroUsd = asMicroUsd(0);
  #lastUsage: CostUnits = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
  #terminatePromise: Promise<void> | null = null;
  #unrecognizedLineCount = 0;

  constructor(args: {
    readonly commandPath: string;
    readonly costEstimator: CodexCliAdapterOptions["costEstimator"];
    readonly deps: RunnerSpawnDeps;
    readonly maxEventHistory: number;
    readonly now: () => Date;
    readonly outputLastMessagePath: string;
    readonly profile: string;
    readonly providerKind: ProviderKind;
    readonly receiptIdFactory: () => ReceiptId;
    readonly request: RunnerSpawnRequest;
    readonly runnerId: RunnerId;
    readonly sandbox: CodexCliSandboxMode;
    readonly secret: string;
    readonly secretEnvVar: "ANTHROPIC_API_KEY" | "OPENAI_API_KEY";
    readonly spawner: CodexCliSpawner;
    readonly taskIdFactory: () => TaskId;
  }) {
    this.id = args.runnerId;
    this.agentId = args.request.agentId;
    this.#commandPath = args.commandPath;
    this.#costEstimator = args.costEstimator;
    this.#deps = args.deps;
    this.#hub = new RunnerEventHub(args.maxEventHistory);
    this.#lifecycle = new LifecycleStateMachine(args.runnerId);
    this.#now = args.now;
    this.#outputLastMessagePath = args.outputLastMessagePath;
    this.#profile = args.profile;
    this.#providerKind = args.providerKind;
    this.#receiptIdFactory = args.receiptIdFactory;
    this.#request = args.request;
    this.#sandbox = args.sandbox;
    this.#secret = args.secret;
    this.#secretEnvVar = args.secretEnvVar;
    this.#spawner = args.spawner;
    this.#taskIdFactory = args.taskIdFactory;
  }

  events() {
    return this.#hub.events();
  }

  start(): void {
    if (this.#done !== null) return;
    this.#done = this.#run().finally(() => {
      this.#hub.close();
    });
  }

  async terminate(opts: { readonly gracePeriodMs?: number } = {}): Promise<void> {
    if (this.#terminatePromise === null) {
      this.#terminatePromise = this.#doTerminate(opts.gracePeriodMs ?? DEFAULT_GRACE_PERIOD_MS);
    }
    return this.#terminatePromise;
  }

  async #run(): Promise<void> {
    let exit: ExitResult = { code: 1, signal: null };
    try {
      this.#lifecycle.markRunning();
      const env = sanitizedCodexEnv(this.#commandPath, this.#secretEnvVar, this.#secret);
      this.#child = this.#spawner(this.#commandPath, this.#codexArgs(), {
        env,
        cwd: this.#request.cwd,
      });
      const exitPromise = waitForExit(this.#child);
      const stdout = this.#consumeStdout(this.#child.stdout);
      const stderr = this.#consumeStderr(this.#child.stderr);
      await this.#emit({ kind: "started", runnerId: this.id, at: this.#isoNow() });
      exit = await exitPromise;
      await Promise.all([stdout, stderr]);
      await this.#emitUnrecognizedSummary();
      if (exit.error !== undefined) {
        await this.#fail(exit.error.message);
        this.#lifecycle.markStopped({ exitCode: 1, error: exit.error.message });
        return;
      }
      if (exit.code !== 0) {
        const signalText = exit.signal === null ? "" : ` (${exit.signal})`;
        const message = `Codex CLI exited with code ${exit.code}${signalText}`;
        await this.#fail(message);
        this.#lifecycle.markStopped({ exitCode: exit.code, error: message });
        return;
      }
      if (this.#failed) {
        this.#lifecycle.markStopped({ exitCode: 1, error: "runner failed" });
        return;
      }
      await this.#writeReceiptAndFinish(exit.code);
      this.#lifecycle.markStopped({ exitCode: exit.code });
    } catch (error) {
      const message = errorMessage(error);
      await this.#fail(message);
      this.#lifecycle.markStopped({ exitCode: exit.code, error: message });
    }
  }

  async #doTerminate(gracePeriodMs: number): Promise<void> {
    const transitioned = this.#lifecycle.beginStopping();
    const child = this.#child;
    if (transitioned && child !== null) {
      child.kill("SIGINT");
      const hardKill = setTimeout(() => {
        child.kill("SIGKILL");
      }, gracePeriodMs);
      hardKill.unref();
      try {
        await (this.#done ?? this.#lifecycle.stopped());
      } finally {
        clearTimeout(hardKill);
      }
      return;
    }
    await (this.#done ?? this.#lifecycle.stopped());
  }

  async #consumeStdout(stream: Readable): Promise<void> {
    const blockLines: string[] = [];
    let buffered = "";
    let sawDelimiter = false;
    for await (const chunk of stream) {
      buffered += chunkToString(chunk);
      let newline = buffered.indexOf("\n");
      while (newline >= 0) {
        const line = buffered.slice(0, newline + 1);
        buffered = buffered.slice(newline + 1);
        if (isDelimiterLine(line)) {
          await this.#processCodexBlock(blockLines.splice(0), false);
          sawDelimiter = true;
        } else {
          blockLines.push(line);
        }
        newline = buffered.indexOf("\n");
      }
    }
    if (buffered.length > 0) {
      if (isDelimiterLine(buffered)) {
        await this.#processCodexBlock(blockLines.splice(0), false);
        sawDelimiter = true;
      } else {
        blockLines.push(buffered);
      }
    }
    await this.#processCodexBlock(blockLines, sawDelimiter);
  }

  async #consumeStderr(stream: Readable): Promise<void> {
    for await (const chunk of stream) {
      const text = chunkToString(chunk);
      if (text.length > 0) {
        await this.#emit({ kind: "stderr", runnerId: this.id, chunk: text, at: this.#isoNow() });
      }
    }
  }

  async #processCodexBlock(lines: readonly string[], isFinalBlock: boolean): Promise<void> {
    if (this.#failed || lines.length === 0) return;
    if (isFinalBlock) {
      await this.#emitStdoutText(lines.filter((line) => !isHookLine(line)).join(""), true);
      return;
    }
    for (const rawLine of lines) {
      if (this.#failed) return;
      const line = stripLineEnding(rawLine);
      if (isHookLine(line)) continue;
      const exec = parseExecLine(line);
      if (exec !== null) {
        await this.#emit({
          kind: "stderr",
          runnerId: this.id,
          chunk: `codex command: ${exec.command} (cwd: ${exec.cwd})\n`,
          at: this.#isoNow(),
        });
        continue;
      }
      if (isToolExitLine(line)) continue;
      const totalTokens = parseTokensUsedLine(line);
      if (totalTokens !== null) {
        await this.#recordTokenUsage(totalTokens);
        continue;
      }
      this.#unrecognizedLineCount += line.trim().length === 0 ? 0 : 1;
      await this.#emitStdoutText(rawLine, false);
    }
  }

  async #recordTokenUsage(totalTokens: number): Promise<void> {
    const units = {
      inputTokens: totalTokens,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheCreationTokens: 0,
    };
    this.#lastUsage = units;
    const model = this.#request.model ?? "codex-cli";
    const entry: CostLedgerEntry = {
      agentSlug: asAgentSlug(this.agentId),
      providerKind: this.#providerKind,
      model,
      amountMicroUsd: this.#estimateCostMicroUsd(model, totalTokens, units),
      units,
      occurredAt: this.#now(),
    };
    this.#lastCostMicroUsd = entry.amountMicroUsd;
    await this.#deps.costLedger.record(entry);
    await this.#emit({ kind: "cost", runnerId: this.id, entry, at: this.#isoNow() });
  }

  #estimateCostMicroUsd(model: string, totalTokens: number, units: CostUnits): MicroUsd {
    try {
      const estimate = this.#costEstimator?.({
        model,
        providerKind: this.#providerKind,
        totalTokens,
        units,
      });
      return estimate ?? asMicroUsd(0);
    } catch {
      return asMicroUsd(0);
    }
  }

  async #emitStdoutText(text: string, finalMessage: boolean): Promise<void> {
    if (text.length === 0) return;
    if (finalMessage) {
      this.#finalText += text;
    }
    for (const chunk of chunkText(text, STDOUT_CHUNK_BYTES)) {
      await this.#emit({ kind: "stdout", runnerId: this.id, chunk, at: this.#isoNow() });
    }
  }

  async #emitUnrecognizedSummary(): Promise<void> {
    if (this.#unrecognizedLineCount === 0) return;
    await this.#emit({
      kind: "stderr",
      runnerId: this.id,
      chunk: `codex output parser saw ${this.#unrecognizedLineCount} unrecognized line(s)\n`,
      at: this.#isoNow(),
    });
  }

  async #writeReceiptAndFinish(exitCode: number): Promise<void> {
    const receipt = this.#buildReceipt();
    try {
      const stored = await this.#deps.receiptStore.put(receipt);
      if (!stored.stored) {
        throw new ReceiptWriteFailed(this.id, "receipt store reported stored=false");
      }
    } catch (error) {
      const message = errorMessage(error);
      await this.#fail(message);
      throw new ReceiptWriteFailed(this.id, message, { cause: error });
    }
    await this.#emit({
      kind: "receipt",
      runnerId: this.id,
      receiptId: receipt.id,
      at: this.#isoNow(),
    });
    await this.#emit({ kind: "finished", runnerId: this.id, exitCode, at: this.#isoNow() });
  }

  #buildReceipt(): ReceiptSnapshot {
    const startedAt = this.#now();
    const finishedAt = this.#now();
    return {
      id: this.#receiptIdFactory(),
      agentSlug: asAgentSlug(this.agentId),
      taskId: this.#request.taskId ?? this.#taskIdFactory(),
      triggerKind: "human_message",
      triggerRef: this.id,
      startedAt,
      finishedAt,
      status: "ok",
      providerKind: this.#providerKind,
      model: this.#request.model ?? "codex-cli",
      promptHash: sha256Hex(this.#request.prompt),
      toolManifest: sha256Hex("codex-cli:v1"),
      toolCalls: [],
      approvals: [],
      filesChanged: [],
      commits: [],
      sourceReads: [],
      writes: [],
      inputTokens: this.#lastUsage.inputTokens,
      outputTokens: this.#lastUsage.outputTokens,
      cacheReadTokens: this.#lastUsage.cacheReadTokens,
      cacheCreationTokens: this.#lastUsage.cacheCreationTokens,
      costUsd: this.#lastCostMicroUsd / 1_000_000,
      finalMessage: SanitizedString.fromUnknown(this.#finalText),
      error: SanitizedString.fromUnknown(""),
      notebookWrites: [],
      wikiWrites: [],
      schemaVersion: 2,
    };
  }

  #codexArgs(): readonly string[] {
    const args = [
      "exec",
      "--sandbox",
      this.#sandbox,
      "--profile",
      this.#profile,
      "--output-last-message",
      this.#outputLastMessagePath,
      "--color",
      "never",
    ];
    if (this.#request.cwd !== undefined) {
      args.push("--cd", this.#request.cwd);
    }
    if (this.#request.model !== undefined) {
      args.push("--model", this.#request.model);
    }
    args.push(this.#request.prompt);
    return args;
  }

  async #emit(event: RunnerEvent): Promise<void> {
    await this.#deps.eventLog.append(event);
    this.#hub.publish(event);
  }

  async #fail(message: string): Promise<void> {
    if (this.#failed) return;
    this.#failed = true;
    const event: RunnerEvent = {
      kind: "failed",
      runnerId: this.id,
      error: message,
      at: this.#isoNow(),
    };
    try {
      await this.#deps.eventLog.append(event);
    } finally {
      this.#hub.publish(event);
    }
  }

  #isoNow(): string {
    return this.#now().toISOString();
  }
}

function resolveCodexCommand(options: CodexCliAdapterOptions): string {
  const candidates =
    options.binaryPath === undefined
      ? (options.candidatePaths ?? defaultCodexCandidates())
      : [options.binaryPath];
  const enforce = options.enforceTrustedCommand ?? options.spawner === undefined;
  const fallback = candidates[0];
  if (fallback === undefined) {
    throw new CodexCliNotAvailable("Codex CLI has no candidate path");
  }
  if (!enforce) {
    if (!path.isAbsolute(fallback)) {
      throw new CodexCliNotAvailable("Codex CLI path must be absolute");
    }
    return fallback;
  }
  for (const candidate of candidates) {
    if (!path.isAbsolute(candidate)) continue;
    try {
      const resolved = realpathSync(candidate);
      const stats = statSync(resolved);
      const parentStats = statSync(path.dirname(resolved));
      if (!stats.isFile()) {
        throw new CodexCliNotAvailable("Codex CLI path does not resolve to a file");
      }
      if (isWritableByNonOwner(stats)) {
        throw new CodexCliNotAvailable("Codex CLI path is writable by non-owner users");
      }
      if (isWritableByNonOwner(parentStats)) {
        throw new CodexCliNotAvailable("Codex CLI parent path is writable by non-owner users");
      }
      return resolved;
    } catch (error) {
      if (error instanceof CodexCliNotAvailable) throw error;
    }
  }
  throw new CodexCliNotAvailable("Codex CLI was not found at a trusted absolute path");
}

function defaultCodexCandidates(): readonly string[] {
  const candidates: string[] = [...DEFAULT_CODEX_CANDIDATES];
  const { PATH: pathEnv } = process.env;
  for (const segment of (pathEnv ?? "").split(path.delimiter)) {
    if (path.isAbsolute(segment)) {
      candidates.push(path.join(segment, "codex"));
    }
  }
  return [...new Set(candidates)];
}

function outputLastMessagePath(
  configured: CodexCliAdapterOptions["outputLastMessagePath"],
  runnerId: RunnerId,
): string {
  if (typeof configured === "function") return configured(runnerId);
  return configured ?? path.join(os.tmpdir(), `${runnerId}-last-message.txt`);
}

function isWritableByNonOwner(stats: { readonly mode: number }): boolean {
  return (stats.mode & 0o022) !== 0;
}

function sanitizedCodexEnv(
  commandPath: string,
  secretEnvVar: "ANTHROPIC_API_KEY" | "OPENAI_API_KEY",
  secret: string,
): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    [secretEnvVar]: secret,
    LC_ALL: "C",
    PATH: path.dirname(commandPath),
  };
  const { HOME, USERPROFILE, USER, USERNAME } = process.env;
  const home = HOME ?? USERPROFILE;
  const user = USER ?? USERNAME;
  if (home !== undefined && home.length > 0) Object.assign(env, { HOME: home });
  if (user !== undefined && user.length > 0) Object.assign(env, { USER: user });
  return env;
}

function nodeSpawner(
  command: string,
  args: readonly string[],
  options: CodexCliSpawnOptions,
): CodexCliChildProcess {
  const spawnOptions: SpawnOptions = {
    env: options.env,
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  };
  if (options.cwd !== undefined) {
    spawnOptions.cwd = options.cwd;
  }
  return spawn(command, [...args], spawnOptions) as ChildProcessWithoutNullStreams;
}

function waitForExit(child: CodexCliChildProcess): Promise<ExitResult> {
  return new Promise((resolve) => {
    let settled = false;
    child.once("error", (error) => {
      if (settled) return;
      settled = true;
      resolve({ code: 1, signal: null, error });
    });
    child.once("exit", (code, signal) => {
      if (settled) return;
      settled = true;
      resolve({ code: code ?? 1, signal });
    });
  });
}

function secretEnvVarForScope(scope: CredentialScope): "ANTHROPIC_API_KEY" | "OPENAI_API_KEY" {
  switch (scope) {
    case "anthropic":
      return "ANTHROPIC_API_KEY";
    case "openai":
    case "openai-compat":
      return "OPENAI_API_KEY";
    default:
      throw new RunnerSpawnFailed(`Codex CLI cannot use ${scope} credentials`);
  }
}

function providerKindForScope(scope: CredentialScope): ProviderKind {
  switch (scope) {
    case "anthropic":
      return asProviderKind("anthropic");
    case "openai":
      return asProviderKind("openai");
    case "openai-compat":
      return asProviderKind("openai-compat");
    default:
      throw new RunnerSpawnFailed(`Codex CLI cannot record ${scope} costs`);
  }
}

function parseExecLine(line: string): { readonly command: string; readonly cwd: string } | null {
  const match = /^exec\s+(.+)\s+in\s+(.+)$/.exec(line.trim());
  if (match === null) return null;
  const command = match[1];
  const cwd = match[2];
  if (command === undefined || cwd === undefined) return null;
  return { command, cwd };
}

function parseTokensUsedLine(line: string): number | null {
  const match = /^tokens used:\s*([0-9][0-9_,]*)\b/i.exec(line.trim());
  const raw = match?.[1];
  if (raw === undefined) return null;
  const parsed = Number.parseInt(raw.replaceAll(/[_,]/g, ""), 10);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function isToolExitLine(line: string): boolean {
  return /^(succeeded|failed) in \d+(?:\.\d+)?(?:ms|s)\b/i.test(line.trim());
}

function isHookLine(line: string): boolean {
  return /^hook:\s+/i.test(stripLineEnding(line).trim());
}

function isDelimiterLine(line: string): boolean {
  return /^-{8,}$/.test(stripLineEnding(line).trim());
}

function stripLineEnding(line: string): string {
  return line.replace(/\r?\n$/, "");
}

function chunkText(text: string, maxBytes: number): string[] {
  const chunks: string[] = [];
  let current = "";
  let currentBytes = 0;
  for (const char of text) {
    const charBytes = Buffer.byteLength(char, "utf8");
    if (current.length > 0 && currentBytes + charBytes > maxBytes) {
      chunks.push(current);
      current = "";
      currentBytes = 0;
    }
    current += char;
    currentBytes += charBytes;
  }
  if (current.length > 0) chunks.push(current);
  return chunks;
}

function chunkToString(chunk: unknown): string {
  if (typeof chunk === "string") return chunk;
  if (Buffer.isBuffer(chunk)) return chunk.toString("utf8");
  return String(chunk);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function randomRunnerId(): RunnerId {
  return asRunnerId(`run_${randomBase32(32)}`);
}

function randomReceiptId(): ReceiptId {
  return asReceiptId(randomBase32(26));
}

function randomTaskId(): TaskId {
  return asTaskId(randomBase32(26));
}

function randomBase32(length: number): string {
  const bytes = randomBytes(length);
  let out = "";
  for (let index = 0; index < length; index += 1) {
    const byte = bytes[index];
    if (byte === undefined) {
      throw new Error("random byte missing");
    }
    const char = CROCKFORD[byte % CROCKFORD.length];
    if (char === undefined) {
      throw new Error("random alphabet lookup failed");
    }
    out += char;
  }
  return out;
}
