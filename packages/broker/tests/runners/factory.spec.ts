import { EndpointNotAllowed, ProviderKindMismatch } from "@wuphf/agent-runners";
import { createFakeAgentRunner } from "@wuphf/agent-runners/testing";
import { CredentialOwnershipMismatch } from "@wuphf/credentials";
import { forBrokerTests } from "@wuphf/credentials/testing";
import {
  type AgentId,
  asAgentId,
  asCredentialHandleId,
  asCredentialScope,
  asProviderKind,
  type CredentialScope,
  createCredentialHandle,
  credentialHandleToJson,
  type RunnerSpawnRequest,
} from "@wuphf/protocol";
import { describe, expect, it } from "vitest";

import { InMemoryReceiptStore } from "../../src/receipt-store.ts";
import {
  type AgentRunnerFactoryDeps,
  createAgentRunnerForBroker,
} from "../../src/runners/factory.ts";

const agentId = asAgentId("agent_alpha");
const otherAgentId = asAgentId("agent_beta");
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
    const reads: Array<{
      readonly handleId: string;
      readonly expectedAgentId: string;
      readonly expectedScope: string;
    }> = [];
    const runner = await createAgentRunnerForBroker(request, forBrokerTests({ agentId }), {
      credentialStore: {
        write: async () => credential,
        read: async () => {
          throw new Error("factory must use readWithOwnership");
        },
        readWithOwnership: async (input) => {
          reads.push({
            handleId: input.handleId,
            expectedAgentId: input.expectedAgentId,
            expectedScope: input.expectedScope,
          });
          return { secret: "secret", agentId, scope: asCredentialScope("anthropic") };
        },
        delete: async () => undefined,
      },
      costLedger: { record: async () => undefined },
      receiptStore: new InMemoryReceiptStore(),
      eventLog: { append: async () => 1 },
      spawnRunner: async (_request, deps) => {
        expect(deps.resolvedProviderKind).toBe(asProviderKind("anthropic"));
        await expect(deps.secretReader(deps.credential)).resolves.toBe("secret");
        return createFakeAgentRunner({
          kind: _request.kind,
          agentId: _request.agentId,
        });
      },
    });

    expect(runner.agentId).toBe(agentId);
    expect(reads).toEqual([
      {
        handleId: credentialHandleToJson(credential).id,
        expectedAgentId: agentId,
        expectedScope: "anthropic",
      },
    ]);
  });

  it("rejects a handle minted for another agent before exposing the secret", async () => {
    const betaCredential = createCredentialHandle({
      id: asCredentialHandleId("cred_beta0123456789ABCDEFGHIJKLMNO"),
      agentId: otherAgentId,
      scope: asCredentialScope("anthropic"),
    });
    const forgedRequest: RunnerSpawnRequest = {
      ...request,
      credential: credentialHandleToJson(betaCredential),
    };

    await expect(
      createAgentRunnerForBroker(forgedRequest, forBrokerTests({ agentId }), {
        ...depsForOwnership({
          actualAgentId: otherAgentId,
          actualScope: asCredentialScope("anthropic"),
          actualSecret: "beta-secret",
        }),
      }),
    ).rejects.toBeInstanceOf(CredentialOwnershipMismatch);
  });

  it("rejects a handle whose stored scope differs from the resolved runner scope", async () => {
    const openAiCredential = createCredentialHandle({
      id: asCredentialHandleId("cred_openai123456789ABCDEFGHIJKLM"),
      agentId,
      scope: asCredentialScope("openai"),
    });
    const wrongScopeRequest: RunnerSpawnRequest = {
      ...request,
      credential: credentialHandleToJson(openAiCredential),
    };

    await expect(
      createAgentRunnerForBroker(wrongScopeRequest, forBrokerTests({ agentId }), {
        ...depsForOwnership({
          actualAgentId: agentId,
          actualScope: asCredentialScope("openai"),
          actualSecret: "openai-secret",
        }),
      }),
    ).rejects.toBeInstanceOf(CredentialOwnershipMismatch);
  });

  it("uses providerRoute credential scope when present", async () => {
    const openAiCredential = createCredentialHandle({
      id: asCredentialHandleId("cred_route0123456789ABCDEFGHIJKLM"),
      agentId,
      scope: asCredentialScope("openai"),
    });
    const routedRequest: RunnerSpawnRequest = {
      ...request,
      credential: credentialHandleToJson(openAiCredential),
      providerRoute: {
        credentialScope: asCredentialScope("openai"),
        providerKind: asProviderKind("openai"),
      },
    };
    const runner = await createAgentRunnerForBroker(routedRequest, forBrokerTests({ agentId }), {
      ...depsForOwnership({
        actualAgentId: agentId,
        actualScope: asCredentialScope("openai"),
        actualSecret: "openai-secret",
        expectedProviderKind: asProviderKind("openai"),
      }),
    });

    expect(runner.agentId).toBe(agentId);
  });

  it("rejects providerRoute providerKind that does not match the credential scope", async () => {
    const openAiCredential = createCredentialHandle({
      id: asCredentialHandleId("cred_route0123456789ABCDEFGHIJKLM"),
      agentId,
      scope: asCredentialScope("openai"),
    });
    const mismatchedRequest: RunnerSpawnRequest = {
      ...request,
      credential: credentialHandleToJson(openAiCredential),
      providerRoute: {
        credentialScope: asCredentialScope("openai"),
        providerKind: asProviderKind("anthropic"),
      },
    };

    await expect(
      createAgentRunnerForBroker(mismatchedRequest, forBrokerTests({ agentId }), {
        ...depsForOwnership({
          actualAgentId: agentId,
          actualScope: asCredentialScope("openai"),
          actualSecret: "openai-secret",
        }),
      }),
    ).rejects.toBeInstanceOf(ProviderKindMismatch);
  });

  it.each([
    ["exact", "https://api.openai.com/v1/chat/completions", ["https://api.openai.com"]],
    [
      "glob",
      "https://eastus.openai.azure.com/openai/deployments/demo/chat/completions",
      ["https://*.openai.azure.com"],
    ],
  ])("allows openai-compatible endpoints that match the %s allowlist", async (_name, endpoint, endpointAllowlist) => {
    const runner = await createAgentRunnerForBroker(
      openAICompatRequest(endpoint),
      forBrokerTests({ agentId }),
      {
        ...depsForOwnership({
          actualAgentId: agentId,
          actualScope: asCredentialScope("openai-compat"),
          actualSecret: "openai-compatible-secret",
          expectedProviderKind: asProviderKind("openai-compat"),
        }),
        endpointAllowlist,
      },
    );

    expect(runner.agentId).toBe(agentId);
  });

  it.each([
    ["not allowlisted", "https://evil.test/v1/chat/completions", ["https://api.openai.com"]],
    ["loopback wildcard", "http://127.0.0.1:8080/v1/chat/completions", ["http://*"]],
    ["file scheme", "file:///etc/passwd", ["file://*"]],
  ])("rejects openai-compatible endpoints: %s", async (_name, endpoint, endpointAllowlist) => {
    await expect(
      createAgentRunnerForBroker(openAICompatRequest(endpoint), forBrokerTests({ agentId }), {
        ...depsForOwnership({
          actualAgentId: agentId,
          actualScope: asCredentialScope("openai-compat"),
          actualSecret: "openai-compatible-secret",
        }),
        endpointAllowlist,
      }),
    ).rejects.toBeInstanceOf(EndpointNotAllowed);
  });
});

function openAICompatRequest(endpoint: string): RunnerSpawnRequest {
  return {
    kind: "openai-compat",
    agentId,
    credential: {
      version: 1,
      id: asCredentialHandleId("cred_openai123456789ABCDEFGHIJKLM"),
    },
    options: {
      kind: "openai-compat",
      endpoint,
    },
    prompt: "run",
  };
}

function depsForOwnership(input: {
  readonly actualAgentId: AgentId;
  readonly actualScope: CredentialScope;
  readonly actualSecret: string;
  readonly expectedProviderKind?: ReturnType<typeof asProviderKind> | undefined;
}): AgentRunnerFactoryDeps {
  return {
    credentialStore: {
      write: async () => credential,
      read: async () => {
        throw new Error("factory must use readWithOwnership");
      },
      readWithOwnership: async (requestInput) => {
        if (
          requestInput.expectedAgentId !== input.actualAgentId ||
          requestInput.expectedScope !== input.actualScope
        ) {
          throw new CredentialOwnershipMismatch();
        }
        return {
          secret: input.actualSecret,
          agentId: input.actualAgentId,
          scope: input.actualScope,
        };
      },
      delete: async () => undefined,
    },
    costLedger: { record: async () => undefined },
    receiptStore: new InMemoryReceiptStore(),
    eventLog: { append: async () => 1 },
    spawnRunner: async (_request: RunnerSpawnRequest, deps) => {
      expect(deps.resolvedProviderKind).toBe(
        input.expectedProviderKind ?? asProviderKind("anthropic"),
      );
      await deps.secretReader(deps.credential);
      return createFakeAgentRunner({
        kind: _request.kind,
        agentId: _request.agentId,
      });
    },
  };
}
