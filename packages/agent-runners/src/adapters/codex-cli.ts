import { type ChildProcessWithoutNullStreams, type SpawnOptions, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { realpathSync, statSync, unlinkSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import type { Readable } from "node:stream";
import { StringDecoder } from "node:string_decoder";

import {
  type AgentId,
  asAgentSlug,
  asMicroUsd,
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
  type RunnerFailureCode,
  type RunnerId,
  type RunnerSpawnRequest,
  SanitizedString,
  sha256Hex,
  type TaskId,
} from "@wuphf/protocol";

import { CodexCliNotAvailable, ReceiptWriteFailed, RunnerSpawnFailed } from "../errors.ts";
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
const MAX_CODEX_BLOCK_LINES = 1024;

export function createCodexCliRunner(options: CodexCliAdapterOptions = {}): SpawnAgentRunner {
  const commandPath = resolveCodexCommand(options);
  const spawner = options.spawner ?? nodeSpawner;
  return async (request, deps) => {
    if (request.kind !== "codex-cli") {
      throw new RunnerSpawnFailed(`Codex CLI adapter cannot run ${request.kind}`);
    }
    const scope = CredentialHandle.scope(deps.credential);
    const secretEnvVar = secretEnvVarForScope(scope);
    const secret = await deps.secretReader(deps.credential);
    const runnerId = options.runnerIdFactory?.() ?? randomRunnerId();
    const requestOptions = request.options?.kind === "codex-cli" ? request.options : undefined;
    const runner = new CodexCliAgentRunner({
      commandPath,
      costEstimator: options.costEstimator,
      deps,
      maxEventHistory: options.maxEventHistory ?? DEFAULT_MAX_EVENT_HISTORY,
      now: options.now ?? (() => new Date()),
      outputLastMessagePath: outputLastMessagePath(options.outputLastMessagePath, runnerId),
      profile: requestOptions?.profile ?? options.profile ?? DEFAULT_PROFILE,
      receiptIdFactory: options.receiptIdFactory ?? randomReceiptId,
      request,
      runnerId,
      sandbox: requestOptions?.sandbox ?? options.sandbox ?? DEFAULT_SANDBOX,
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
  readonly #emitter: SerializedEmitter;
  readonly #hub: RunnerEventHub;
  readonly #lifecycle: LifecycleStateMachine;
  readonly #now: () => Date;
  readonly #outputLastMessagePath: string;
  readonly #profile: string;
  readonly #receiptId: ReceiptId;
  readonly #redactor: StreamingRedactor;
  readonly #request: RunnerSpawnRequest;
  readonly #sandbox: CodexCliSandboxMode;
  readonly #secret: string;
  readonly #secretEnvVar: "ANTHROPIC_API_KEY" | "OPENAI_API_KEY";
  readonly #spawner: CodexCliSpawner;
  readonly #taskId: TaskId;
  #child: CodexCliChildProcess | null = null;
  #childExited = false;
  #done: Promise<void> | null = null;
  #exitPromise: Promise<ExitResult> | null = null;
  #failed = false;
  #finalText = "";
  #lastCostMicroUsd: MicroUsd = asMicroUsd(0);
  #lastUsage: CostUnits = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
  #redactionTarget: "stdout" | "stderr" | null = null;
  #redactionFinalMessage = false;
  #startedAt: Date | null = null;
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
    this.#emitter = new SerializedEmitter({ eventLog: args.deps.eventLog, eventHub: this.#hub });
    this.#lifecycle = new LifecycleStateMachine(args.runnerId);
    this.#now = args.now;
    this.#outputLastMessagePath = args.outputLastMessagePath;
    this.#profile = args.profile;
    this.#receiptId = args.receiptIdFactory();
    this.#redactor = createSecretStreamingRedactor(args.secret);
    this.#request = args.request;
    this.#sandbox = args.sandbox;
    this.#secret = args.secret;
    this.#secretEnvVar = args.secretEnvVar;
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
      this.#removeLastMessageArtifact();
      await this.#emitter.close();
      this.#hub.close();
    });
  }

  #removeLastMessageArtifact(): void {
    try {
      unlinkSync(this.#outputLastMessagePath);
    } catch {
      // The artifact may not exist (early failure) or may already be unlinked
      // by a previous attempt. Either way, cleanup is best-effort.
    }
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
      const env = sanitizedCodexEnv(this.#commandPath, this.#secretEnvVar, this.#secret);
      this.#child = this.#spawner(this.#commandPath, this.#codexArgs(), {
        env,
        cwd: this.#request.cwd,
      });
      this.#childExited = false;
      this.#exitPromise = waitForExit(this.#child).then((result) => {
        this.#childExited = true;
        return result;
      });
      await this.#emit({ kind: "started", runnerId: this.id, at: this.#isoNow() });
      const stdout = this.#consumeStdout(this.#child.stdout);
      const stderr = this.#consumeStderr(this.#child.stderr);
      [exit] = await Promise.all([this.#exitPromise, stdout, stderr]);
      await this.#emitUnrecognizedSummary();
      if (exit.error !== undefined) {
        throw new RunnerFailure(exit.error.message, "spawn_failed", { cause: exit.error });
      }
      if (exit.code !== 0) {
        const signalText = exit.signal === null ? "" : ` (${exit.signal})`;
        const message = `Codex CLI exited with code ${exit.code}${signalText}`;
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
    const blockLines: string[] = [];
    let sawDelimiter = false;
    const buffer = new BoundedLineBuffer(DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES);
    try {
      for await (const chunk of stream) {
        for (const line of buffer.push(decodeChunk(decoder, chunk))) {
          if (isDelimiterLine(line)) {
            await this.#processCodexBlock(blockLines.splice(0), false);
            sawDelimiter = true;
          } else {
            pushCodexBlockLine(blockLines, `${line}\n`);
          }
        }
      }
      const tail = decoder.end();
      if (tail.length > 0) {
        for (const line of buffer.push(tail)) {
          if (isDelimiterLine(line)) {
            await this.#processCodexBlock(blockLines.splice(0), false);
            sawDelimiter = true;
          } else {
            pushCodexBlockLine(blockLines, `${line}\n`);
          }
        }
      }
      for (const line of buffer.flush()) {
        if (isDelimiterLine(line)) {
          await this.#processCodexBlock(blockLines.splice(0), false);
          sawDelimiter = true;
        } else {
          pushCodexBlockLine(blockLines, line);
        }
      }
    } catch (error) {
      if (error instanceof RunnerInputBufferOverflow) {
        throw new RunnerFailure(error.message, "runner_input_buffer_overflow", { cause: error });
      }
      throw error;
    }
    await this.#processCodexBlock(blockLines, sawDelimiter);
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
        await this.#emitText("stderr", `codex command: ${exec.command} (cwd: ${exec.cwd})\n`);
        continue;
      }
      if (isToolExitLine(line)) continue;
      const totalTokens = parseTokensUsedLine(line);
      if (totalTokens.kind === "valid") {
        await this.#recordTokenUsage(totalTokens.value);
        continue;
      }
      if (totalTokens.kind === "invalid") {
        throw new RunnerFailure("Codex CLI emitted invalid token usage", "provider_returned_error");
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
    const amountMicroUsd = this.#estimateCostMicroUsd(model, totalTokens, units);
    const entry = validatedCostEntry({
      request: this.#request,
      providerKind: this.#deps.resolvedProviderKind,
      defaultModel: "codex-cli",
      amountMicroUsd,
      units,
      occurredAt: this.#now(),
      receiptId: this.#receiptId,
      taskId: this.#taskId,
    });
    this.#lastCostMicroUsd = entry.amountMicroUsd;
    await this.#recordCost(entry);
    await this.#emit({ kind: "cost", runnerId: this.id, entry, at: this.#isoNow() });
  }

  #estimateCostMicroUsd(model: string, totalTokens: number, units: CostUnits): number {
    try {
      const estimate = this.#costEstimator?.({
        model,
        providerKind: this.#deps.resolvedProviderKind,
        totalTokens,
        units,
      });
      return estimate ?? 0;
    } catch {
      return 0;
    }
  }

  async #emitStdoutText(text: string, finalMessage: boolean): Promise<void> {
    if (text.length === 0) return;
    const redacted = await this.#redactForTarget("stdout", text, finalMessage);
    for (const chunk of chunkStdio(redacted)) {
      await this.#emit({ kind: "stdout", runnerId: this.id, chunk, at: this.#isoNow() });
      if (finalMessage) {
        this.#finalText += chunk;
      }
    }
  }

  async #emitUnrecognizedSummary(): Promise<void> {
    if (this.#unrecognizedLineCount === 0) return;
    await this.#emitText(
      "stderr",
      `codex output parser saw ${this.#unrecognizedLineCount} unrecognized line(s)\n`,
    );
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
    const startedAt = this.#startedAt ?? this.#now();
    const finishedAt = this.#now();
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
      model: trustedCostModel({ request: this.#request, defaultModel: "codex-cli" }),
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

  #buildFailureReceipt(message: string): ReceiptSnapshot {
    const startedAt = this.#startedAt ?? this.#now();
    const finishedAt = this.#now();
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
      model: trustedCostModel({ request: this.#request, defaultModel: "codex-cli" }),
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
      error: SanitizedString.fromUnknown(message),
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
    args.push("--");
    args.push(this.#request.prompt);
    return args;
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
    this.#redactionFinalMessage = false;
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
    const redacted = await this.#redactForTarget(target, text, false);
    for (const chunk of chunkStdio(redacted)) {
      await this.#emit({ kind: target, runnerId: this.id, chunk, at: this.#isoNow() });
    }
  }

  async #redactForTarget(
    target: "stdout" | "stderr",
    text: string,
    finalMessage: boolean,
  ): Promise<string> {
    if (
      this.#redactionTarget !== null &&
      (this.#redactionTarget !== target || this.#redactionFinalMessage !== finalMessage)
    ) {
      await this.#flushRedactorCarry();
    }
    this.#redactionTarget = target;
    this.#redactionFinalMessage = finalMessage;
    return this.#redactor.redact(text);
  }

  async #flushRedactorCarry(): Promise<void> {
    const target = this.#redactionTarget;
    const finalMessage = this.#redactionFinalMessage;
    const carry = this.#redactor.flush();
    this.#redactionTarget = null;
    this.#redactionFinalMessage = false;
    if (target === null || carry.length === 0) return;
    for (const chunk of chunkStdio(carry)) {
      await this.#emit({ kind: target, runnerId: this.id, chunk, at: this.#isoNow() });
      if (target === "stdout" && finalMessage) {
        this.#finalText += chunk;
      }
    }
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
  const commandNames =
    process.platform === "win32" ? ["codex.exe", "codex.cmd", "codex"] : ["codex"];
  const { PATH: pathEnv } = process.env;
  for (const segment of (pathEnv ?? "").split(path.delimiter)) {
    if (path.isAbsolute(segment)) {
      for (const commandName of commandNames) {
        candidates.push(path.join(segment, commandName));
      }
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

function parseExecLine(line: string): { readonly command: string; readonly cwd: string } | null {
  const match = /^exec\s+(.+)\s+in\s+(.+)$/.exec(line.trim());
  if (match === null) return null;
  const command = match[1];
  const cwd = match[2];
  if (command === undefined || cwd === undefined) return null;
  return { command, cwd };
}

type TokenUsageParseResult =
  | { readonly kind: "none" }
  | { readonly kind: "valid"; readonly value: number }
  | { readonly kind: "invalid" };

function parseTokensUsedLine(line: string): TokenUsageParseResult {
  const trimmed = line.trim();
  if (!/^tokens used:/i.test(trimmed)) return { kind: "none" };
  const match = /^tokens used:\s*([0-9][0-9_,]*)\b/i.exec(trimmed);
  const raw = match?.[1];
  if (raw === undefined) return { kind: "invalid" };
  const parsed = Number.parseInt(raw.replaceAll(/[_,]/g, ""), 10);
  return Number.isSafeInteger(parsed) && parsed >= 0
    ? { kind: "valid", value: parsed }
    : { kind: "invalid" };
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

function pushCodexBlockLine(lines: string[], line: string): void {
  lines.push(line);
  if (lines.length > MAX_CODEX_BLOCK_LINES) {
    throw new RunnerFailure(
      `Codex CLI output block exceeded ${MAX_CODEX_BLOCK_LINES} lines`,
      "runner_input_buffer_overflow",
    );
  }
}

function stripLineEnding(line: string): string {
  return line.replace(/\r?\n$/, "");
}

function decodeChunk(decoder: StringDecoder, chunk: unknown): string {
  if (Buffer.isBuffer(chunk)) return decoder.write(chunk);
  if (typeof chunk === "string") return chunk;
  return String(chunk);
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
