// Branch 10 — per-agent provider routing.
//
// Shared interface between the SQLite-backed store (implemented in `store.ts`)
// and the HTTP route + factory consumers (implemented in `route.ts` and
// `packages/broker/src/runners/factory.ts`).
//
// The store is the only source of truth for per-agent routing state.
// `RunnerSpawnRequest` arriving without an inline `providerRoute` triggers a
// store lookup; a hit fills in `providerRoute` before the request reaches
// `@wuphf/agent-runners`. A miss falls back to the default route derived from
// `runnerKindToProviderKind(request.kind)` — the existing v0 behavior — so
// configuring no routes for an agent does not break it.

import type { AgentId, AgentProviderRouting, RunnerKind } from "@wuphf/protocol";

export interface AgentProviderRoutingStore {
  /**
   * Return the routing configuration for an agent. If no routes are stored,
   * resolves to a config with an empty `routes` array (callers should treat
   * this as "use defaults," not "agent not found"). The agentId in the
   * returned object always equals the requested agentId.
   */
  get(agentId: AgentId): Promise<AgentProviderRouting>;

  /**
   * Look up the route for a single (agent, kind) pair. Returns `null` when no
   * entry exists for that pair. The factory uses this on every spawn — keep
   * the implementation O(1) per call.
   */
  getEntry(
    agentId: AgentId,
    kind: RunnerKind,
  ): Promise<{
    readonly credentialScope: import("@wuphf/protocol").CredentialScope;
    readonly providerKind: import("@wuphf/protocol").ProviderKind;
  } | null>;

  /**
   * Atomically replace all routes for the agent. The implementation MUST run
   * the delete + inserts inside a single transaction. Passing an empty
   * `routes` array clears all stored routes for the agent and is the canonical
   * "reset to defaults" call.
   */
  put(config: AgentProviderRouting): Promise<void>;

  /** Release any held resources (DB handles, prepared statements). */
  close(): void;
}
