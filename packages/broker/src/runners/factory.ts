import type { AgentRunner, Receipt, SpawnAgentRunner } from "@wuphf/agent-runners";
import { CredentialOwnershipMismatch, type CredentialStore } from "@wuphf/credentials";
import type {
  BrokerIdentity,
  CostLedgerEntry,
  CredentialScope,
  RunnerEvent,
  RunnerKind,
  RunnerSpawnRequest,
} from "@wuphf/protocol";
import {
  asCredentialScope,
  credentialHandleFromJson,
  credentialHandleToJson,
} from "@wuphf/protocol";

import type { ReceiptStore } from "../receipt-store.ts";

export interface RunnerCostLedger {
  record(entry: CostLedgerEntry): Promise<void>;
}

export interface RunnerEventLog {
  append(event: RunnerEvent): Promise<number>;
}

export interface AgentRunnerFactoryDeps {
  readonly credentialStore: CredentialStore;
  readonly costLedger: RunnerCostLedger;
  readonly receiptStore: ReceiptStore;
  readonly eventLog: RunnerEventLog;
  readonly spawnRunner: SpawnAgentRunner;
}

export async function createAgentRunnerForBroker(
  request: RunnerSpawnRequest,
  brokerIdentity: BrokerIdentity,
  deps: AgentRunnerFactoryDeps,
): Promise<AgentRunner> {
  const credentialScope =
    request.providerRoute?.credentialScope ?? credentialScopeForRunnerKind(request.kind);
  const credential = credentialHandleFromJson(request.credential, {
    broker: brokerIdentity,
    agentId: request.agentId,
    scope: credentialScope,
  });

  return deps.spawnRunner(request, {
    credential,
    secretReader: async (handle) => {
      const resolved = await deps.credentialStore.readWithOwnership({
        broker: brokerIdentity,
        handleId: credentialHandleToJson(handle).id,
        expectedAgentId: request.agentId,
        expectedScope: credentialScope,
      });
      if (resolved.agentId !== request.agentId || resolved.scope !== credentialScope) {
        throw new CredentialOwnershipMismatch();
      }
      return resolved.secret;
    },
    costLedger: deps.costLedger,
    receiptStore: {
      put: async (receipt: Receipt) => {
        const result = await deps.receiptStore.put(receipt);
        return { stored: !result.existed };
      },
    },
    eventLog: deps.eventLog,
  });
}

function credentialScopeForRunnerKind(kind: RunnerKind): CredentialScope {
  switch (kind) {
    case "claude-cli":
      return asCredentialScope("anthropic");
    case "codex-cli":
      return asCredentialScope("openai");
    case "openai-compat":
      return asCredentialScope("openai-compat");
  }
}
