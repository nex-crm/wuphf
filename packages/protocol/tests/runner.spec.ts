import { describe, expect, it } from "vitest";

import {
  asAgentId,
  asMicroUsd,
  asRunnerId,
  isRunnerId,
  isRunnerKind,
  MAX_RUNNER_CWD_BYTES,
  MAX_RUNNER_ERROR_BYTES,
  MAX_RUNNER_MODEL_BYTES,
  MAX_RUNNER_PROMPT_BYTES,
  MAX_RUNNER_STDIO_CHUNK_BYTES,
  RUNNER_KIND_VALUES,
  runnerEventFromJson,
  runnerEventToJsonValue,
  runnerSpawnRequestFromJson,
  runnerSpawnRequestToJsonValue,
} from "../src/index.ts";
import runnerVectors from "../testdata/runner-vectors.json";

type RunnerVector = (typeof runnerVectors.vectors)[number];

const runnerId = "run_0123456789ABCDEFGHIJKLMNOPQRSTUV";
const receiptId = "01ARZ3NDEKTSV4RRFFQ69G5FAV";
const at = "2026-05-08T18:00:00.000Z";

function vector(name: string): RunnerVector {
  const found = runnerVectors.vectors.find((item) => item.name === name);
  if (found === undefined) throw new Error(`missing runner vector: ${name}`);
  return found;
}

function recordWithAccessor(
  base: Readonly<Record<string, unknown>>,
  key: string,
): Record<string, unknown> {
  const record: Record<string, unknown> = { ...base };
  Object.defineProperty(record, key, {
    enumerable: true,
    get() {
      throw new Error("accessor should not be invoked");
    },
  });
  return record;
}

describe("RunnerId and RunnerKind", () => {
  it("accepts the frozen runner kinds and branded ids", () => {
    expect(RUNNER_KIND_VALUES).toEqual(["claude-cli", "codex-cli", "openai-compat"]);
    expect(isRunnerKind("claude-cli")).toBe(true);
    expect(isRunnerKind("bogus")).toBe(false);
    expect(asRunnerId(runnerId)).toBe(runnerId);
    expect(isRunnerId(runnerId)).toBe(true);
    expect(isRunnerId("runner_short")).toBe(false);
    expect(() => asRunnerId("runner_short")).toThrow(/not a RunnerId/);
  });
});

describe("RunnerSpawnRequest codec", () => {
  it("round-trips the golden spawn vector", () => {
    const request = runnerSpawnRequestFromJson(vector("claude-cli spawn request").json);

    expect(request.kind).toBe("claude-cli");
    expect(request.agentId).toBe(asAgentId("agent_alpha"));
    expect(request.costCeilingMicroUsd).toBe(asMicroUsd(2_500_000));
    expect(runnerSpawnRequestToJsonValue(request)).toEqual(vector("claude-cli spawn request").json);
  });

  it.each([
    ["non-object", null, /runnerSpawnRequest: must be an object/],
    [
      "unknown key",
      { ...vector("claude-cli spawn request").json, extra: true },
      /runnerSpawnRequest\/extra: is not allowed/,
    ],
    [
      "missing kind",
      { ...vector("claude-cli spawn request").json, kind: undefined },
      /runnerSpawnRequest.kind: is required/,
    ],
    [
      "unsupported kind",
      { ...vector("claude-cli spawn request").json, kind: "other" },
      /unsupported RunnerKind/,
    ],
    [
      "invalid agent id",
      { ...vector("claude-cli spawn request").json, agentId: "Agent Alpha" },
      /runnerSpawnRequest.agentId: not an AgentId/,
    ],
    [
      "accessor prompt",
      recordWithAccessor(
        { ...vector("claude-cli spawn request").json, prompt: undefined },
        "prompt",
      ),
      /runnerSpawnRequest.prompt: must be a data property/,
    ],
    [
      "oversized prompt",
      {
        ...vector("claude-cli spawn request").json,
        prompt: "x".repeat(MAX_RUNNER_PROMPT_BYTES + 1),
      },
      /runnerSpawnRequest.prompt: exceeds/,
    ],
    [
      "oversized model",
      {
        ...vector("claude-cli spawn request").json,
        model: "x".repeat(MAX_RUNNER_MODEL_BYTES + 1),
      },
      /runnerSpawnRequest.model: exceeds/,
    ],
    [
      "oversized cwd",
      {
        ...vector("claude-cli spawn request").json,
        cwd: "x".repeat(MAX_RUNNER_CWD_BYTES + 1),
      },
      /runnerSpawnRequest.cwd: exceeds/,
    ],
    [
      "invalid task id",
      { ...vector("claude-cli spawn request").json, taskId: "" },
      /runnerSpawnRequest.taskId: not a TaskId/,
    ],
    [
      "invalid cost ceiling",
      { ...vector("claude-cli spawn request").json, costCeilingMicroUsd: 1.5 },
      /runnerSpawnRequest.costCeilingMicroUsd: not a MicroUsd/,
    ],
  ])("rejects malformed spawn requests: %s", (_name, json, error) => {
    expect(() => runnerSpawnRequestFromJson(json)).toThrow(error);
  });

  it("omits undefined optional fields when projecting to JSON", () => {
    const request = runnerSpawnRequestFromJson({
      kind: "codex-cli",
      agentId: "agent_alpha",
      credential: { version: 1, id: "cred_runner0123456789ABCDEFGHIJKLMN" },
      prompt: "Run tests",
    });

    expect(runnerSpawnRequestToJsonValue(request)).toEqual({
      kind: "codex-cli",
      agentId: "agent_alpha",
      credential: { version: 1, id: "cred_runner0123456789ABCDEFGHIJKLMN" },
      prompt: "Run tests",
    });
  });
});

describe("RunnerEvent codec", () => {
  it("round-trips every golden event vector", () => {
    for (const item of runnerVectors.vectors.filter((candidate) => candidate.kind === "event")) {
      const event = runnerEventFromJson(item.json);
      expect(runnerEventToJsonValue(event), item.name).toEqual(item.json);
    }
  });

  it.each([
    ["unknown kind", { kind: "other", runnerId, at }, /unsupported RunnerEvent kind/],
    ["bad runner id", { kind: "started", runnerId: "runner_short", at }, /not a RunnerId/],
    [
      "bad timestamp",
      { kind: "started", runnerId, at: "2026-05-08T18:00:00Z" },
      /ISO8601 UTC millisecond/,
    ],
    ["invalid date", { kind: "started", runnerId, at: "2026-99-99T18:00:00.000Z" }, /valid/],
    [
      "stdout unknown key",
      { kind: "stdout", runnerId, chunk: "ok", at, extra: true },
      /runnerEvent\/extra: is not allowed/,
    ],
    [
      "stdout oversized chunk",
      { kind: "stdout", runnerId, chunk: "x".repeat(MAX_RUNNER_STDIO_CHUNK_BYTES + 1), at },
      /runnerEvent.chunk: exceeds/,
    ],
    ["cost invalid entry", { kind: "cost", runnerId, entry: { bad: true }, at }, /not allowed/],
    ["receipt bad id", { kind: "receipt", runnerId, receiptId: "not-ulid", at }, /ReceiptId/],
    ["finished bad exit code", { kind: "finished", runnerId, exitCode: 256, at }, /0 to 255/],
    [
      "failed oversized error",
      { kind: "failed", runnerId, error: "x".repeat(MAX_RUNNER_ERROR_BYTES + 1), at },
      /runnerEvent.error: exceeds/,
    ],
  ])("rejects malformed runner events: %s", (_name, json, error) => {
    expect(() => runnerEventFromJson(json)).toThrow(error);
  });

  it("decodes discriminants to stable TypeScript branches", () => {
    const stdout = runnerEventFromJson({ kind: "stdout", runnerId, chunk: "hello", at });
    if (stdout.kind !== "stdout") throw new Error("expected stdout event");
    expect(stdout.chunk).toBe("hello");

    const finished = runnerEventFromJson({ kind: "finished", runnerId, exitCode: 0, at });
    if (finished.kind !== "finished") throw new Error("expected finished event");
    expect(finished.exitCode).toBe(0);

    const receipt = runnerEventFromJson({ kind: "receipt", runnerId, receiptId, at });
    if (receipt.kind !== "receipt") throw new Error("expected receipt event");
    expect(receipt.receiptId).toBe(receiptId);
  });
});
