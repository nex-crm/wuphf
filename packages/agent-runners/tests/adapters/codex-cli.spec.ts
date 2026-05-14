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
  type CostLedgerEntry,
  type CredentialHandle,
  createCredentialHandle,
  type RunnerEvent,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  type CodexCliChildProcess,
  type CodexCliSpawnOptions,
  createCodexCliRunner,
} from "../../src/adapters/codex-cli.ts";
import { CodexCliNotAvailable } from "../../src/errors.ts";
import type { Receipt, RunnerSpawnDeps } from "../../src/runner.ts";

const agentId = asAgentId("agent_alpha");
const credential = createCredentialHandle({
  id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN"),
  agentId,
  scope: asCredentialScope("openai"),
});
const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");
const receiptId = asReceiptId("01ARZ3NDEKTSV4RRFFQ69G5FAV");
const taskId = asTaskId("01ARZ3NDEKTSV4RRFFQ69G5FAW");
const fixedDate = new Date("2026-05-08T18:00:00.000Z");

class FakeCodexChild extends EventEmitter implements CodexCliChildProcess {
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
    return true;
  }

  writeStdout(chunk: string): void {
    this.stdout.write(chunk);
  }

  writeStderr(chunk: string): void {
    this.stderr.write(chunk);
  }

  exit(code: number, signal: NodeJS.Signals | null = null): void {
    this.stdout.end();
    this.stderr.end();
    this.emit("exit", code, signal);
  }

  fail(error: Error): void {
    this.stdout.end();
    this.stderr.end();
    this.emit("error", error);
  }
}

interface Harness {
  readonly child: FakeCodexChild;
  readonly calls: Array<{
    readonly command: string;
    readonly args: readonly string[];
    readonly options: CodexCliSpawnOptions;
  }>;
  readonly costEntries: CostLedgerEntry[];
  readonly deps: RunnerSpawnDeps;
  readonly events: RunnerEvent[];
  readonly receipts: Receipt[];
  readonly secretReads: CredentialHandle[];
}

function makeHarness(
  args: {
    readonly receiptPut?: ((receipt: Receipt) => Promise<{ readonly stored: boolean }>) | undefined;
    readonly secret?: string | undefined;
  } = {},
): Harness {
  const child = new FakeCodexChild();
  const calls: Harness["calls"] = [];
  const costEntries: CostLedgerEntry[] = [];
  const events: RunnerEvent[] = [];
  const receipts: Receipt[] = [];
  const secretReads: CredentialHandle[] = [];
  const receiptPut =
    args.receiptPut ??
    (async (receipt: Receipt) => {
      receipts.push(receipt);
      return { stored: true };
    });
  return {
    child,
    calls,
    costEntries,
    events,
    receipts,
    secretReads,
    deps: {
      credential,
      secretReader: async (handle) => {
        secretReads.push(handle);
        return args.secret ?? "fixture-openai-secret-do-not-use";
      },
      costLedger: {
        record: async (entry) => {
          costEntries.push(entry);
        },
      },
      receiptStore: { put: receiptPut },
      eventLog: {
        append: async (event) => {
          events.push(event);
        },
      },
    },
  };
}

function spawnRequest() {
  return {
    kind: "codex-cli" as const,
    agentId,
    credential: {
      version: 1 as const,
      id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN"),
    },
    prompt: "Run tests and summarize",
    model: "gpt-5",
    cwd: "/workspace/project",
    taskId,
  };
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

function makeSpawnRunner(harness: Harness) {
  return createCodexCliRunner({
    binaryPath: "/usr/bin/codex",
    costEstimator: (input) => asMicroUsd(input.totalTokens * 2),
    enforceTrustedCommand: false,
    now: () => fixedDate,
    outputLastMessagePath: "/tmp/codex-last-message.txt",
    receiptIdFactory: () => receiptId,
    runnerIdFactory: () => runnerId,
    spawner: (command, args, options) => {
      harness.calls.push({ command, args, options });
      return harness.child;
    },
    taskIdFactory: () => taskId,
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("createCodexCliRunner", () => {
  it("runs a successful turn with a tool call, cost event, receipt, and final message", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("--------\n");
    harness.child.writeStdout("exec bunx vitest run in /workspace/project\n");
    harness.child.writeStdout("succeeded in 12ms\n");
    harness.child.writeStdout("hook: PostToolUse\n");
    harness.child.writeStdout("--------\n");
    harness.child.writeStdout("tokens used: 123\n");
    harness.child.writeStdout("--------\n");
    harness.child.writeStdout("Done.\n");
    harness.child.exit(0);
    const events = await eventsPromise;

    expect(harness.secretReads).toEqual([credential]);
    expect(harness.calls[0]?.command).toBe("/usr/bin/codex");
    expect(harness.calls[0]?.args).toEqual([
      "exec",
      "--sandbox",
      "workspace-write",
      "--profile",
      "auto",
      "--output-last-message",
      "/tmp/codex-last-message.txt",
      "--color",
      "never",
      "--cd",
      "/workspace/project",
      "--model",
      "gpt-5",
      "Run tests and summarize",
    ]);
    const env = harness.calls[0]?.options.env ?? {};
    const { ANTHROPIC_API_KEY, LC_ALL, OPENAI_API_KEY, PATH } = env;
    expect(OPENAI_API_KEY).toBe("fixture-openai-secret-do-not-use");
    expect(ANTHROPIC_API_KEY).toBeUndefined();
    expect(LC_ALL).toBe("C");
    expect(PATH).toBe("/usr/bin");
    expect(Object.keys(env).every((key) => allowedEnvKeys.has(key))).toBe(true);
    expect(events.map((event) => event.kind)).toEqual([
      "started",
      "stderr",
      "cost",
      "stdout",
      "receipt",
      "finished",
    ]);
    expect(harness.costEntries[0]?.amountMicroUsd).toBe(asMicroUsd(246));
    expect(harness.costEntries[0]?.units.inputTokens).toBe(123);
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]?.providerKind).toBe(asProviderKind("openai"));
    expect(harness.receipts[0]?.inputTokens).toBe(123);
    expect(harness.receipts[0]?.costUsd).toBe(0.000246);
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("Done.\n");
  });

  it("fails the runner when receipt put fails", async () => {
    const harness = makeHarness({
      receiptPut: async () => {
        throw new Error("disk full");
      },
    });
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("--------\nfinal message\n");
    harness.child.exit(0);
    const events = await eventsPromise;
    await runner.terminate({ gracePeriodMs: 10 });

    expect(
      events.some((event) => event.kind === "failed" && event.error.includes("disk full")),
    ).toBe(true);
    expect(events.some((event) => event.kind === "finished")).toBe(false);
  });

  it("emits failed on subprocess crash mid-stream", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("partial output before crash\n");
    harness.child.writeStderr("fatal stream error\n");
    harness.child.exit(2);
    const events = await eventsPromise;

    expect(events.map((event) => event.kind)).toContain("stderr");
    expect(events.some((event) => event.kind === "failed" && event.error.includes("code 2"))).toBe(
      true,
    );
    expect(events.some((event) => event.kind === "finished")).toBe(false);
    expect(harness.child.signals).toEqual([]);
  });

  it("terminates during streaming, waits for exit, and hard-kills after the grace period", async () => {
    vi.useFakeTimers();
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);
    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    let resolved = false;

    const terminatePromise = runner.terminate({ gracePeriodMs: 50 }).then(() => {
      resolved = true;
    });
    await Promise.resolve();

    expect(harness.child.signals).toEqual(["SIGINT"]);
    await vi.advanceTimersByTimeAsync(50);
    expect(harness.child.signals).toEqual(["SIGINT", "SIGKILL"]);
    expect(resolved).toBe(false);

    harness.child.exit(1, "SIGKILL");
    await terminatePromise;
    await eventsPromise;
    expect(resolved).toBe(true);
  });

  it("does not collapse locale-translated codex errors into availability failures", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStderr("Keine passenden Einträge gefunden\n");
    harness.child.exit(1);
    const events = await eventsPromise;

    expect(events.some((event) => event.kind === "stderr")).toBe(true);
    expect(events.some((event) => event.kind === "failed" && event.error.includes("code 1"))).toBe(
      true,
    );
  });

  it("emits one stderr summary for unrecognized output lines", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("nonsense one\nnonsense two\nnonsense three\n");
    harness.child.exit(0);
    const events = await eventsPromise;

    const summaries = events.filter(
      (event) => event.kind === "stderr" && event.chunk.includes("3 unrecognized line(s)"),
    );
    expect(summaries).toHaveLength(1);
    expect(events.some((event) => event.kind === "finished")).toBe(true);
  });

  it("rejects unavailable trusted Codex binaries at construction", () => {
    expect(() =>
      createCodexCliRunner({
        candidatePaths: ["/definitely/missing/codex"],
        enforceTrustedCommand: true,
      }),
    ).toThrow(CodexCliNotAvailable);
  });
});

const allowedEnvKeys = new Set([
  "OPENAI_API_KEY",
  "ANTHROPIC_API_KEY",
  "LC_ALL",
  "PATH",
  "HOME",
  "USER",
]);
