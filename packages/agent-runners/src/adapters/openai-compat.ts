import { randomBytes } from "node:crypto";

import {
  type AgentId,
  asAgentSlug,
  asReceiptId,
  asRunnerId,
  asTaskId,
  type CostLedgerEntry,
  CredentialHandle,
  type ReceiptId,
  type ReceiptSnapshot,
  type RunnerEvent,
  type RunnerFailureCode,
  type RunnerId,
  type RunnerSpawnOptions,
  type RunnerSpawnRequest,
  SanitizedString,
  sha256Hex,
  type TaskId,
} from "@wuphf/protocol";

import { ReceiptWriteFailed, RunnerOptionsRequired, RunnerSpawnFailed } from "../errors.ts";
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

export type OpenAICompatRunnerOptions = Extract<
  RunnerSpawnOptions,
  { readonly kind: "openai-compat" }
>;

export type OpenAICompatFetch = (
  input: string | URL | Request,
  init?: RequestInit,
) => Promise<Response>;

export interface OpenAICompatAdapterOptions {
  readonly fetchFn?: OpenAICompatFetch | undefined;
  readonly now?: (() => Date) | undefined;
  readonly runnerIdFactory?: (() => RunnerId) | undefined;
  readonly receiptIdFactory?: (() => ReceiptId) | undefined;
  readonly taskIdFactory?: (() => TaskId) | undefined;
  readonly maxEventHistory?: number | undefined;
}

interface OpenAICompatUsage {
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cacheReadTokens: number;
  readonly cacheCreationTokens: number;
  readonly model?: string | undefined;
}

interface AuthSelection {
  readonly headers: Headers;
  readonly warning?: string | undefined;
}

interface FailureCause {
  readonly name?: string | undefined;
  readonly message: string;
}

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
export const OPENAI_COMPAT_DEFAULT_TIMEOUT_MS = 60_000;
const DEFAULT_MODEL = "openai-compat";
const SAFE_HTTP_ERROR_BODY_BYTES = 2_048;
const SAFE_FAILURE_MESSAGE_BYTES = 7_500;

export function createOpenAICompatRunner(
  options: OpenAICompatAdapterOptions = {},
): SpawnAgentRunner {
  const fetchFn = options.fetchFn ?? ((input, init) => globalThis.fetch(input, init));
  return async (request, deps) => {
    if (request.kind !== "openai-compat") {
      throw new RunnerSpawnFailed(`OpenAI-compatible adapter cannot run ${request.kind}`);
    }
    const runnerOptions = parseRunnerOptions(request);
    const secret = await deps.secretReader(deps.credential);
    const runner = new OpenAICompatAgentRunner({
      deps,
      fetchFn,
      maxEventHistory: options.maxEventHistory ?? DEFAULT_MAX_EVENT_HISTORY,
      now: options.now ?? (() => new Date()),
      receiptIdFactory: options.receiptIdFactory ?? randomReceiptId,
      request,
      runnerId: options.runnerIdFactory?.() ?? randomRunnerId(),
      runnerOptions,
      secret,
      taskIdFactory: options.taskIdFactory ?? randomTaskId,
    });
    runner.start();
    return runner;
  };
}

class OpenAICompatAgentRunner implements AgentRunner {
  readonly id: RunnerId;
  readonly kind = "openai-compat" as const;
  readonly agentId: AgentId;

  readonly #abortController = new AbortController();
  readonly #deps: RunnerSpawnDeps;
  readonly #emitter: SerializedEmitter;
  readonly #fetchFn: OpenAICompatFetch;
  readonly #hub: RunnerEventHub;
  readonly #lifecycle: LifecycleStateMachine;
  readonly #now: () => Date;
  readonly #receiptId: ReceiptId;
  readonly #redactor: StreamingRedactor;
  readonly #request: RunnerSpawnRequest;
  readonly #runnerOptions: OpenAICompatRunnerOptions;
  readonly #secret: string;
  readonly #taskId: TaskId;
  #costEmitted = false;
  #done: Promise<void> | null = null;
  #failed = false;
  #finalText = "";
  #lastUsage: OpenAICompatUsage | null = null;
  #redactionTarget: "stdout" | "stderr" | null = null;
  #runSettled: Promise<void> | null = null;
  #startedAt: Date;
  #terminatePromise: Promise<void> | null = null;
  #terminateRequested = false;

  constructor(args: {
    readonly deps: RunnerSpawnDeps;
    readonly fetchFn: OpenAICompatFetch;
    readonly maxEventHistory: number;
    readonly now: () => Date;
    readonly receiptIdFactory: () => ReceiptId;
    readonly request: RunnerSpawnRequest;
    readonly runnerId: RunnerId;
    readonly runnerOptions: OpenAICompatRunnerOptions;
    readonly secret: string;
    readonly taskIdFactory: () => TaskId;
  }) {
    this.id = args.runnerId;
    this.agentId = args.request.agentId;
    this.#deps = args.deps;
    this.#fetchFn = args.fetchFn;
    this.#hub = new RunnerEventHub(args.maxEventHistory);
    this.#emitter = new SerializedEmitter({ eventLog: args.deps.eventLog, eventHub: this.#hub });
    this.#lifecycle = new LifecycleStateMachine(args.runnerId);
    this.#now = args.now;
    this.#receiptId = args.receiptIdFactory();
    this.#redactor = createSecretStreamingRedactor(args.secret);
    this.#request = args.request;
    this.#runnerOptions = args.runnerOptions;
    this.#secret = args.secret;
    this.#startedAt = args.now();
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
    const runSettled = this.#run();
    this.#runSettled = runSettled;
    this.#done = runSettled.finally(async () => {
      await this.#lifecycle.stopped().catch(() => undefined);
      await this.#emitter.close();
      this.#hub.close();
    });
  }

  async terminate(): Promise<void> {
    if (this.#terminatePromise === null) {
      this.#terminatePromise = Promise.resolve().then(async () => {
        const failure = new RunnerFailure("runner terminated by request", "terminated_by_request");
        await this.#cleanupWithFailure(failure, "terminated_by_request", 1, true);
        await (this.#done ?? this.#lifecycle.stopped());
      });
    }
    return this.#terminatePromise;
  }

  async #run(): Promise<void> {
    const timeoutSignal = AbortSignal.timeout(
      this.#runnerOptions.timeoutMs ?? OPENAI_COMPAT_DEFAULT_TIMEOUT_MS,
    );
    const signal = AbortSignal.any([this.#abortController.signal, timeoutSignal]);
    try {
      this.#lifecycle.markRunning();
      this.#startedAt = this.#now();
      const auth = this.#authSelection();
      // TODO(#NEW): put retries in ledger/idempotency-aware middleware above adapters.
      const response = await withAbort(
        this.#fetchFn(this.#runnerOptions.endpoint, {
          body: JSON.stringify(this.#requestBody()),
          headers: auth.headers,
          method: "POST",
          signal,
        }),
        signal,
      );
      if (!response.ok) {
        const body = await safeResponseText(
          response,
          signal,
          DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES,
        );
        const message = failureMessage("openai_compat_http_error", {
          body,
          exitCode: 1,
          status: response.status,
          statusText: response.statusText,
        });
        throw new RunnerFailure(message, "provider_returned_error");
      }
      await this.#emit({ kind: "started", runnerId: this.id, at: this.#isoNow() });
      if (auth.warning !== undefined) {
        await this.#emitText("stderr", auth.warning);
      }
      const sawDone = await this.#consumeSse(response.body, signal);
      if (!sawDone) {
        const message = failureMessage("openai_compat_stream_ended_without_done", {
          exitCode: 1,
        });
        throw new RunnerFailure(message, "unrecognized_provider_response");
      }
      if (!this.#costEmitted) {
        await this.#emitCost(zeroUsage(), "provider_did_not_report_usage");
      }
      await this.#writeReceiptAndFinish(0);
    } catch (error) {
      const failure = this.#failureForCaughtError(error, timeoutSignal);
      await this.#cleanupWithFailure(failure, "network_failed", 1);
    }
  }

  #requestBody(): Readonly<Record<string, unknown>> {
    return {
      messages: [{ role: "user", content: this.#request.prompt }],
      model: this.#request.model ?? DEFAULT_MODEL,
      stream: true,
      stream_options: { include_usage: true },
    };
  }

  #authSelection(): AuthSelection {
    const scope = String(CredentialHandle.scope(this.#deps.credential));
    const headers = new Headers();
    headers.set("Accept", "text/event-stream");
    headers.set("Content-Type", "application/json");
    for (const [key, value] of Object.entries(this.#runnerOptions.headers ?? {})) {
      headers.set(key, value);
    }
    if (scope === "anthropic") {
      headers.set("x-api-key", this.#secret);
      headers.delete("Authorization");
      return { headers };
    }
    headers.set("Authorization", `Bearer ${this.#secret}`);
    headers.delete("x-api-key");
    if (scope === "openai" || scope === "openai-compat") {
      return { headers };
    }
    return {
      headers,
      warning: JSON.stringify({
        assumedAuthShape: "authorization_bearer",
        code: "openai_compat_unknown_credential_scope",
        scope,
      }),
    };
  }

  async #consumeSse(
    body: ReadableStream<Uint8Array> | null,
    signal: AbortSignal,
  ): Promise<boolean> {
    if (body === null) {
      throw new Error("openai_compat_response_body_missing");
    }
    const reader = body.getReader();
    const decoder = new TextDecoder();
    const buffer = new BoundedLineBuffer(DEFAULT_MAX_RUNNER_INPUT_BUFFER_BYTES);
    try {
      while (true) {
        const next = await withAbort(reader.read(), signal);
        if (next.done) break;
        if (await this.#handleSseLines(buffer.push(decoder.decode(next.value, { stream: true })))) {
          await reader.cancel();
          return true;
        }
      }
      if (await this.#handleSseLines(buffer.push(decoder.decode()))) {
        return true;
      }
      return await this.#handleSseLines(buffer.flush());
    } catch (error) {
      if (error instanceof RunnerInputBufferOverflow) {
        throw new RunnerFailure(error.message, "runner_input_buffer_overflow", { cause: error });
      }
      throw error;
    } finally {
      reader.releaseLock();
    }
  }

  async #handleSseLines(lines: readonly string[]): Promise<boolean> {
    for (const rawLine of lines) {
      const line = stripTrailingCarriageReturn(rawLine);
      if (await this.#handleSseLine(line)) {
        return true;
      }
    }
    return false;
  }

  async #handleSseLine(line: string): Promise<boolean> {
    if (line.length === 0 || line.startsWith(":")) return false;
    if (!line.startsWith("data:")) return false;
    const payload = line.slice("data:".length).trimStart();
    if (payload === "[DONE]") return true;
    const parsed = parseJsonRecord(payload, "openai_compat_sse_json_invalid");
    const content = extractDeltaContent(parsed);
    if (content.length > 0) {
      await this.#emitStdout(content);
    }
    const usage = extractUsage(parsed);
    if (usage !== null && !this.#costEmitted) {
      await this.#emitCost(usage);
    }
    return false;
  }

  async #emitCost(
    usage: OpenAICompatUsage,
    note?: "provider_did_not_report_usage" | undefined,
  ): Promise<void> {
    this.#lastUsage = usage;
    this.#costEmitted = true;
    const ledgerEntry = this.#costEntry(usage);
    await this.#recordCost(ledgerEntry);
    const eventEntry =
      note === undefined
        ? ledgerEntry
        : ({ ...ledgerEntry, note } satisfies CostLedgerEntry & { readonly note: string });
    await this.#emit({
      kind: "cost",
      runnerId: this.id,
      entry: eventEntry,
      at: this.#isoNow(),
    });
  }

  #costEntry(usage: OpenAICompatUsage): CostLedgerEntry {
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    return validatedCostEntry({
      request: this.#request,
      providerKind: this.#deps.resolvedProviderKind,
      defaultModel: DEFAULT_MODEL,
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
    const usage = this.#lastUsage ?? zeroUsage();
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    const finishedAt = this.#now();
    return {
      id: this.#receiptId,
      agentSlug: asAgentSlug(this.agentId),
      taskId: this.#taskId,
      triggerKind: "human_message",
      triggerRef: this.id,
      startedAt: this.#startedAt,
      finishedAt,
      status: "ok",
      providerKind: this.#deps.resolvedProviderKind,
      model: trustedCostModel({ request: this.#request, defaultModel: DEFAULT_MODEL }),
      promptHash: sha256Hex(this.#request.prompt),
      toolManifest: sha256Hex("openai-compat-http:v1"),
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
    const usage = this.#lastUsage ?? zeroUsage();
    const amount =
      usage.inputTokens + usage.outputTokens + usage.cacheReadTokens + usage.cacheCreationTokens;
    const finishedAt = this.#now();
    return {
      id: this.#receiptId,
      agentSlug: asAgentSlug(this.agentId),
      taskId: this.#taskId,
      triggerKind: "human_message",
      triggerRef: this.id,
      startedAt: this.#startedAt,
      finishedAt,
      status: "error",
      providerKind: this.#deps.resolvedProviderKind,
      model: trustedCostModel({ request: this.#request, defaultModel: DEFAULT_MODEL }),
      promptHash: sha256Hex(this.#request.prompt),
      toolManifest: sha256Hex("openai-compat-http:v1"),
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
    waitForDone = false,
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
      error: truncateUtf8(message, SAFE_FAILURE_MESSAGE_BYTES),
      code: failure.code,
      at: this.#isoNow(),
    };
    await terminalCleanup({
      lifecycle: this.#lifecycle,
      target: this.#cleanupTarget(failure.code === "terminated_by_request", waitForDone),
      emitter: this.#emitter,
      receiptStore: this.#deps.receiptStore,
      failureReceipt: this.#buildFailureReceipt(event.error),
      failureCode: failure.code,
      failureEvent: event,
      stopped: { exitCode, error: event.error },
    });
  }

  #failureForCaughtError(error: unknown, timeoutSignal: AbortSignal): RunnerFailure {
    if (error instanceof RunnerFailure) return error;
    const cause = failureCause(error);
    if (this.#terminateRequested) {
      return new RunnerFailure(
        failureMessage("openai_compat_aborted", { cause, exitCode: 1 }),
        "terminated_by_request",
        { cause: error },
      );
    }
    if (timeoutSignal.aborted) {
      return new RunnerFailure(
        failureMessage("openai_compat_timeout", {
          cause,
          exitCode: 1,
          timeoutMs: this.#runnerOptions.timeoutMs ?? OPENAI_COMPAT_DEFAULT_TIMEOUT_MS,
        }),
        "subprocess_timed_out",
        { cause: error },
      );
    }
    return new RunnerFailure(
      failureMessage("openai_compat_network_error", { cause, exitCode: 1 }),
      "network_failed",
      { cause: error },
    );
  }

  #isoNow(): string {
    return this.#now().toISOString();
  }

  #cleanupTarget(markTerminated: boolean, waitForDone: boolean): TerminalCleanupTarget {
    return {
      kind: "abort",
      abort: {
        abort: () => {
          if (markTerminated) {
            this.#terminateRequested = true;
          }
          if (!this.#abortController.signal.aborted) {
            this.#abortController.abort(new DOMException("terminated", "AbortError"));
          }
        },
        wait: async () => {
          if (waitForDone) {
            await (this.#runSettled ?? this.#lifecycle.stopped()).catch(() => undefined);
          }
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

function parseRunnerOptions(request: RunnerSpawnRequest): OpenAICompatRunnerOptions {
  const options = request.options;
  if (options?.kind !== "openai-compat") {
    throw new RunnerOptionsRequired("OpenAI-compatible runner requires options.kind=openai-compat");
  }
  return options;
}

async function safeResponseText(
  response: Response,
  signal: AbortSignal,
  maxBytes: number,
): Promise<string> {
  const body = response.body;
  if (body === null) return "";
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let totalBytes = 0;
  let text = "";
  try {
    while (true) {
      const next = await withAbort(reader.read(), signal);
      if (next.done) break;
      totalBytes += next.value.byteLength;
      if (totalBytes > maxBytes) {
        await reader.cancel().catch(() => undefined);
        throw new RunnerFailure(
          `OpenAI-compatible HTTP error body exceeded ${maxBytes} bytes`,
          "runner_input_buffer_overflow",
        );
      }
      if (Buffer.byteLength(text, "utf8") <= SAFE_HTTP_ERROR_BODY_BYTES) {
        text += decoder.decode(next.value, { stream: true });
      }
    }
    text += decoder.decode();
    return truncateUtf8(text, SAFE_HTTP_ERROR_BODY_BYTES);
  } catch (error) {
    if (error instanceof RunnerFailure) throw error;
    if (isAbortLikeError(error)) throw error;
    return failureMessage("openai_compat_error_body_unavailable", {
      cause: failureCause(error),
    });
  } finally {
    reader.releaseLock();
  }
}

function isAbortLikeError(error: unknown): boolean {
  if (error instanceof DOMException) return error.name === "AbortError";
  if (error instanceof Error && error.name === "AbortError") return true;
  return false;
}

function extractDeltaContent(record: Readonly<Record<string, unknown>>): string {
  const choices = recordValue(record, "choices");
  if (!Array.isArray(choices)) return "";
  const first = choices[0];
  if (!isRecord(first)) return "";
  const delta = recordValue(first, "delta");
  if (!isRecord(delta)) return "";
  const content = recordValue(delta, "content");
  return typeof content === "string" ? content : "";
}

function extractUsage(record: Readonly<Record<string, unknown>>): OpenAICompatUsage | null {
  const usage = recordValue(record, "usage");
  if (!isRecord(usage)) return null;
  const model = recordString(record, "model");
  const promptTokens = nonNegativeInteger(usage, "prompt_tokens", "usage.prompt_tokens") ?? 0;
  const completionTokens =
    nonNegativeInteger(usage, "completion_tokens", "usage.completion_tokens") ?? 0;
  const promptDetails = recordValue(usage, "prompt_tokens_details");
  const cachedTokensRaw = isRecord(promptDetails)
    ? (nonNegativeInteger(
        promptDetails,
        "cached_tokens",
        "usage.prompt_tokens_details.cached_tokens",
      ) ?? 0)
    : 0;
  const inputTokens = nonNegativeInteger(usage, "input_tokens", "usage.input_tokens");
  const outputTokens = nonNegativeInteger(usage, "output_tokens", "usage.output_tokens");
  const cacheReadTokens = nonNegativeInteger(
    usage,
    "cache_read_input_tokens",
    "usage.cache_read_input_tokens",
  );
  const cacheCreationTokens = nonNegativeInteger(
    usage,
    "cache_creation_input_tokens",
    "usage.cache_creation_input_tokens",
  );
  if (inputTokens !== undefined || outputTokens !== undefined) {
    return {
      inputTokens: inputTokens ?? 0,
      outputTokens: outputTokens ?? 0,
      cacheReadTokens: cacheReadTokens ?? 0,
      cacheCreationTokens: cacheCreationTokens ?? 0,
      ...(model === null ? {} : { model }),
    };
  }
  const cachedTokens = Math.min(Math.max(0, cachedTokensRaw), promptTokens);
  return {
    inputTokens: Math.max(0, promptTokens - cachedTokens),
    outputTokens: completionTokens,
    cacheReadTokens: cachedTokens,
    cacheCreationTokens: 0,
    ...(model === null ? {} : { model }),
  };
}

function zeroUsage(): OpenAICompatUsage {
  return {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  };
}

function nonNegativeInteger(
  record: Readonly<Record<string, unknown>>,
  key: string,
  path: string,
): number | undefined {
  const value = recordValue(record, key);
  if (value === undefined) return undefined;
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) {
    return value;
  }
  throw new RunnerFailure(
    `${path}: must be a non-negative safe integer`,
    "provider_returned_error",
  );
}

function parseJsonRecord(payload: string, code: string): Readonly<Record<string, unknown>> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(payload);
  } catch (error) {
    throw new RunnerFailure(
      failureMessage(code, { cause: failureCause(error) }),
      "unrecognized_provider_response",
      { cause: error },
    );
  }
  if (!isRecord(parsed)) {
    throw new RunnerFailure(
      failureMessage(code, { cause: { message: "data payload was not an object" } }),
      "unrecognized_provider_response",
    );
  }
  return parsed;
}

async function withAbort<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) throw abortError(signal);
  return await new Promise<T>((resolve, reject) => {
    const onAbort = (): void => {
      reject(abortError(signal));
    };
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(
      (value) => {
        signal.removeEventListener("abort", onAbort);
        resolve(value);
      },
      (reason: unknown) => {
        signal.removeEventListener("abort", onAbort);
        reject(reason);
      },
    );
  });
}

function abortError(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException("aborted", "AbortError");
}

function failureMessage(code: string, fields: Readonly<Record<string, unknown>>): string {
  return `${code}: ${JSON.stringify(fields)}`;
}

function failureCause(error: unknown): FailureCause {
  if (error instanceof Error) {
    return {
      name: error.name,
      message: error.message,
    };
  }
  return { message: String(error) };
}

function truncateUtf8(value: string, maxBytes: number): string {
  const encoded = new TextEncoder().encode(value);
  if (encoded.byteLength <= maxBytes) return value;
  const truncated = new TextDecoder().decode(encoded.slice(0, maxBytes));
  return `${truncated}[truncated]`;
}

function stripTrailingCarriageReturn(value: string): string {
  return value.endsWith("\r") ? value.slice(0, -1) : value;
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
