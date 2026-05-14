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
  type ProviderKind,
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
    readonly resolvedProviderKind?: ProviderKind | undefined;
    readonly secret?: string | undefined;
  } = {},
): Harness {
  const child = new FakeCodexChild();
  const calls: Harness["calls"] = [];
  const costEntries: CostLedgerEntry[] = [];
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
    costEntries,
    events,
    receipts,
    secretReads,
    deps: {
      credential,
      resolvedProviderKind: args.resolvedProviderKind ?? asProviderKind("openai"),
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
          lsn += 1;
          return lsn;
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
      "--",
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
    expect(harness.costEntries[0]?.receiptId).toBe(receiptId);
    expect(harness.costEntries[0]?.taskId).toBe(taskId);
    expect(harness.costEntries[0]?.units.inputTokens).toBe(123);
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]?.providerKind).toBe(asProviderKind("openai"));
    expect(harness.receipts[0]?.inputTokens).toBe(123);
    expect(harness.receipts[0]?.costUsd).toBe(0.000246);
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("Done.\n");
  });

  it.each([
    "--no-receipt foo bar",
    "-h",
    "",
  ])("passes prompt after an argv separator: %j", async (prompt) => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner({ ...spawnRequest(), prompt }, harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.exit(0);
    await eventsPromise;

    expect(harness.calls[0]?.args.slice(-2)).toEqual(["--", prompt]);
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
    await Promise.resolve();

    expect(harness.child.signals).toEqual(["SIGTERM"]);
    await vi.advanceTimersByTimeAsync(50);
    expect(harness.child.signals).toEqual(["SIGTERM", "SIGKILL"]);
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

  it("redacts secret material from stdout, stderr, and receipts", async () => {
    const credentialFixture = "abcdefghijklmnop";
    const harness = makeHarness({ secret: credentialFixture });
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("--------\n");
    harness.child.writeStdout(`final ${credentialFixture}\n`);
    harness.child.writeStderr(`stderr ${credentialFixture.slice(0, 12)}`);
    harness.child.exit(0);
    const events = await eventsPromise;
    const serialized = JSON.stringify(events);

    expect(serialized).not.toContain(credentialFixture);
    expect(serialized).not.toContain(credentialFixture.slice(0, 12));
    expect(serialized).toContain("<redacted>");
    expect(harness.receipts[0]?.finalMessage?.toString()).toBe("final <redacted>\n");
  });

  it("fails closed and terminates the subprocess on oversized stdout lines", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout("x".repeat(17 * 1024 * 1024));
    await waitForSignal(harness.child);
    harness.child.exit(1, "SIGTERM");
    const events = await eventsPromise;

    expect(
      events.some(
        (event) => event.kind === "failed" && event.code === "runner_input_buffer_overflow",
      ),
    ).toBe(true);
    expect(harness.child.signals).toEqual(["SIGTERM"]);
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]).toMatchObject({
      providerKind: asProviderKind("openai"),
      status: "error",
    });
    expect(harness.receipts[0]?.error?.toString()).toContain("runner input buffer exceeded");
  });

  it("fails closed when a codex output block has too many lines", async () => {
    const harness = makeHarness();
    const spawnRunner = makeSpawnRunner(harness);

    const runner = await spawnRunner(spawnRequest(), harness.deps);
    const eventsPromise = collectAll(runner.events());
    harness.child.writeStdout(
      Array.from({ length: 1025 }, (_, index) => `line ${index}\n`).join(""),
    );
    await waitForSignal(harness.child);
    harness.child.exit(1, "SIGTERM");
    const events = await eventsPromise;

    expect(
      events.some(
        (event) => event.kind === "failed" && event.code === "runner_input_buffer_overflow",
      ),
    ).toBe(true);
    expect(harness.receipts).toHaveLength(1);
    expect(harness.receipts[0]).toMatchObject({
      providerKind: asProviderKind("openai"),
      status: "error",
    });
    expect(harness.receipts[0]?.error?.toString()).toContain("Codex CLI output block exceeded");
  });

  it("rejects negative usage and cost ceiling overflows", async () => {
    const negativeHarness = makeHarness();
    const negativeRunner = await makeSpawnRunner(negativeHarness)(
      spawnRequest(),
      negativeHarness.deps,
    );
    const negativeEventsPromise = collectAll(negativeRunner.events());
    negativeHarness.child.writeStdout("tokens used: -1\n");
    await Promise.resolve();
    await Promise.resolve();
    negativeHarness.child.exit(1, "SIGTERM");
    const negativeEvents = await negativeEventsPromise;

    expect(
      negativeEvents.some(
        (event) => event.kind === "failed" && event.code === "provider_returned_error",
      ),
    ).toBe(true);
    expect(negativeEvents.some((event) => event.kind === "cost")).toBe(false);

    const ceilingHarness = makeHarness();
    const ceilingRunner = await makeSpawnRunner(ceilingHarness)(
      { ...spawnRequest(), costCeilingMicroUsd: asMicroUsd(1) },
      ceilingHarness.deps,
    );
    const ceilingEventsPromise = collectAll(ceilingRunner.events());
    ceilingHarness.child.writeStdout("tokens used: 123\n");
    await Promise.resolve();
    await Promise.resolve();
    ceilingHarness.child.exit(1, "SIGTERM");
    const ceilingEvents = await ceilingEventsPromise;

    expect(
      ceilingEvents.some(
        (event) => event.kind === "failed" && event.code === "cost_ceiling_exceeded",
      ),
    ).toBe(true);
    expect(ceilingEvents.some((event) => event.kind === "cost")).toBe(false);
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

async function waitForSignal(child: FakeCodexChild): Promise<void> {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (child.signals.length > 0) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  throw new Error("timed out waiting for subprocess termination signal");
}
