import { createFakeAgentRunner } from "@wuphf/agent-runners/testing";
import { forBrokerTests } from "@wuphf/credentials/testing";
import {
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  createCredentialHandle,
  credentialHandleToJson,
  type RunnerSpawnRequest,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { InMemoryReceiptStore } from "../../src/receipt-store.ts";
import { createAgentRunnerForBroker } from "../../src/runners/factory.ts";

const agentId = asAgentId("agent_alpha");
const credential = createCredentialHandle({
  id: asCredentialHandleId("cred_runner0123456789ABCDEFGHIJKLMN"),
  agentId,
  scope: asCredentialScope("anthropic"),
});
const request: RunnerSpawnRequest = {
  kind: "claude-cli",
  agentId,
  credential: credentialHandleToJson(credential),
  prompt: "run",
};

describe("createAgentRunnerForBroker", () => {
  it("injects a broker-scoped secret reader without giving runners BrokerIdentity", async () => {
    const reads: Array<{ readonly handleId: string; readonly agentId: string }> = [];
    const runner = await createAgentRunnerForBroker(request, forBrokerTests({ agentId }), {
      credentialStore: {
        write: async () => credential,
        read: async (input) => {
          reads.push({ handleId: input.handleId, agentId: input.agentId });
          return "secret";
        },
        delete: async () => undefined,
      },
      costLedger: { record: async () => undefined },
      receiptStore: new InMemoryReceiptStore(),
      eventLog: { append: async () => undefined },
      spawnRunner: async (_request, deps) => {
        await expect(deps.secretReader(deps.credential)).resolves.toBe("secret");
        return createFakeAgentRunner({
          kind: _request.kind,
          agentId: _request.agentId,
        });
      },
    });

    expect(runner.agentId).toBe(agentId);
    expect(reads).toEqual([{ handleId: credentialHandleToJson(credential).id, agentId }]);
  });
});
