import { mkdir, mkdtemp, realpath, rm, symlink } from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { createFakeAgentRunner, type FakeAgentRunner } from "@wuphf/agent-runners/testing";
import { forBrokerTests } from "@wuphf/credentials/testing";
import {
  asAgentId,
  asApiToken,
  asCredentialHandleId,
  asRunnerId,
  type RunnerSpawnRequest,
  runnerSpawnRequestToJsonValue,
} from "@wuphf/protocol";
import { afterEach, describe, expect, it } from "vitest";

import { type BrokerHandle, createBroker } from "../../src/index.ts";

const token = asApiToken("test-token-with-enough-entropy-AAAAAAAAA");
const agentId = asAgentId("agent_alpha");
const otherAgentId = asAgentId("agent_beta");
const runnerId = asRunnerId("run_0123456789ABCDEFGHIJKLMNOPQRSTUV");

function spawnRequest(agent = agentId): RunnerSpawnRequest {
  return {
    kind: "claude-cli",
    agentId: agent,
    credential: { version: 1, id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN") },
    prompt: "Say hello",
  };
}

describe("runner routes", () => {
  let broker: BrokerHandle | null = null;
  let runner: FakeAgentRunner | null = null;
  let spawnedRequest: RunnerSpawnRequest | null = null;
  let tempDirs: string[] = [];

  afterEach(async () => {
    runner?.close();
    runner = null;
    spawnedRequest = null;
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
    await Promise.all(tempDirs.map((dir) => rm(dir, { force: true, recursive: true })));
    tempDirs = [];
  });

  async function startBroker(workspaceRoot?: string): Promise<BrokerHandle> {
    broker = await createBroker({
      token,
      workspaceRoot,
      runners: {
        tokenAgentIds: new Map([[token, agentId]]),
        brokerIdentityForAgent: (id) => forBrokerTests({ agentId: id }),
        credentialStore: {
          write: async () => {
            throw new Error("not used by runner route tests");
          },
          read: async () => {
            throw new Error("runner route must use ownership-aware reads");
          },
          readWithOwnership: async (input) => ({
            secret: "secret",
            agentId: input.expectedAgentId,
            scope: input.expectedScope,
          }),
          delete: async () => undefined,
        },
        costLedger: { record: async () => undefined },
        eventLog: { append: async () => undefined },
        spawnRunner: async (request) => {
          spawnedRequest = request;
          runner = createFakeAgentRunner({
            id: runnerId,
            kind: request.kind,
            agentId: request.agentId,
          });
          return runner;
        },
      },
    });
    return broker;
  }

  it("spawns a runner through the protocol request envelope", async () => {
    const workspaceRoot = await makeWorkspaceRoot();
    const projectDir = path.join(workspaceRoot, agentId, "project");
    await mkdir(projectDir, { recursive: true });
    const handle = await startBroker(workspaceRoot);
    const res = await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue({ ...spawnRequest(), cwd: "project" })),
    });

    expect(res.status).toBe(201);
    await expect(res.json()).resolves.toEqual({ runnerId });
    expect(runner?.agentId).toBe(agentId);
    expect(spawnedRequest?.cwd).toBe(await realpath(projectDir));
  });

  it("rejects a spawn request whose agentId does not match the bearer map", async () => {
    const handle = await startBroker();
    const res = await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue(spawnRequest(otherAgentId))),
    });

    expect(res.status).toBe(403);
    await expect(res.text()).resolves.toBe("runner_agent_mismatch");
  });

  it.each([
    ["relative sibling traversal", "../agent_beta/secrets"],
    ["absolute system directory", "/etc"],
  ])("rejects cwd outside the caller workspace: %s", async (_name, cwd) => {
    const workspaceRoot = await makeWorkspaceRoot();
    await mkdir(path.join(workspaceRoot, otherAgentId, "secrets"), { recursive: true });
    const handle = await startBroker(workspaceRoot);
    const res = await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue({ ...spawnRequest(), cwd })),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toMatchObject({ error: "cwd_out_of_workspace" });
    expect(runner).toBeNull();
  });

  it("rejects a cwd symlink that resolves outside the caller workspace", async () => {
    const workspaceRoot = await makeWorkspaceRoot();
    const outsideDir = await mkdtemp(path.join(os.tmpdir(), "wuphf-runner-outside-"));
    tempDirs.push(outsideDir);
    const symlinkPath = path.join(workspaceRoot, agentId, "escape");
    await mkdir(path.dirname(symlinkPath), { recursive: true });
    await symlink(outsideDir, symlinkPath, "dir");
    const handle = await startBroker(workspaceRoot);
    const res = await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue({ ...spawnRequest(), cwd: "escape" })),
    });

    expect(res.status).toBe(400);
    await expect(res.json()).resolves.toMatchObject({ error: "cwd_out_of_workspace" });
    expect(runner).toBeNull();
  });

  it("streams runner events over SSE", async () => {
    const workspaceRoot = await makeWorkspaceRoot();
    const handle = await startBroker(workspaceRoot);
    await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue(spawnRequest())),
    });
    if (runner === null) throw new Error("runner was not created");

    const res = await fetch(`${handle.url}/api/runners/${encodeURIComponent(runnerId)}/events`, {
      headers: {
        Authorization: `Bearer ${token}`,
        Accept: "text/event-stream",
      },
    });
    expect(res.status).toBe(200);
    const reader = res.body?.getReader();
    if (reader === undefined) throw new Error("missing SSE body");

    await runner.emit({
      kind: "started",
      runnerId,
      at: "2026-05-08T18:00:00.000Z",
    });
    await runner.emit({
      kind: "finished",
      runnerId,
      exitCode: 0,
      at: "2026-05-08T18:00:01.000Z",
    });
    runner.close();

    let text = "";
    while (!text.includes("event: finished")) {
      const chunk = await reader.read();
      if (chunk.done) break;
      text += new TextDecoder().decode(chunk.value);
    }

    expect(text).toContain("event: started");
    expect(text).toContain('"runnerId":"run_0123456789ABCDEFGHIJKLMNOPQRSTUV"');
    expect(text).toContain("event: finished");
  });

  async function makeWorkspaceRoot(): Promise<string> {
    const workspaceRoot = await mkdtemp(path.join(os.tmpdir(), "wuphf-runner-workspaces-"));
    tempDirs.push(workspaceRoot);
    await mkdir(path.join(workspaceRoot, agentId), { recursive: true });
    return workspaceRoot;
  }
});
