import type { ReadableStream as RunnerEventStream } from "node:stream/web";

import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asRunnerId,
  asTaskId,
  type CostLedgerEntry,
  type CredentialHandle,
  type CredentialScope,
  credentialHandleFromJson,
  type ProviderKind,
  type RunnerEvent,
  type RunnerSpawnRequest,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createBrokerIdentityForTesting } from "../../../protocol/src/credential-handle.ts";
import {
  createOpenAICompatRunner,
  OPENAI_COMPAT_DEFAULT_TIMEOUT_MS,
  type OpenAICompatFetch,
  type OpenAICompatRunnerOptions,
} from "../../src/adapters/openai-compat.ts";
import type { Receipt, RunnerSpawnDeps } from "../../src/runner.ts";

const agentId = asAgentId("agent_alpha");
const credentialId = asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN");
const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");
const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const taskId = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const fixedDate = new Date("2026-05-08T18:00:00.000Z");
const endpoint = "https://provider.test/v1/chat/completions";
const encoder = new TextEncoder();

interface FetchCall {
  readonly input: string | URL | Request;
  readonly init?: RequestInit | undefined;
}

interface Harness {
  readonly credential: CredentialHandle;
  readonly costs: CostLedgerEntry[];
  readonly deps: RunnerSpawnDeps;
  readonly events: RunnerEvent[];
  readonly receipts: Receipt[];
  readonly secretReads: CredentialHandle[];
}

class ControlledSseStream {
  readonly body: ReadableStream<Uint8Array>;
  #controller: ReadableStreamDefaultController<Uint8Array> | null = null;

  constructor() {
    this.body = new ReadableStream<Uint8Array>({
      start: (controller) => {
        this.#controller = controller;
      },
    });
  }

  enqueue(text: string): void {
    const controller = this.#controller;
    if (controller === null) throw new Error("stream controller was not ready");
    controller.enqueue(encoder.encode(text));
  }

  close(): void {
    const controller = this.#controller;
    if (controller === null) throw new Error("stream controller was not ready");
    controller.close();
  }

  error(error: Error): void {
    const controller = this.#controller;
    if (controller === null) throw new Error("stream controller was not ready");
    controller.error(error);
  }
}

function makeHarness(
  args: {
    readonly receiptPut?: ((receipt: Receipt) => Promise<{ readonly stored: boolean }>) | undefined;
    readonly resolvedProviderKind?: ProviderKind | undefined;
    readonly scope?: CredentialScope | undefined;
    readonly secret?: string | undefined;
  } = {},
): Harness {
  const scope = args.scope ?? asCredentialScope("openai");
  const credential = credentialForScope(scope);
  const costs: CostLedgerEntry[] = [];
  const events: RunnerEvent[] = [];
  const receipts: Receipt[] = [];
  const secretReads: CredentialHandle[] = [];
  let lsn = 0;
  const receiptPut =
    args.receiptPut ??
    (async (receipt: Receipt) => {
      receipts.push(receipt);
      return { stored: true };
    });
  return {
    credential,
    costs,
    events,
    receipts,
    secretReads,
    deps: {
      credential,
      resolvedProviderKind:
        args.resolvedProviderKind ??
        asProviderKind(scope === "github" ? "openai-compat" : String(scope)),
      secretReader: async (handle) => {
        secretReads.push(handle);
        return args.secret ?? "sk-test-secret";
      },
      costLedger: {
        record: async (entry) => {
          costs.push(entry);
        },
      },
      receiptStore: { put: receiptPut },
      eventLog: {
        append: async (event) => {
          events.push(event);
          lsn += 1;
          return lsn;
        },
      },
    },
  };
}

function credentialForScope(scope: CredentialScope): CredentialHandle {
  return credentialHandleFromJson(
    { version: 1, id: credentialId },
    {
      agentId,
      broker: createBrokerIdentityForTesting({ agentId }),
      scope,
    },
  );
}

function spawnRequest(
  options: OpenAICompatRunnerOptions = { kind: "openai-compat", endpoint },
): RunnerSpawnRequest {
  return {
    kind: "openai-compat",
    agentId,
    credential: { version: 1, id: credentialId },
    prompt: "Say hello",
    model: "gpt-5-mini",
    taskId,
    options,
  };
}

function redactionFixture(): string {
  return ["redact safe", "fixture value", "token!"].join(" ");
}

function createSpawner(fetchFn: OpenAICompatFetch) {
  return createOpenAICompatRunner({
    fetchFn,
    now: () => fixedDate,
    receiptIdFactory: () => receiptId,
    runnerIdFactory: () => runnerId,
    taskIdFactory: () => taskId,
  });
}

function sseData(payload: unknown): string {
  return `data: ${JSON.stringify(payload)}\n\n`;
}

function doneData(): string {
  return "data: [DONE]\n\n";
}

async function collectAll(stream: RunnerEventStream<RunnerEvent>): Promise<RunnerEvent[]> {
  const reader = stream.getReader();
  const events: RunnerEvent[] = [];
  while (true) {
    const next = await reader.read();
    if (next.done) return events;
    events.push(next.value);
  }
}

async function waitForEvent(
  events: readonly RunnerEvent[],
  predicate: (event: RunnerEvent) => boolean,
): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (events.some(predicate)) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  throw new Error("timed out waiting for event");
}

function firstCall(calls: readonly FetchCall[]): FetchCall {
  const call = calls[0];
  if (call === undefined) throw new Error("missing fetch call");
  return call;
}

function headersFrom(call: FetchCall): Headers {
  return new Headers(call.init?.headers);
}

function bodyRecord(call: FetchCall): Readonly<Record<string, unknown>> {
  if (typeof call.init?.body !== "string") throw new Error("request body was not a string");
  const parsed: unknown = JSON.parse(call.init.body);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error("request body was not an object");
  }
  return parsed as Readonly<Record<string, unknown>>;
}

function recordValue(record: object, key: string): unknown {
  const descriptor = Object.getOwnPropertyDescriptor(record, key);
  return descriptor !== undefined && "value" in descriptor ? descriptor.value : undefined;
}

function costEvent(events: readonly RunnerEvent[]): Extract<RunnerEvent, { kind: "cost" }> {
  const event = events.find((item): item is Extract<RunnerEvent, { kind: "cost" }> => {
    return item.kind === "cost";
  });
  if (event === undefined) throw new Error("missing cost event");
  return event;
}

describe("createOpenAICompatRunner", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("streams a successful turn, records cost, writes a receipt, and finishes", async () => {
    const harness = makeHarness();
    const stream = new ControlledSseStream();
    const calls: FetchCall[] = [];
    const fetchFn: OpenAICompatFetch = async (input, init) => {
      calls.push({ input, init });
      return new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    };
    const spawnRunner = createSpawner(fetchFn);

    const runner = await spawnRunner(
      spawnRequest({ kind: "openai-compat", endpoint, headers: { "X-Trace-Id": "trace-1" } }),
      harness.deps,
    );
    const eventsPromise = collectAll(runner.events());
    stream.enqueue(sseData({ choices: [{ delta: { content: "hel" } }] }));
    stream.enqueue(sseData({ choices: [{ delta: { content: "lo" } }] }));
    stream.enqueue(
      sseData({
        choices: [{ delta: {}, finish_reason: "stop" }],
        model: "gpt-5-mini-2026-05-08",
        usage: {
          prompt_tokens: 10,
          completion_tokens: 5,
          prompt_tokens_details: { cached_tokens: 2 },
        },
      }),
    );
    stream.enqueue(doneData());
    const events = await eventsPromise;

    expect(harness.secretReads).toEqual([harness.credential]);
    expect(events.map((event) => event.kind)).toEqual([
      "started",
      "stdout",
      "stdout",
      "cost",
      "receipt",
      "finished",
    ]);
    expect(events.filter((event) => event.kind === "stdout").map((event) => event.chunk)).toEqual([
      "hel",
      "lo",
    ]);
    expect(harness.events.map((event) => event.kind)).toEqual(events.map((event) => event.kind));

    const call = firstCall(calls);
    expect(call.input).toBe(endpoint);
    expect(call.init?.method).toBe("POST");
    expect(headersFrom(call).get("Authorization")).toBe("Bearer sk-test-secret");
    expect(headersFrom(call).get("x-api-key")).toBeNull();
    expect(headersFrom(call).get("X-Trace-Id")).toBe("trace-1");
    expect(bodyRecord(call)).toMatchObject({
      model: "gpt-5-mini",
      stream: true,
      stream_options: { include_usage: true },
    });

    expect(harness.costs).toHaveLength(1);
    expect(harness.costs[0]).toMatchObject({
      amountMicroUsd: 15,
      model: "gpt-5-mini",
      providerKind: "openai",
      receiptId,
      taskId,
      units: {
        inputTokens: 8,
        outputTokens: 5,
        cacheReadTokens: 2,
        cacheCreationTokens: 0,
      },
    });
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]).toMatchObject({
      id: receiptId,
      inputTokens: 8,
      outputTokens: 5,
      cacheReadTokens: 2,
      providerKind: "openai",
      taskId,
    });
    const receipt = harness.receipts[0];
    if (receipt === undefined) throw new Error("missing receipt");
    if (receipt.finalMessage === undefined) throw new Error("missing receipt finalMessage");
    expect(receipt.finalMessage.toString()).toBe("hello");
  });

  it("fails the runner when receipt put fails and does not emit finished", async () => {
    const harness = makeHarness({
      receiptPut: async () => {
        throw new Error("disk full");
      },
    });
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(sseData({ choices: [{ delta: { content: "hello" } }] }));
    stream.enqueue(sseData({ choices: [], usage: { prompt_tokens: 1, completion_tokens: 1 } }));
    stream.enqueue(doneData());
    const events = await eventsPromise;
    await runner.terminate();

    expect(events.map((event) => event.kind)).toContain("failed");
    expect(
      events.some((event) => event.kind === "failed" && event.error.includes("disk full")),
    ).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
    expect(events.some((event) => event.kind === "receipt")).toBe(false);
  });

  it("emits failed with structured cause on network failure mid-stream", async () => {
    const harness = makeHarness();
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(sseData({ choices: [{ delta: { content: "partial" } }] }));
    await waitForEvent(harness.events, (event) => event.kind === "stdout");
    stream.error(new Error("socket reset"));
    const events = await eventsPromise;

    expect(events.map((event) => event.kind)).toContain("stdout");
    expect(
      events.some(
        (event) =>
          event.kind === "failed" &&
          event.error.includes("openai_compat_network_error") &&
          event.error.includes("socket reset"),
      ),
    ).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
  });

  it("maps HTTP 5xx responses to failed with a safely truncated response body", async () => {
    const harness = makeHarness();
    const longBody = `upstream unavailable ${"x".repeat(4_000)}`;
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(longBody, { status: 503, statusText: "Service Unavailable" });

    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const events = await collectAll(runner.events());

    expect(events.map((event) => event.kind)).toEqual(["failed"]);
    const failed = events[0];
    if (failed?.kind !== "failed") throw new Error("expected failed event");
    expect(failed.error).toContain("openai_compat_http_error");
    expect(failed.error).toContain('"status":503');
    expect(failed.error).toContain("upstream unavailable");
    expect(failed.error).toContain("[truncated]");
    expect(failed.error.length).toBeLessThan(longBody.length);
  });

  it("aborts and waits for drain when terminate is called during streaming", async () => {
    const harness = makeHarness();
    const stream = new ControlledSseStream();
    const abortSeen = deferred<void>();
    const signalSeen = deferred<AbortSignal>();
    const fetchFn: OpenAICompatFetch = async (_input, init) => {
      const signal = init?.signal;
      if (!(signal instanceof AbortSignal)) throw new Error("missing abort signal");
      signalSeen.resolve(signal);
      signal.addEventListener("abort", () => abortSeen.resolve(), { once: true });
      return new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    };
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(sseData({ choices: [{ delta: { content: "partial" } }] }));
    await waitForEvent(harness.events, (event) => event.kind === "stdout");
    const terminatePromise = runner.terminate();
    await abortSeen.promise;
    await terminatePromise;
    const events = await eventsPromise;

    const signal = await signalSeen.promise;
    expect(signal.aborted).toBe(true);
    expect(events.map((event) => event.kind)).toContain("stdout");
    expect(events.some((event) => event.kind === "failed")).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
  });

  it("selects provider auth shape from CredentialScope", async () => {
    const openai = await completedAuthRun(asCredentialScope("openai"));
    expect(headersFrom(firstCall(openai.calls)).get("Authorization")).toBe("Bearer sk-test-secret");
    expect(headersFrom(firstCall(openai.calls)).get("x-api-key")).toBeNull();
    expect(openai.events.some((event) => event.kind === "stderr")).toBe(false);

    const anthropic = await completedAuthRun(asCredentialScope("anthropic"));
    expect(headersFrom(firstCall(anthropic.calls)).get("x-api-key")).toBe("sk-test-secret");
    expect(headersFrom(firstCall(anthropic.calls)).get("Authorization")).toBeNull();
    expect(anthropic.events.some((event) => event.kind === "stderr")).toBe(false);

    const unknown = await completedAuthRun(asCredentialScope("github"));
    expect(headersFrom(firstCall(unknown.calls)).get("Authorization")).toBe(
      "Bearer sk-test-secret",
    );
    expect(
      unknown.events.some(
        (event) =>
          event.kind === "stderr" &&
          event.chunk.includes("openai_compat_unknown_credential_scope") &&
          event.chunk.includes("authorization_bearer"),
      ),
    ).toBe(true);
  });

  it("emits a zero-cost ledger entry with a structured note when usage is missing", async () => {
    const harness = makeHarness();
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(sseData({ choices: [{ delta: { content: "hello" } }] }));
    stream.enqueue(doneData());
    const events = await eventsPromise;

    const event = costEvent(events);
    expect(event.entry).toMatchObject({
      amountMicroUsd: 0,
      units: {
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheCreationTokens: 0,
      },
    });
    expect(recordValue(event.entry, "note")).toBe("provider_did_not_report_usage");
    expect(harness.costs).toHaveLength(1);
    expect(recordValue(harness.costs[0] ?? {}, "note")).toBeUndefined();
    expect(events.map((item) => item.kind)).toEqual([
      "started",
      "stdout",
      "cost",
      "receipt",
      "finished",
    ]);
  });

  it("fails unresolved fetches when the default timeout signal fires", async () => {
    const harness = makeHarness();
    const timeoutController = new AbortController();
    const timeoutSpy = vi.spyOn(AbortSignal, "timeout").mockReturnValue(timeoutController.signal);
    const fetchFn: OpenAICompatFetch = async () =>
      await new Promise<Response>(() => {
        return undefined;
      });

    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    timeoutController.abort(new DOMException("timed out", "TimeoutError"));
    const events = await eventsPromise;

    expect(timeoutSpy).toHaveBeenCalledWith(OPENAI_COMPAT_DEFAULT_TIMEOUT_MS);
    expect(
      events.some(
        (event) =>
          event.kind === "failed" &&
          event.error.includes("openai_compat_timeout") &&
          event.error.includes("60000"),
      ),
    ).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
  });

  it("redacts secret material from stdout, failed events, and receipts", async () => {
    const credentialFixture = "abcdefghijklmnop";
    const harness = makeHarness({ secret: credentialFixture });
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(sseData({ choices: [{ delta: { content: `hello ${credentialFixture}` } }] }));
    stream.enqueue(sseData({ choices: [], usage: { prompt_tokens: 0, completion_tokens: 0 } }));
    stream.enqueue(doneData());
    const events = await eventsPromise;
    const serialized = JSON.stringify(events);

    expect(serialized).not.toContain(credentialFixture);
    expect(serialized).toContain("<redacted>");
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("hello <redacted>");

    const failedHarness = makeHarness({ secret: credentialFixture });
    const failedFetch: OpenAICompatFetch = async () =>
      new Response(`upstream ${credentialFixture.slice(0, 12)}`, { status: 503 });
    const failedRunner = await createSpawner(failedFetch)(spawnRequest(), failedHarness.deps);
    const failedEvents = await collectAll(failedRunner.events());
    expect(JSON.stringify(failedEvents)).not.toContain(credentialFixture.slice(0, 12));
    expect(JSON.stringify(failedEvents)).toContain("<redacted>");
  });

  it("redacts secrets split across SSE events without masking near misses", async () => {
    const credentialFixture = redactionFixture();
    const harness = makeHarness({ secret: credentialFixture });
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    for (const chunk of credentialFixture.match(/.{1,2}/g) ?? []) {
      stream.enqueue(sseData({ choices: [{ delta: { content: chunk } }] }));
    }
    stream.enqueue(sseData({ choices: [], usage: { prompt_tokens: 0, completion_tokens: 0 } }));
    stream.enqueue(doneData());
    const events = await eventsPromise;
    const serialized = JSON.stringify(events);

    expect(serialized).not.toContain(credentialFixture);
    expect(serialized).toContain("<redacted>");
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("<redacted>");

    const nearMiss = `${credentialFixture.slice(0, 12)}X${credentialFixture.slice(13)}`;
    const nearMissHarness = makeHarness({ secret: credentialFixture });
    const nearMissStream = new ControlledSseStream();
    const nearMissFetch: OpenAICompatFetch = async () =>
      new Response(nearMissStream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const nearMissRunner = await createSpawner(nearMissFetch)(spawnRequest(), nearMissHarness.deps);
    const nearMissEventsPromise = collectAll(nearMissRunner.events());
    for (const chunk of nearMiss.match(/.{1,2}/g) ?? []) {
      nearMissStream.enqueue(sseData({ choices: [{ delta: { content: chunk } }] }));
    }
    nearMissStream.enqueue(
      sseData({ choices: [], usage: { prompt_tokens: 0, completion_tokens: 0 } }),
    );
    nearMissStream.enqueue(doneData());
    const nearMissEvents = await nearMissEventsPromise;

    expect(nearMissHarness.receipts[0]?.finalMessage?.toString()).toBe(nearMiss);
    expect(JSON.stringify(nearMissEvents)).not.toContain("<redacted>");
  });

  it("fails closed on oversized SSE events before writing receipts", async () => {
    const harness = makeHarness();
    const stream = new ControlledSseStream();
    const fetchFn: OpenAICompatFetch = async () =>
      new Response(stream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    stream.enqueue(`data: ${"x".repeat(17 * 1024 * 1024)}`);
    const events = await eventsPromise;

    expect(
      events.some(
        (event) => event.kind === "failed" && event.code === "runner_input_buffer_overflow",
      ),
    ).toBe(true);
    expect(harness.receipts).toHaveLength(0);
    expect(harness.costs).toHaveLength(0);
  });

  it("rejects negative usage and cost ceiling overflows", async () => {
    const negativeHarness = makeHarness();
    const negativeStream = new ControlledSseStream();
    const negativeFetch: OpenAICompatFetch = async () =>
      new Response(negativeStream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const negativeRunner = await createSpawner(negativeFetch)(spawnRequest(), negativeHarness.deps);
    const negativeEventsPromise = collectAll(negativeRunner.events());
    negativeStream.enqueue(sseData({ choices: [], usage: { prompt_tokens: -1 } }));
    const negativeEvents = await negativeEventsPromise;

    expect(
      negativeEvents.some(
        (event) => event.kind === "failed" && event.code === "provider_returned_error",
      ),
    ).toBe(true);
    expect(negativeEvents.some((event) => event.kind === "cost")).toBe(false);

    const ceilingHarness = makeHarness();
    const ceilingStream = new ControlledSseStream();
    const ceilingFetch: OpenAICompatFetch = async () =>
      new Response(ceilingStream.body, {
        headers: { "Content-Type": "text/event-stream" },
        status: 200,
      });
    const ceilingRunner = await createSpawner(ceilingFetch)(
      { ...spawnRequest(), costCeilingMicroUsd: asMicroUsd(1) },
      ceilingHarness.deps,
    );
    const ceilingEventsPromise = collectAll(ceilingRunner.events());
    ceilingStream.enqueue(sseData({ choices: [], usage: { prompt_tokens: 2 } }));
    const ceilingEvents = await ceilingEventsPromise;

    expect(
      ceilingEvents.some(
        (event) => event.kind === "failed" && event.code === "cost_ceiling_exceeded",
      ),
    ).toBe(true);
    expect(ceilingEvents.some((event) => event.kind === "cost")).toBe(false);
  });
});

async function completedAuthRun(scope: CredentialScope): Promise<{
  readonly calls: FetchCall[];
  readonly events: RunnerEvent[];
}> {
  const harness = makeHarness({ scope });
  const stream = new ControlledSseStream();
  const calls: FetchCall[] = [];
  const fetchFn: OpenAICompatFetch = async (input, init) => {
    calls.push({ input, init });
    return new Response(stream.body, {
      headers: { "Content-Type": "text/event-stream" },
      status: 200,
    });
  };
  const runner = await createSpawner(fetchFn)(spawnRequest(), harness.deps);
  const eventsPromise = collectAll(runner.events());

  stream.enqueue(sseData({ choices: [], usage: { prompt_tokens: 0, completion_tokens: 0 } }));
  stream.enqueue(doneData());
  const events = await eventsPromise;
  return { calls, events };
}

function deferred<T>(): {
  readonly promise: Promise<T>;
  readonly resolve: (value: T | PromiseLike<T>) => void;
  readonly reject: (reason: unknown) => void;
} {
  let resolveFn: ((value: T | PromiseLike<T>) => void) | null = null;
  let rejectFn: ((reason: unknown) => void) | null = null;
  const promise = new Promise<T>((resolve, reject) => {
    resolveFn = resolve;
    rejectFn = reject;
  });
  return {
    promise,
    resolve(value) {
      if (resolveFn === null) throw new Error("deferred resolve not initialized");
      resolveFn(value);
    },
    reject(reason) {
      if (rejectFn === null) throw new Error("deferred reject not initialized");
      rejectFn(reason);
    },
  };
}
