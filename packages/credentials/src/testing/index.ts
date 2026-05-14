import { type AgentId, type BrokerIdentity, createBrokerIdentityForTesting } from "@wuphf/protocol";

export function forBrokerTests(input: {
  readonly agentId: AgentId;
  readonly revocationToken?: symbol | undefined;
}): BrokerIdentity {
  return createBrokerIdentityForTesting(input);
}
