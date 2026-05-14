import { EventEmitter } from "node:events";
import { PassThrough } from "node:stream";
import type { ReadableStream } from "node:stream/web";

import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  asMicroUsd,
  asProviderKind,
  asReceiptId,
  asRunnerId,
  asTaskId,
  type CredentialHandle,
  createCredentialHandle,
  type ProviderKind,
  type RunnerEvent,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import {
  type ClaudeCliChildProcess,
  type ClaudeCliSpawnOptions,
  createClaudeCliRunner,
} from "../../src/adapters/claude-cli.ts";
import { ClaudeCliNotAvailable } from "../../src/errors.ts";
import type { RunnerEventRecord } from "../../src/internal/event-hub.ts";
import type { Receipt, RunnerSpawnDeps } from "../../src/runner.ts";

const agentId = asAgentId("agent_alpha");
const credential = createCredentialHandle({
  id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN"),
  agentId,
  scope: asCredentialScope("anthropic"),
});
const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");
const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const taskId = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const fixedDate = new Date("2026-05-08T18:00:00.000Z");

class FakeClaudeChild extends EventEmitter implements ClaudeCliChildProcess {
  readonly stdout = new PassThrough();
  readonly stderr = new PassThrough();
  readonly signals: NodeJS.Signals[] = [];

  override once(
    event: "exit" | "error",
    listener:
      | ((code: number | null, signal: NodeJS.Signals | null) => void)
      | ((error: Error) => void),
  ): this {
    return super.once(event, listener);
  }

  kill(signal: NodeJS.Signals = "SIGTERM"): boolean {
    this.signals.push(signal);
    if (signal === "SIGTERM" || signal === "SIGKILL") {
      this.stdout.end();
      this.stderr.end();
      queueMicrotask(() => this.emit("exit", 130, signal));
    }
    return true;
  }

  writeStdout(line: string): void {
    this.stdout.write(line);
  }

  writeStderr(chunk: string): void {
    this.stderr.write(chunk);
  }

  exit(code: number): void {
    this.stdout.end();
    this.stderr.end();
    this.emit("exit", code, null);
  }

  fail(error: Error): void {
    this.stdout.end();
    this.stderr.end();
    this.emit("error", error);
  }
}

interface Harness {
  readonly child: FakeClaudeChild;
  readonly calls: Array<{
    readonly command: string;
    readonly args: readonly string[];
    readonly options: ClaudeCliSpawnOptions;
  }>;
  readonly deps: RunnerSpawnDeps;
  readonly events: RunnerEvent[];
  readonly receipts: Receipt[];
  readonly secretReads: CredentialHandle[];
}

function makeHarness(
  args: {
    readonly receiptPut?: ((receipt: Receipt) => Promise<{ readonly stored: boolean }>) | undefined;
    readonly resolvedProviderKind?: ProviderKind | undefined;
    readonly secret?: string | undefined;
  } = {},
): Harness {
  const child = new FakeClaudeChild();
  const calls: Harness["calls"] = [];
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
    child,
    calls,
    events,
    receipts,
    secretReads,
    deps: {
      credential,
      resolvedProviderKind: args.resolvedProviderKind ?? asProviderKind("anthropic"),
      secretReader: async (handle) => {
        secretReads.push(handle);
        return args.secret ?? "sk-ant-こんにちは";
      },
      costLedger: { record: async () => undefined },
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

function spawnRequest() {
  return {
    kind: "claude-cli" as const,
    agentId,
    credential: {
      version: 1 as const,
      id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN"),
    },
    prompt: "Say hello",
    model: "claude-sonnet-4-7",
    taskId,
  };
}

function redactionFixture(): string {
  return ["redact safe", "fixture value", "token!"].join(" ");
}

async function collectAll(stream: ReadableStream<RunnerEvent>): Promise<RunnerEvent[]> {
  const reader = stream.getReader();
  const events: RunnerEvent[] = [];
  while (true) {
    const next = await reader.read();
    if (next.done) return events;
    events.push(next.value);
  }
}

async function collectRecords(
  stream: ReadableStream<RunnerEventRecord>,
): Promise<RunnerEventRecord[]> {
  const reader = stream.getReader();
  const records: RunnerEventRecord[] = [];
  while (true) {
    const next = await reader.read();
    if (next.done) return records;
    records.push(next.value);
  }
}

function deferred<T>(): {
  readonly promise: Promise<T>;
  readonly resolve: (value: T | PromiseLike<T>) => void;
} {
  let resolveFn: ((value: T | PromiseLike<T>) => void) | null = null;
  const promise = new Promise<T>((resolve) => {
    resolveFn = resolve;
  });
  return {
    promise,
    resolve(value) {
      if (resolveFn === null) throw new Error("deferred resolve not initialized");
      resolveFn(value);
    },
  };
}

async function waitFor(predicate: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (predicate()) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  throw new Error("timed out waiting for condition");
}

function assistantLine(text: string): string {
  return `${JSON.stringify({
    type: "assistant",
    message: {
      model: "claude-sonnet-4-7",
      content: [{ type: "text", text }],
      usage: {
        input_tokens: 10,
        output_tokens: 5,
        cache_read_input_tokens: 2,
        cache_creation_input_tokens: 3,
      },
    },
  })}\n`;
}

function usageLine(inputTokens: number, model = "claude-sonnet-4-7"): string {
  return `${JSON.stringify({
    type: "assistant",
    message: {
      model,
      content: [],
      usage: {
        input_tokens: inputTokens,
        output_tokens: 5,
        cache_read_input_tokens: 0,
        cache_creation_input_tokens: 0,
      },
    },
  })}\n`;
}

describe("createClaudeCliRunner", () => {
  it("runs a successful turn, writes a receipt, and emits cost as usage arrives", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
      taskIdFactory: () => taskId,
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine("hello"));
    harness.child.exit(0);
    const events = await eventsPromise;

    expect(harness.secretReads).toEqual([credential]);
    expect(harness.calls[0]?.command).toBe("/usr/bin/claude");
    expect(harness.calls[0]?.args).toEqual([
      "--print",
      "--output-format",
      "stream-json",
      "--",
      "Say hello",
    ]);
    const { ANTHROPIC_API_KEY, LC_ALL, PATH } = harness.calls[0]?.options.env ?? {};
    expect(ANTHROPIC_API_KEY).toBe("sk-ant-こんにちは");
    expect(LC_ALL).toBe("C");
    expect(PATH).toBe("/usr/bin");
    expect(Object.keys(harness.calls[0]?.options.env ?? {}).sort()).toEqual(
      expect.arrayContaining(["ANTHROPIC_API_KEY", "LC_ALL", "PATH"]),
    );
    expect(events.map((event) => event.kind)).toEqual([
      "started",
      "stdout",
      "cost",
      "receipt",
      "finished",
    ]);
    expect(harness.events.map((event) => event.kind)).toEqual(events.map((event) => event.kind));
    const cost = events.find((event) => event.kind === "cost");
    expect(cost?.kind).toBe("cost");
    if (cost?.kind === "cost") {
      expect(cost.entry.receiptId).toBe(receiptId);
      expect(cost.entry.taskId).toBe(taskId);
      expect(cost.entry.providerKind).toBe(asProviderKind("anthropic"));
    }
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]?.id).toBe(receiptId);
    expect(harness.receipts[0]?.taskId).toBe(taskId);
    expect(harness.receipts[0]?.providerKind).toBe(asProviderKind("anthropic"));
    expect(harness.receipts[0]?.inputTokens).toBe(10);
    expect(harness.receipts[0]?.outputTokens).toBe(5);
  });

  it("starts the durable event stream before buffered stdout and stderr", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
      taskIdFactory: () => taskId,
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const recordsPromise = collectRecords(runner.eventRecords());
    for (let index = 0; index < 100; index += 1) {
      harness.child.writeStdout(assistantLine(`out-${index}`));
      harness.child.writeStderr(`err-${index}\n`);
    }
    harness.child.exit(0);
    const records = await recordsPromise;

    expect(records[0]?.event.kind).toBe("started");
    const lsns = records.map((record) => record.lsn);
    expect(lsns).toEqual(Array.from({ length: records.length }, (_value, index) => index + 1));
  });

  it("uses broker-resolved provider kind for cost and receipt attribution", async () => {
    const harness = makeHarness({ resolvedProviderKind: asProviderKind("openclaw") });
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
      taskIdFactory: () => taskId,
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine("hello"));
    harness.child.exit(0);
    const events = await eventsPromise;
    const cost = events.find((event) => event.kind === "cost");

    expect(cost?.kind).toBe("cost");
    if (cost?.kind === "cost") {
      expect(cost.entry.providerKind).toBe(asProviderKind("openclaw"));
    }
    expect(harness.receipts[0]?.providerKind).toBe(asProviderKind("openclaw"));
  });

  it.each([
    "--no-receipt foo bar",
    "-h",
    "",
  ])("passes prompt after an argv separator: %j", async (prompt) => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner({ ...spawnRequest(), prompt }, harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.exit(0);
    await eventsPromise;

    expect(harness.calls[0]?.args).toEqual([
      "--print",
      "--output-format",
      "stream-json",
      "--",
      prompt,
    ]);
  });

  it("fails the runner when receipt put fails", async () => {
    const harness = makeHarness({
      receiptPut: async () => {
        throw new Error("disk full");
      },
    });
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
      taskIdFactory: () => taskId,
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine("hello"));
    harness.child.exit(0);
    const events = await eventsPromise;

    expect(
      events.some((event) => event.kind === "failed" && event.error.includes("disk full")),
    ).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
  });

  it("emits one terminal event when terminate wins a pending receipt write", async () => {
    const successReceiptRelease = deferred<{ readonly stored: boolean }>();
    const firstReceiptStarted = deferred<void>();
    let putCount = 0;
    const harness = makeHarness({
      receiptPut: async (receipt) => {
        harness.receipts.push(receipt);
        putCount += 1;
        if (putCount === 1) {
          firstReceiptStarted.resolve();
          return await successReceiptRelease.promise;
        }
        return { stored: true };
      },
    });
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
      taskIdFactory: () => taskId,
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine("hello"));
    harness.child.exit(0);
    await firstReceiptStarted.promise;
    const terminatePromise = runner.terminate({ gracePeriodMs: 50 });
    await waitFor(() => harness.receipts.length === 2);
    successReceiptRelease.resolve({ stored: true });
    await terminatePromise;
    const events = await eventsPromise;

    const terminalEvents = events.filter(
      (event) => event.kind === "failed" || event.kind === "finished",
    );
    expect(terminalEvents.map((event) => event.kind)).toEqual(["failed"]);
    expect(events.some((event) => event.kind === "receipt")).toBe(false);
    expect(harness.receipts[1]).toMatchObject({
      id: receiptId,
      providerKind: asProviderKind("anthropic"),
      status: "error",
    });
  });

  it("emits failed on subprocess crash mid-stream", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine("partial"));
    harness.child.writeStderr("boom");
    harness.child.exit(2);
    const events = await eventsPromise;

    expect(events.map((event) => event.kind)).toContain("stderr");
    expect(events.some((event) => event.kind === "failed" && event.error.includes("code 2"))).toBe(
      true,
    );
    expect(events.some((event) => event.kind === "finished")).toBe(false);
    expect(harness.receipts[0]).toMatchObject({
      id: receiptId,
      status: "error",
      providerKind: asProviderKind("anthropic"),
    });
  });

  it("terminates during streaming with SIGTERM and waits for process exit", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });
    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());

    await runner.terminate({ gracePeriodMs: 50 });
    await runner.terminate({ gracePeriodMs: 50 });
    const events = await eventsPromise;

    expect(harness.child.signals).toEqual(["SIGTERM"]);
    expect(
      events.some((event) => event.kind === "failed" && event.code === "terminated_by_request"),
    ).toBe(true);
  });

  it("handles trailing newlines without emitting empty failures", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(`${assistantLine("hello")}\n`);
    harness.child.exit(0);
    const events = await eventsPromise;

    expect(events.filter((event) => event.kind === "failed")).toHaveLength(0);
  });

  it("redacts secret material from stdout, stderr, failures, and receipts", async () => {
    const credentialFixture = "abcdefghijklmnop";
    const harness = makeHarness({ secret: credentialFixture });
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(assistantLine(`stdout ${credentialFixture}`));
    harness.child.writeStderr(`stderr ${credentialFixture.slice(0, 12)}`);
    harness.child.exit(0);
    const events = await eventsPromise;
    const serialized = JSON.stringify(events);

    expect(serialized).not.toContain(credentialFixture);
    expect(serialized).not.toContain(credentialFixture.slice(0, 12));
    expect(serialized).toContain("<redacted>");
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("stdout <redacted>");
  });

  it("redacts a secret split across stdout events", async () => {
    const credentialFixture = redactionFixture();
    const harness = makeHarness({ secret: credentialFixture });
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    for (const chunk of credentialFixture.match(/.{1,2}/g) ?? []) {
      harness.child.writeStdout(assistantLine(chunk));
    }
    harness.child.exit(0);
    const events = await eventsPromise;
    const serialized = JSON.stringify(events);

    expect(serialized).not.toContain(credentialFixture);
    expect(serialized).toContain("<redacted>");
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("<redacted>");
  });

  it("fails closed and terminates the subprocess on oversized stdout lines", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("x".repeat(17 * 1024 * 1024));
    const events = await eventsPromise;

    expect(
      events.some(
        (event) => event.kind === "failed" && event.code === "runner_input_buffer_overflow",
      ),
    ).toBe(true);
    expect(harness.child.signals).toEqual(["SIGTERM"]);
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]).toMatchObject({
      providerKind: asProviderKind("anthropic"),
      status: "error",
    });
    expect(harness.receipts[0]?.error?.toString()).toContain("runner input buffer exceeded");
  });

  it("fails negative usage and cost ceiling overflows before recording cost", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(
      { ...spawnRequest(), costCeilingMicroUsd: asMicroUsd(1) },
      harness.deps,
    );
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(usageLine(10));
    const events = await eventsPromise;

    expect(
      events.some((event) => event.kind === "failed" && event.code === "cost_ceiling_exceeded"),
    ).toBe(true);
    expect(events.some((event) => event.kind === "cost")).toBe(false);
  });

  it("rejects negative provider usage", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(usageLine(-1));
    const events = await eventsPromise;

    expect(
      events.some((event) => event.kind === "failed" && event.code === "provider_returned_error"),
    ).toBe(true);
    expect(events.some((event) => event.kind === "cost")).toBe(false);
  });

  it("clamps provider-reported model identifiers to the request model", async () => {
    const harness = makeHarness();
    const spawnRunner = createClaudeCliRunner({
      binaryPath: "/usr/bin/claude",
      enforceTrustedCommand: false,
      now: () => fixedDate,
      receiptIdFactory: () => receiptId,
      runnerIdFactory: () => runnerId,
      spawner: (command, args, options) => {
        harness.calls.push({ command, args, options });
        return harness.child;
      },
    });

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(usageLine(10, "claude-sonnet-4-7\nid: injected"));
    harness.child.exit(0);
    const events = await eventsPromise;
    const cost = events.find((event) => event.kind === "cost");

    expect(cost?.kind).toBe("cost");
    if (cost?.kind === "cost") {
      expect(cost.entry.model).toBe("claude-sonnet-4-7");
    }
  });

  it("rejects unavailable trusted Claude binaries at construction", () => {
    expect(() =>
      createClaudeCliRunner({
        candidatePaths: ["/definitely/missing/claude"],
        enforceTrustedCommand: true,
      }),
    ).toThrow(ClaudeCliNotAvailable);
  });
});
