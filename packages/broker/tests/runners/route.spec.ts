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

  afterEach(async () => {
    runner?.close();
    runner = null;
    if (broker !== null) {
      await broker.stop();
      broker = null;
    }
  });

  async function startBroker(): Promise<BrokerHandle> {
    broker = await createBroker({
      token,
      runners: {
        tokenAgentIds: new Map([[token, agentId]]),
        brokerIdentityForAgent: (id) => forBrokerTests({ agentId: id }),
        credentialStore: {
          write: async () => {
            throw new Error("not used by runner route tests");
          },
          read: async () => "secret",
          delete: async () => undefined,
        },
        costLedger: { record: async () => undefined },
        eventLog: { append: async () => undefined },
        spawnRunner: async (request) => {
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
    const handle = await startBroker();
    const res = await fetch(`${handle.url}/api/runners`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(runnerSpawnRequestToJsonValue(spawnRequest())),
    });

    expect(res.status).toBe(201);
    await expect(res.json()).resolves.toEqual({ runnerId });
    expect(runner?.agentId).toBe(agentId);
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

  it("streams runner events over SSE", async () => {
    const handle = await startBroker();
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
});
