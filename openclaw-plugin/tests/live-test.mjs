/**
 * Live integration test — hits the mock Nex server directly
 * through the plugin's NexClient, capture filter, and context formatter.
 *
 * Usage: node tests/live-test.mjs
 * Requires: mock-nex-server.mjs running on port 31999
 */

import { NexClient } from "../dist/nex-client.js";
import { RateLimiter } from "../dist/rate-limiter.js";
import { SessionStore } from "../dist/session-store.js";
import { formatNexContext, stripNexContext } from "../dist/context-format.js";
import { captureFilter, resetDedupCache } from "../dist/capture-filter.js";
import { parseConfig } from "../dist/config.js";

const BASE_URL = "http://localhost:31999";
const API_KEY = "sk-test-mock-key";

let passed = 0;
let failed = 0;

function assert(condition, name) {
  if (condition) {
    console.log(`  ✓ ${name}`);
    passed++;
  } else {
    console.log(`  ✗ ${name}`);
    failed++;
  }
}

async function main() {
  const client = new NexClient(API_KEY, BASE_URL);
  const limiter = new RateLimiter();
  const sessions = new SessionStore();
  const config = parseConfig({ apiKey: API_KEY, baseUrl: BASE_URL });

  console.log("\n=== Test 1: Health Check ===");
  const healthy = await client.healthCheck();
  assert(healthy, "healthCheck returns true");

  console.log("\n=== Test 2: Ingest via /text ===");
  const ingest1 = await client.ingest("My favorite color is blue and I love TypeScript");
  assert(ingest1.artifact_id, `ingest returned artifact_id: ${ingest1.artifact_id}`);

  const ingest2 = await client.ingest("Sarah Johnson is VP of Engineering at Acme Corp", "meeting-notes");
  assert(ingest2.artifact_id, `ingest with context returned artifact_id: ${ingest2.artifact_id}`);

  console.log("\n=== Test 3: Recall via /ask ===");
  const ask1 = await client.ask("What is my favorite color?");
  assert(ask1.answer.includes("blue"), `recall found 'blue': "${ask1.answer.slice(0, 80)}..."`);
  assert(ask1.session_id, `recall returned session_id: ${ask1.session_id}`);

  console.log("\n=== Test 4: Session Continuity ===");
  sessions.set("session-abc", ask1.session_id);
  const storedSession = sessions.get("session-abc");
  assert(storedSession === ask1.session_id, `session store persists session_id`);

  const ask2 = await client.ask("Tell me about Sarah", storedSession);
  assert(ask2.answer.includes("Sarah Johnson"), `recall with session found 'Sarah Johnson'`);
  assert(ask2.entity_references.length > 0, `recall returned ${ask2.entity_references.length} entity references`);
  assert(ask2.entity_references[0].name === "Sarah Johnson", `entity ref name: ${ask2.entity_references[0].name}`);

  console.log("\n=== Test 5: Context Formatting ===");
  const context = formatNexContext({
    answer: ask2.answer,
    entityCount: ask2.entity_references.length,
    sessionId: ask2.session_id,
  });
  assert(context.includes("<nex-context>"), "formatted context has open tag");
  assert(context.includes("</nex-context>"), "formatted context has close tag");
  assert(context.includes("related entities found"), "formatted context shows entity count");

  const stripped = stripNexContext(`User said: hello ${context} and goodbye`);
  assert(!stripped.includes("<nex-context>"), "stripNexContext removes injected blocks");
  assert(stripped.includes("hello"), "stripNexContext preserves surrounding text");

  console.log("\n=== Test 6: Capture Filter ===");
  resetDedupCache();

  const captureResult = captureFilter(
    [
      { role: "user", content: "Remember that my favorite color is blue" },
      { role: "assistant", content: "I'll remember that your favorite color is blue!" },
    ],
    config,
  );
  assert(!captureResult.skipped, "capture filter accepts normal conversation");
  assert(captureResult.text.includes("favorite color"), "captured text contains conversation");

  const skipShort = captureFilter([{ role: "user", content: "hi" }], config);
  assert(skipShort.skipped, "capture filter skips short messages");

  const skipCommand = captureFilter([{ role: "user", content: "/help me" }], config);
  assert(skipCommand.skipped, "capture filter skips slash commands");

  const skipSystem = captureFilter(
    [{ role: "user", content: "system event content here" }],
    config,
    { messageProvider: "cron-event" },
  );
  assert(skipSystem.skipped, "capture filter skips cron-event provider");

  const skipFailed = captureFilter(
    [{ role: "user", content: "this should be skipped entirely" }],
    config,
    { success: false },
  );
  assert(skipFailed.skipped, "capture filter skips failed agent runs");

  console.log("\n=== Test 7: Rate-Limited Ingest ===");
  let ingestCount = 0;
  const promises = [];
  for (let i = 0; i < 5; i++) {
    promises.push(
      limiter.enqueue(async () => {
        await client.ingest(`Rate limit test message ${i}`);
        ingestCount++;
      })
    );
  }
  await Promise.all(promises);
  assert(ingestCount === 5, `all 5 rate-limited ingests completed`);

  console.log("\n=== Test 8: Auth Error ===");
  const badClient = new NexClient("sk-bad-key", BASE_URL);
  try {
    await badClient.ask("should fail");
    assert(false, "bad key should throw");
  } catch (err) {
    assert(err.name === "NexAuthError", `bad key throws NexAuthError: ${err.message}`);
  }

  console.log("\n=== Test 9: No Match Recall ===");
  const noMatch = await client.ask("quantum physics theories");
  assert(noMatch.answer.includes("don't have specific information"), "no-match returns graceful message");
  assert(noMatch.entity_references.length === 0, "no-match returns empty entity refs");

  // Cleanup
  limiter.destroy();

  console.log(`\n${"=".repeat(40)}`);
  console.log(`Results: ${passed} passed, ${failed} failed`);
  console.log(`${"=".repeat(40)}\n`);

  process.exit(failed > 0 ? 1 : 0);
}

main().catch((err) => {
  console.error("Test crashed:", err);
  process.exit(1);
});
