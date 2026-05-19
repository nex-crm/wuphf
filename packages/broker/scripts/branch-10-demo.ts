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
import { asAgentId, asApiToken, threadSpecContentHash } from "@wuphf/protocol";
import BetterSqlite3 from "better-sqlite3";

import { createAgentProviderRoutingStore } from "../src/agent-provider-routing/index.ts";
import { createEventLog, runMigrations } from "../src/event-log/index.ts";
import { createBroker } from "../src/index.ts";
import { SqliteReceiptStore } from "../src/sqlite-receipt-store.ts";
import { createThreadSubsystem } from "../src/threads/index.ts";

const tmp = mkdtempSync(join(tmpdir(), "wuphf-branch-10-"));
const dbPath = join(tmp, "broker.db");

console.log(`[demo] SQLite DB: ${dbPath}`);
const db = new BetterSqlite3(dbPath);
runMigrations(db);
const eventLog = createEventLog(db);
const agentProviderRoutingStore = createAgentProviderRoutingStore(db);
const receiptStore = SqliteReceiptStore.fromDatabase(db, eventLog);
const threads = createThreadSubsystem(db, eventLog, receiptStore);

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
  threads,
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

const createRevisionId = "01BRZ3NDEKTSV4RRFFQ69G5FB0";
const threadId = createRevisionId;
const editRevisionId = "01CRZ3NDEKTSV4RRFFQ69G5FC0";
const initialThreadContent = { goal: "manual thread smoke", version: 1 };
const editedThreadContent = { goal: "manual thread smoke", version: 2 };
const threadCreateBody = JSON.stringify({
  title: "Manual thread smoke",
  specContent: initialThreadContent,
  externalRefs: { source_urls: ["https://example.com/manual-thread"], entity_ids: ["demo:thread"] },
  idempotencyKey: createRevisionId,
});
const threadSpecEditBody = JSON.stringify({
  baseRevisionId: createRevisionId,
  baseContentHash: threadSpecContentHash(initialThreadContent),
  content: editedThreadContent,
  idempotencyKey: editRevisionId,
});
const staleThreadSpecEditBody = JSON.stringify({
  baseRevisionId: createRevisionId,
  baseContentHash: threadSpecContentHash(initialThreadContent),
  content: { goal: "stale edit", version: 3 },
  idempotencyKey: "01DRZ3NDEKTSV4RRFFQ69G5FD0",
});
const closeThreadBody = JSON.stringify({
  fromStatus: "open",
  toStatus: "closed",
  idempotencyKey: "01ERZ3NDEKTSV4RRFFQ69G5FE0",
});
const outOfTerminalThreadBody = JSON.stringify({
  fromStatus: "closed",
  toStatus: "merged",
  idempotencyKey: "01FRZ3NDEKTSV4RRFFQ69G5FF0",
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
console.log("# 8. Create a thread (appends thread.created + initial thread.spec_edited)");
console.log(
  `curl -s -X POST -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${threadCreateBody}' \\\n     "${broker.url}/api/v1/threads"; echo`,
);
console.log("");
console.log("# 9. Duplicate create idempotency key → replayed response, no duplicate event");
console.log(
  `curl -s -i -X POST -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${threadCreateBody}' \\\n     "${broker.url}/api/v1/threads" | head -6`,
);
console.log("");
console.log("# 10. Accepted spec edit with matching baseRevisionId + baseContentHash");
console.log(
  `curl -s -X PATCH -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${threadSpecEditBody}' \\\n     "${broker.url}/api/v1/threads/${threadId}/spec"; echo`,
);
console.log("");
console.log("# 11. Stale-base spec edit → 409");
console.log(
  `curl -s -i -X PATCH -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${staleThreadSpecEditBody}' \\\n     "${broker.url}/api/v1/threads/${threadId}/spec" | head -1`,
);
console.log("");
console.log("# 12. Close thread, then prove out-of-terminal transition returns 422");
console.log(
  `curl -s -X PATCH -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${closeThreadBody}' \\\n     "${broker.url}/api/v1/threads/${threadId}/status"; echo`,
);
console.log(
  `curl -s -i -X PATCH -H "Authorization: Bearer ${broker.token}" \\\n     -H "Content-Type: application/json" \\\n     -d '${outOfTerminalThreadBody}' \\\n     "${broker.url}/api/v1/threads/${threadId}/status" | head -1`,
);
console.log("");
console.log("# 13. Read folded projection");
console.log(
  `curl -s -H "Authorization: Bearer ${broker.token}" \\\n     "${broker.url}/api/v1/threads/${threadId}"; echo`,
);
console.log("");
console.log("Ctrl-C to stop. Temp DB will be cleaned up on shutdown.");
