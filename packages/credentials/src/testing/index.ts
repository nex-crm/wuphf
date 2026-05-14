import type { AgentId, BrokerIdentity } from "@wuphf/protocol";

import { createBrokerIdentityForTesting } from "../../../protocol/src/credential-handle.ts";

export function forBrokerTests(input: {
  readonly agentId: AgentId;
  readonly revocationToken?: symbol | undefined;
}): BrokerIdentity {
  return createBrokerIdentityForTesting(input);
}
