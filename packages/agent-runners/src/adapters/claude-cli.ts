import { type ChildProcessWithoutNullStreams, type SpawnOptions, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { realpathSync, statSync } from "node:fs";
import path from "node:path";
import type { Readable } from "node:stream";
import { StringDecoder } from "node:string_decoder";

import {
  type AgentId,
  asAgentSlug,
  asReceiptId,
  asRunnerId,
  asTaskId,
  type CostLedgerEntry,
  type CostUnits,
  type ReceiptId,
  type ReceiptSnapshot,
  type RunnerEvent,
  type RunnerFailureCode,
  type RunnerId,
  type RunnerSpawnRequest,
  SanitizedString,
  sha256Hex,
  type TaskId,
} from "@wuphf/protocol";

import { ClaudeCliNotAvailable, ReceiptWriteFailed, RunnerSpawnFailed } from "../errors.ts";
import {
  BoundedLineBuffer,
  chunkStdio,
  DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES,
  RunnerInputBufferOverflow,
} from "../internal/chunk.ts";
import {
  errorMessage,
  RunnerFailure,
  runnerFailureFromError,
  type TerminalCleanupTarget,
  terminalCleanup,
} from "../internal/cleanup.ts";
import { trustedCostModel, validatedCostEntry } from "../internal/cost.ts";
import {
  DEFAULT_MAX_EVENT_HISTORY,
  RunnerEventHub,
  SerializedEmitter,
} from "../internal/event-hub.ts";
import { createSecretStreamingRedactor, type StreamingRedactor } from "../internal/redact.ts";
import { LifecycleStateMachine } from "../lifecycle.ts";
import type { AgentRunner, RunnerSpawnDeps, SpawnAgentRunner } from "../runner.ts";

export interface ClaudeCliSpawnOptions {
  readonly env: NodeJS.ProcessEnv;
  readonly cwd?: string | undefined;
}

export interface ClaudeCliChildProcess {
  readonly stdout: Readable;
  readonly stderr: Readable;
  kill(signal?: NodeJS.Signals): boolean;
  once(event: "exit", listener: (code: number | null, signal: NodeJS.Signals | null) => void): this;
  once(event: "error", listener: (error: Error) => void): this;
}

export type ClaudeCliSpawner = (
  command: string,
  args: readonly string[],
  options: ClaudeCliSpawnOptions,
) => ClaudeCliChildProcess;

export interface ClaudeCliAdapterOptions {
  readonly binaryPath?: string | undefined;
  readonly candidatePaths?: readonly string[] | undefined;
  readonly enforceTrustedCommand?: boolean | undefined;
  readonly spawner?: ClaudeCliSpawner | undefined;
  readonly now?: (() => Date) | undefined;
  readonly runnerIdFactory?: (() => RunnerId) | undefined;
  readonly receiptIdFactory?: (() => ReceiptId) | undefined;
  readonly taskIdFactory?: (() => TaskId) | undefined;
  readonly maxEventHistory?: number | undefined;
}

interface ExitResult {
  readonly code: number;
  readonly signal: NodeJS.Signals | null;
  readonly error?: Error | undefined;
}

interface Usage {
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cacheReadTokens: number;
  readonly cacheCreationTokens: number;
  readonly model?: string | undefined;
}

const DEFAULT_CLAUDE_CANDIDATES = [
  "/opt/homebrew/bin/claude",
  "/usr/local/bin/claude",
  "/usr/bin/claude",
] as const;
const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
const DEFAULT_GRACE_PERIOD_MS = 5_000;

export function createClaudeCliRunner(options: ClaudeCliAdapterOptions = {}): SpawnAgentRunner {
  const commandPath = resolveClaudeCommand(options);
  const spawner = options.spawner ?? nodeSpawner;
  return async (request, deps) => {
    if (request.kind !== "claude-cli") {
      throw new RunnerSpawnFailed(`Claude CLI adapter cannot run ${request.kind}`);
    }
    const secret = await deps.secretReader(deps.credential);
    const runner = new ClaudeCliAgentRunner({
      commandPath,
      deps,
      maxEventHistory: options.maxEventHistory ?? DEFAULT_MAX_EVENT_HISTORY,
      now: options.now ?? (() => new Date()),
      receiptIdFactory: options.receiptIdFactory ?? randomReceiptId,
      request,
      runnerId: options.runnerIdFactory?.() ?? randomRunnerId(),
      secret,
      spawner,
      taskIdFactory: options.taskIdFactory ?? randomTaskId,
    });
    runner.start();
    return runner;
  };
}

class ClaudeCliAgentRunner implements AgentRunner {
  readonly id: RunnerId;
  readonly kind = "claude-cli" as const;
  readonly agentId: AgentId;

  readonly #commandPath: string;
  readonly #deps: RunnerSpawnDeps;
  readonly #emitter: SerializedEmitter;
  readonly #hub: RunnerEventHub;
  readonly #lifecycle: LifecycleStateMachine;
  readonly #now: () => Date;
  readonly #receiptId: ReceiptId;
  readonly #redactor: StreamingRedactor;
  readonly #request: RunnerSpawnRequest;
  readonly #secret: string;
  readonly #spawner: ClaudeCliSpawner;
  readonly #taskId: TaskId;
  #child: ClaudeCliChildProcess | null = null;
  #childExited = false;
  #done: Promise<void> | null = null;
  #exitPromise: Promise<ExitResult> | null = null;
  #failed = false;
  #finalText = "";
  #lastUsage: CostUnits = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
  #redactionTarget: "stdout" | "stderr" | null = null;
  #startedAt: Date | null = null;
  #terminatePromise: Promise<void> | null = null;

  constructor(args: {
    readonly commandPath: string;
    readonly deps: RunnerSpawnDeps;
    readonly maxEventHistory: number;
    readonly now: () => Date;
    readonly receiptIdFactory: () => ReceiptId;
    readonly request: RunnerSpawnRequest;
    readonly runnerId: RunnerId;
    readonly secret: string;
    readonly spawner: ClaudeCliSpawner;
    readonly taskIdFactory: () => TaskId;
  }) {
    this.id = args.runnerId;
    this.agentId = args.request.agentId;
    this.#commandPath = args.commandPath;
    this.#deps = args.deps;
    this.#hub = new RunnerEventHub(args.maxEventHistory);
    this.#emitter = new SerializedEmitter({ eventLog: args.deps.eventLog, eventHub: this.#hub });
    this.#lifecycle = new LifecycleStateMachine(args.runnerId);
    this.#now = args.now;
    this.#receiptId = args.receiptIdFactory();
    this.#redactor = createSecretStreamingRedactor(args.secret);
    this.#request = args.request;
    this.#secret = args.secret;
    this.#spawner = args.spawner;
    this.#taskId = args.request.taskId ?? args.taskIdFactory();
  }

  events(options?: Parameters<RunnerEventHub["events"]>[0]) {
    return this.#hub.events(options);
  }

  eventRecords(options?: Parameters<RunnerEventHub["eventRecords"]>[0]) {
    return this.#hub.eventRecords(options);
  }

  start(): void {
    if (this.#done !== null) return;
    this.#done = this.#run().finally(async () => {
      await this.#emitter.close();
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
      this.#startedAt = this.#now();
      const env = sanitizedClaudeEnv(this.#commandPath, this.#secret);
      const extraArgs =
        this.#request.options?.kind === "claude-cli" ? (this.#request.options.extraArgs ?? []) : [];
      this.#child = this.#spawner(
        this.#commandPath,
        ["--print", "--output-format", "stream-json", ...extraArgs, "--", this.#request.prompt],
        { env, cwd: this.#request.cwd },
      );
      this.#childExited = false;
      this.#exitPromise = waitForExit(this.#child).then((result) => {
        this.#childExited = true;
        return result;
      });
      await this.#emit({ kind: "started", runnerId: this.id, at: this.#isoNow() });
      const stdout = this.#consumeStdout(this.#child.stdout);
      const stderr = this.#consumeStderr(this.#child.stderr);
      [exit] = await Promise.all([this.#exitPromise, stdout, stderr]);
      if (exit.error !== undefined) {
        throw new RunnerFailure(exit.error.message, "spawn_failed", { cause: exit.error });
      }
      if (exit.code !== 0) {
        const signalText = exit.signal === null ? "" : ` (${exit.signal})`;
        const message = `Claude CLI exited with code ${exit.code}${signalText}`;
        const code =
          this.#lifecycle.snapshot().phase === "stopping"
            ? "terminated_by_request"
            : "subprocess_crashed";
        throw new RunnerFailure(message, code);
      }
      if (this.#failed) {
        await this.#lifecycle.stopped().catch(() => undefined);
        return;
      }
      await this.#writeReceiptAndFinish(exit.code);
    } catch (error) {
      const fallbackCode = this.#child === null ? "spawn_failed" : "subprocess_crashed";
      await this.#cleanupWithFailure(error, fallbackCode, exit.code);
    }
  }

  async #doTerminate(gracePeriodMs: number): Promise<void> {
    const failure = new RunnerFailure("runner terminated by request", "terminated_by_request");
    await this.#cleanupWithFailure(failure, "terminated_by_request", 1, gracePeriodMs);
    await (this.#done ?? this.#lifecycle.stopped()).catch(() => undefined);
  }

  async #consumeStdout(stream: Readable): Promise<void> {
    const decoder = new StringDecoder("utf8");
    const buffer = new BoundedLineBuffer(DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES);
    try {
      for await (const chunk of stream) {
        for (const line of buffer.push(decodeChunk(decoder, chunk))) {
          await this.#handleClaudeLine(line);
        }
      }
      const tail = decoder.end();
      if (tail.length > 0) {
        for (const line of buffer.push(tail)) {
          await this.#handleClaudeLine(line);
        }
      }
      for (const line of buffer.flush()) {
        await this.#handleClaudeLine(line);
      }
    } catch (error) {
      if (error instanceof RunnerInputBufferOverflow) {
        throw new RunnerFailure(error.message, "runner_input_buffer_overflow", { cause: error });
      }
      throw error;
    }
  }

  async #consumeStderr(stream: Readable): Promise<void> {
    const decoder = new StringDecoder("utf8");
    for await (const chunk of stream) {
      const text = decodeChunk(decoder, chunk);
      if (text.length > 0) {
        await this.#emitText("stderr", text);
      }
    }
    const tail = decoder.end();
    if (tail.length > 0) {
      await this.#emitText("stderr", tail);
    }
  }

  async #handleClaudeLine(line: string): Promise<void> {
    const trimmed = line.trim();
    if (trimmed.length === 0 || this.#failed) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmed);
    } catch (error) {
      throw new RunnerFailure(
        `Claude CLI emitted malformed JSON: ${errorMessage(error)}`,
        "unrecognized_provider_response",
        { cause: error },
      );
    }
    if (!isRecord(parsed)) {
      throw new RunnerFailure(
        "Claude CLI emitted a non-object JSON line",
        "unrecognized_provider_response",
      );
    }
    const type = recordString(parsed, "type");
    if (type === "error") {
      throw new RunnerFailure(
        recordString(parsed, "message") ?? "Claude CLI emitted an error",
        "provider_returned_error",
      );
    }
    const text = extractText(parsed);
    if (text.length > 0) {
      await this.#emitStdout(text);
    }
    const usage = extractUsage(parsed);
    if (usage !== null) {
      this.#lastUsage = usage;
      const entry = this.#costEntry(usage);
      await this.#recordCost(entry);
      await this.#emit({ kind: "cost", runnerId: this.id, entry, at: this.#isoNow() });
    }
  }

  async #emitStdout(text: string): Promise<void> {
    const redacted = await this.#redactForTarget("stdout", text);
    for (const chunk of chunkStdio(redacted)) {
      await this.#emit({ kind: "stdout", runnerId: this.id, chunk, at: this.#isoNow() });
      this.#finalText += chunk;
    }
  }

  async #recordCost(entry: CostLedgerEntry): Promise<void> {
    try {
      await this.#deps.costLedger.record(entry);
    } catch (error) {
      throw new RunnerFailure(errorMessage(error), "cost_ledger_write_failed", { cause: error });
    }
  }

  async #writeReceiptAndFinish(exitCode: number): Promise<void> {
    await this.#flushRedactorCarry();
    const receipt = this.#buildReceipt();
    try {
      const stored = await this.#deps.receiptStore.put(receipt);
      if (!stored.stored) {
        throw new ReceiptWriteFailed(this.id, "receipt store reported stored=false");
      }
    } catch (error) {
      const message = errorMessage(error);
      throw new RunnerFailure(message, "receipt_write_failed", {
        cause: new ReceiptWriteFailed(this.id, message, { cause: error }),
      });
    }
    if (!this.#lifecycle.tryTerminate("finished")) return;
    try {
      await this.#emit({
        kind: "receipt",
        runnerId: this.id,
        receiptId: receipt.id,
        at: this.#isoNow(),
      });
      await this.#emit({ kind: "finished", runnerId: this.id, exitCode, at: this.#isoNow() });
    } finally {
      this.#lifecycle.markStopped({ exitCode });
    }
  }

  #buildReceipt(): ReceiptSnapshot {
    const usage = this.#lastUsage;
    const startedAt = this.#startedAt ?? this.#now();
    const finishedAt = this.#now();
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    return {
      id: this.#receiptId,
      agentSlug: asAgentSlug(this.agentId),
      taskId: this.#taskId,
      triggerKind: "human_message",
      triggerRef: this.id,
      startedAt,
      finishedAt,
      status: "ok",
      providerKind: this.#deps.resolvedProviderKind,
      model: trustedCostModel({ request: this.#request, defaultModel: "claude-cli" }),
      promptHash: sha256Hex(this.#request.prompt),
      toolManifest: sha256Hex("claude-cli:v1"),
      toolCalls: [],
      approvals: [],
      filesChanged: [],
      commits: [],
      sourceReads: [],
      writes: [],
      inputTokens: usage.inputTokens,
      outputTokens: usage.outputTokens,
      cacheReadTokens: usage.cacheReadTokens,
      cacheCreationTokens: usage.cacheCreationTokens,
      costUsd: amount / 1_000_000,
      finalMessage: SanitizedString.fromUnknown(this.#finalText),
      error: SanitizedString.fromUnknown(""),
      notebookWrites: [],
      wikiWrites: [],
      schemaVersion: 2,
    };
  }

  #buildFailureReceipt(message: string): ReceiptSnapshot {
    const usage = this.#lastUsage;
    const startedAt = this.#startedAt ?? this.#now();
    const finishedAt = this.#now();
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    return {
      id: this.#receiptId,
      agentSlug: asAgentSlug(this.agentId),
      taskId: this.#taskId,
      triggerKind: "human_message",
      triggerRef: this.id,
      startedAt,
      finishedAt,
      status: "error",
      providerKind: this.#deps.resolvedProviderKind,
      model: trustedCostModel({ request: this.#request, defaultModel: "claude-cli" }),
      promptHash: sha256Hex(this.#request.prompt),
      toolManifest: sha256Hex("claude-cli:v1"),
      toolCalls: [],
      approvals: [],
      filesChanged: [],
      commits: [],
      sourceReads: [],
      writes: [],
      inputTokens: usage.inputTokens,
      outputTokens: usage.outputTokens,
      cacheReadTokens: usage.cacheReadTokens,
      cacheCreationTokens: usage.cacheCreationTokens,
      costUsd: amount / 1_000_000,
      finalMessage: SanitizedString.fromUnknown(this.#finalText),
      error: SanitizedString.fromUnknown(message),
      notebookWrites: [],
      wikiWrites: [],
      schemaVersion: 2,
    };
  }

  #costEntry(usage: Usage): CostLedgerEntry {
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    return validatedCostEntry({
      request: this.#request,
      providerKind: this.#deps.resolvedProviderKind,
      defaultModel: "claude-cli",
      reportedModel: usage.model,
      amountMicroUsd: amount,
      units: {
        inputTokens: usage.inputTokens,
        outputTokens: usage.outputTokens,
        cacheReadTokens: usage.cacheReadTokens,
        cacheCreationTokens: usage.cacheCreationTokens,
      },
      occurredAt: this.#now(),
      receiptId: this.#receiptId,
      taskId: this.#taskId,
    });
  }

  async #emit(event: RunnerEvent): Promise<void> {
    try {
      await this.#emitter.emit(event);
    } catch (error) {
      throw new RunnerFailure(errorMessage(error), "event_log_write_failed", { cause: error });
    }
  }

  async #cleanupWithFailure(
    error: unknown,
    fallbackCode: RunnerFailureCode,
    exitCode: number,
    gracePeriodMs?: number | undefined,
  ): Promise<void> {
    const failure = runnerFailureFromError(error, fallbackCode);
    if (this.#failed) return;
    this.#failed = true;
    await this.#flushRedactorCarry();
    const message = `${this.#redactor.redact(failure.message)}${this.#redactor.flush()}`;
    this.#redactionTarget = null;
    const event: RunnerEvent = {
      kind: "failed",
      runnerId: this.id,
      error: message,
      code: failure.code,
      at: this.#isoNow(),
    };
    await terminalCleanup({
      lifecycle: this.#lifecycle,
      target: this.#cleanupTarget(),
      emitter: this.#emitter,
      receiptStore: this.#deps.receiptStore,
      failureReceipt: this.#buildFailureReceipt(message),
      failureCode: failure.code,
      failureEvent: event,
      gracePeriodMs,
      stopped: { exitCode, error: message },
    });
  }

  #isoNow(): string {
    return this.#now().toISOString();
  }

  #cleanupTarget(): TerminalCleanupTarget | undefined {
    const child = this.#child;
    const exitPromise = this.#exitPromise;
    if (child === null || exitPromise === null) return undefined;
    return {
      kind: "child",
      child: {
        isAlive: () => !this.#childExited,
        kill: (signal) => {
          child.kill(signal);
        },
        wait: async () => {
          await exitPromise;
        },
      },
    };
  }

  async #emitText(target: "stdout" | "stderr", text: string): Promise<void> {
    const redacted = await this.#redactForTarget(target, text);
    for (const chunk of chunkStdio(redacted)) {
      await this.#emit({ kind: target, runnerId: this.id, chunk, at: this.#isoNow() });
      if (target === "stdout") {
        this.#finalText += chunk;
      }
    }
  }

  async #redactForTarget(target: "stdout" | "stderr", text: string): Promise<string> {
    if (this.#redactionTarget !== null && this.#redactionTarget !== target) {
      await this.#flushRedactorCarry();
    }
    this.#redactionTarget = target;
    return this.#redactor.redact(text);
  }

  async #flushRedactorCarry(): Promise<void> {
    const target = this.#redactionTarget;
    const carry = this.#redactor.flush();
    this.#redactionTarget = null;
    if (target === null || carry.length === 0) return;
    for (const chunk of chunkStdio(carry)) {
      await this.#emit({ kind: target, runnerId: this.id, chunk, at: this.#isoNow() });
      if (target === "stdout") {
        this.#finalText += chunk;
      }
    }
  }
}

function resolveClaudeCommand(options: ClaudeCliAdapterOptions): string {
  const candidates =
    options.binaryPath === undefined
      ? (options.candidatePaths ?? DEFAULT_CLAUDE_CANDIDATES)
      : [options.binaryPath];
  const enforce = options.enforceTrustedCommand ?? options.spawner === undefined;
  const fallback = candidates[0];
  if (fallback === undefined) {
    throw new ClaudeCliNotAvailable("Claude CLI has no candidate path");
  }
  if (!enforce) {
    if (!path.isAbsolute(fallback)) {
      throw new ClaudeCliNotAvailable("Claude CLI path must be absolute");
    }
    return fallback;
  }
  for (const candidate of candidates) {
    if (!path.isAbsolute(candidate)) continue;
    try {
      const resolved = realpathSync(candidate);
      const stats = statSync(resolved);
      if ((stats.mode & 0o022) !== 0) {
        throw new ClaudeCliNotAvailable("Claude CLI path is writable by non-owner users");
      }
      return resolved;
    } catch (error) {
      if (error instanceof ClaudeCliNotAvailable) throw error;
    }
  }
  throw new ClaudeCliNotAvailable("Claude CLI was not found at a trusted absolute path");
}

function sanitizedClaudeEnv(commandPath: string, secret: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    ANTHROPIC_API_KEY: secret,
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
  options: ClaudeCliSpawnOptions,
): ClaudeCliChildProcess {
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

function waitForExit(child: ClaudeCliChildProcess): Promise<ExitResult> {
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

function extractText(record: Readonly<Record<string, unknown>>): string {
  const direct = recordString(record, "text");
  if (direct !== null) return direct;
  const result = recordString(record, "result");
  if (result !== null) return result;
  const delta = recordValue(record, "delta");
  if (isRecord(delta)) {
    const deltaText = recordString(delta, "text");
    if (deltaText !== null) return deltaText;
  }
  const message = recordValue(record, "message");
  if (!isRecord(message)) return "";
  const content = recordValue(message, "content");
  if (!Array.isArray(content)) return "";
  let output = "";
  for (const item of content) {
    if (isRecord(item)) {
      const text = recordString(item, "text");
      if (text !== null) output += text;
    }
  }
  return output;
}

function extractUsage(record: Readonly<Record<string, unknown>>): Usage | null {
  const usage = usageRecord(recordValue(record, "usage"));
  if (usage !== null) return usage;
  const message = recordValue(record, "message");
  return isRecord(message)
    ? usageRecord(recordValue(message, "usage"), recordString(message, "model"))
    : null;
}

function usageRecord(value: unknown, model?: string | null): Usage | null {
  if (!isRecord(value)) return null;
  return {
    inputTokens: optionalUsageInteger(value, "input_tokens"),
    outputTokens: optionalUsageInteger(value, "output_tokens"),
    cacheReadTokens: optionalUsageInteger(value, "cache_read_input_tokens"),
    cacheCreationTokens: optionalUsageInteger(value, "cache_creation_input_tokens"),
    ...(model === undefined || model === null ? {} : { model }),
  };
}

function optionalUsageInteger(record: Readonly<Record<string, unknown>>, key: string): number {
  const value = recordValue(record, key);
  if (value === undefined) return 0;
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) {
    return value;
  }
  throw new RunnerFailure(`${key} must be a non-negative safe integer`, "provider_returned_error");
}

function decodeChunk(decoder: StringDecoder, chunk: unknown): string {
  if (Buffer.isBuffer(chunk)) return decoder.write(chunk);
  if (typeof chunk === "string") return chunk;
  return String(chunk);
}

function recordString(record: Readonly<Record<string, unknown>>, key: string): string | null {
  const value = recordValue(record, key);
  return typeof value === "string" ? value : null;
}

function recordValue(record: Readonly<Record<string, unknown>>, key: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
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
