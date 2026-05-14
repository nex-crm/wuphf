// Manual-verification harness for branch 10 (per-agent provider routing).
//
// Boots a broker on a random loopback port with the SQLite-backed
// AgentProviderRoutingStore wired in, then prints the URL, bearer token,
// and a ready-to-paste curl recipe.
//
// Run from the broker package:
//   cd packages/broker
//   bunx tsx scripts/branch-10-demo.ts

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { forBrokerTests } from "@wuphf/credentials/testing";
import { asAgentId, asApiToken } from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";

import { createAgentProviderRoutingStore } from "../src/agent-provider-routing/index.ts";
import { runMigrations } from "../src/event-log/index.ts";
import { createBroker } from "../src/index.ts";

const tmp = mkdtempSync(join(tmpdir(), "wuphf-branch-10-"));
const dbPath = join(tmp, "broker.db");

console.log(`[demo] SQLite DB: ${dbPath}`);
const db = new BetterSqlite3(dbPath);
runMigrations(db);
const agentProviderRoutingStore = createAgentProviderRoutingStore(db);

const token = asApiToken("demo-token-with-enough-entropy-AAAAAAAAA");
const agentId = asAgentId("agent_alice_001");

const broker = await createBroker({
  token,
  runners: {
    tokenAgentIds: new Map([[token, agentId]]),
    brokerIdentityForAgent: (id) => forBrokerTests({ agentId: id }),
    credentialStore: {
      write: async () => {
        throw new Error("demo does not spawn runners; no credential write");
      },
      read: async () => {
        throw new Error("demo does not spawn runners; no credential read");
      },
      readWithOwnership: async () => {
        throw new Error("demo does not spawn runners; no credential read");
      },
      delete: async () => undefined,
    },
    costLedger: { record: async () => undefined },
    eventLog: { append: async () => 1 },
    spawnRunner: async () => {
      throw new Error("demo does not spawn runners");
    },
    agentProviderRoutingStore,
  },
});

let cleanupStarted = false;
const cleanup = async () => {
  if (cleanupStarted) return;
  cleanupStarted = true;
  try {
    await broker.stop();
  } catch (err) {
    console.error("[demo] Error stopping broker:", err);
  }
  try {
    db.close();
  } catch (err) {
    console.error("[demo] Error closing DB:", err);
  }
  rmSync(tmp, { recursive: true, force: true });
  process.exit(0);
};
process.on("SIGINT", cleanup);
process.on("SIGTERM", cleanup);

const writeBody = JSON.stringify({
  agentId,
  routes: [
    { kind: "claude-cli", credentialScope: "anthropic", providerKind: "anthropic" },
    { kind: "codex-cli", credentialScope: "openai", providerKind: "openai" },
  ],
});

console.log("");
console.log("=== Broker up. ===");
console.log(`URL:    ${broker.url}`);
console.log(`Token:  ${broker.token}`);
console.log("");
console.log("Copy-paste these curl commands to verify branch 10 behavior:");
console.log("");
console.log("# 1. Empty config for a fresh agent → { routes: [] }");
console.log(
  `curl -s -H "Authorization: Bearer ${broker.token}" \\\n     "${broker.url}/api/agents/${agentId}/provider-routing"; echo`,
);
console.log("");
console.log("# 2. Set per-agent routing (Claude→Anthropic, Codex→OpenAI)");
console.log(
  `curl -s -X PUT -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${writeBody}' \\\n     "${broker.url}/api/agents/${agentId}/provider-routing"; echo`,
);
console.log("");
console.log("# 3. Read back → routes appear in deterministic kind order");
console.log(
  `curl -s -H "Authorization: Bearer ${broker.token}" \\\n     "${broker.url}/api/agents/${agentId}/provider-routing"; echo`,
);
console.log("");
console.log("# 4. Confused-deputy guard: body.agentId != URL.agentId → 400");
console.log(
  `curl -s -i -X PUT -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '{"agentId":"agent_mallory","routes":[]}' \\\n     "${broker.url}/api/agents/${agentId}/provider-routing" | head -1`,
);
console.log("");
console.log("# 5. Wrong method → 405 with Allow header");
console.log(
  `curl -s -i -X POST -H "Authorization: Bearer ${broker.token}" \\\n     "${broker.url}/api/agents/${agentId}/provider-routing" | head -3`,
);
console.log("");
console.log("# 6. Missing bearer → 401");
console.log(`curl -s -i "${broker.url}/api/agents/${agentId}/provider-routing" | head -1`);
console.log("");
console.log("# 7. Clear routes (idempotent reset to defaults)");
console.log(
  `curl -s -X PUT -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '{"agentId":"${agentId}","routes":[]}' \\\n     "${broker.url}/api/agents/${agentId}/provider-routing"; echo`,
);
console.log("");
console.log("Ctrl-C to stop. Temp DB will be cleaned up on shutdown.");
