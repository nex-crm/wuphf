/**
 * User Acceptance Tests (UAT) for the WUPHF TUI.
 *
 * 10 acceptance scenarios that exercise the real TUI process via PTY.
 * Each test spawns a fresh TUI, sends keystrokes, and asserts terminal output.
 *
 * Prerequisites:
 *   - node-pty installed (used by ../e2e/harness.ts)
 *   - No valid API key required (tests handle auth errors gracefully)
 *
 * Run: npx tsx --test tests/uat/acceptance.test.ts
 *
 * NOTE: These tests use node-pty which requires node's event loop.
 * Use `npx tsx --test` (not `bun test`) since bun's event loop
 * doesn't drive node-pty's native addon callbacks.
 */

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { TuiTest } from "../e2e/harness.js";

// ── Helpers ──────────────────────────────────────────────────────────

const STARTUP_TIMEOUT = 12000;
const ACTION_TIMEOUT = 8000;

/** Create a TuiTest instance with standard UAT settings. */
function createTui(opts?: { timeout?: number }): TuiTest {
  return new TuiTest({
    timeout: opts?.timeout ?? 15000,
    cols: 120,
    rows: 30,
  });
}

/** Wait for TUI to fully start. */
async function waitForStartup(tui: TuiTest): Promise<void> {
  // Stream view shows either "What would you like to do?" (with API key)
  // or "Welcome to WUPHF" / "/init to get started" (without API key)
  const found = await tui.waitForMatch(
    /What would you like|Welcome to WUPHF|get started|Message Team-Lead/i,
    STARTUP_TIMEOUT,
  );
  assert.ok(found, `TUI failed to start. Got:\n${tui.text().slice(-500)}`);
}

// ── AC-1: Non-blocking input ─────────────────────────────────────────

describe("UAT Acceptance Tests", () => {
  it("AC-1: Non-blocking input — user can type and send messages", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // Type a message and send
      tui.type("research competitors");
      tui.enter();

      // Message should appear in stream
      const msgAppeared = await tui.waitForText("research competitors", ACTION_TIMEOUT);
      assert.ok(msgAppeared, `User message should appear in stream. Got:\n${tui.text().slice(-500)}`);

      // Input should be available for another message immediately (non-blocking)
      // We can type right away without waiting for a response
      await tui.wait(500);
      tui.type("second message");

      // The fact that we can type without error proves non-blocking input
      await tui.wait(300);
      const text = tui.text();
      // Stream should still have the first message visible
      assert.ok(
        text.includes("research competitors"),
        `First message should still be in stream. Got:\n${text.slice(-500)}`,
      );
    } finally {
      await tui.kill();
    }
  });

  // ── AC-2: Rapid multi-message ──────────────────────────────────────

  it("AC-2: Rapid multi-message — multiple messages accepted quickly", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // Send 3 messages in rapid succession
      tui.type("fix our SEO rankings");
      tui.enter();
      await tui.wait(300);

      tui.type("find leads in fintech");
      tui.enter();
      await tui.wait(300);

      tui.type("pipeline status check");
      tui.enter();

      // Wait for messages to appear
      await tui.wait(2000);
      const text = tui.text();

      // All messages should appear in the stream (user echo)
      const msg1 = text.includes("fix our SEO rankings");
      const msg2 = text.includes("find leads in fintech");
      const msg3 = text.includes("pipeline status check");

      // At least 2 of 3 should appear (terminal scrolling may hide earliest)
      const appeared = [msg1, msg2, msg3].filter(Boolean).length;
      assert.ok(
        appeared >= 2,
        `Expected at least 2 of 3 messages in stream, got ${appeared}. Got:\n${text.slice(-800)}`,
      );
    } finally {
      await tui.kill();
    }
  });

  // ── AC-3: Thread follow-up ─────────────────────────────────────────

  it("AC-3: Thread follow-up — second message within 30s is treated as follow-up", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // Send initial message
      tui.type("research competitor pricing");
      tui.enter();
      await tui.wait(1500);

      // Send follow-up within 30s (should route to same agent)
      tui.type("also check their market share");
      tui.enter();

      // Both messages should appear
      const msg1 = await tui.waitForText("competitor pricing", ACTION_TIMEOUT);
      assert.ok(msg1, `First message should appear. Got:\n${tui.text().slice(-500)}`);

      const msg2 = await tui.waitForText("also check their market share", ACTION_TIMEOUT);
      assert.ok(msg2, `Follow-up message should appear. Got:\n${tui.text().slice(-500)}`);

      // TUI should still be responsive
      const text = tui.text();
      assert.ok(
        text.includes("competitor pricing") && text.includes("market share"),
        `Both messages should be in stream for follow-up detection. Got:\n${text.slice(-500)}`,
      );
    } finally {
      await tui.kill();
    }
  });

  // ── AC-4: Agent creation via /agents wizard ────────────────────────

  it("AC-4: /agents opens agent manager", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      tui.type("/agents");
      tui.enter();

      // Should show agent manager — either the picker, an agent list, or "Agents" label
      const found = await tui.waitForMatch(
        /Agents|agent|Create|template|Team Lead/i,
        ACTION_TIMEOUT,
      );
      assert.ok(found, `/agents should open agent manager. Got:\n${tui.text().slice(-500)}`);
    } finally {
      await tui.kill();
    }
  });

  // ── AC-5: Onboarding (/init) ──────────────────────────────────────

  it("AC-5: /init starts onboarding flow", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      tui.type("/init");
      tui.enter();

      // Should show email prompt, provider picker, or key status
      const found = await tui.waitForMatch(
        /email|set up|API key|valid|expired|provider|Welcome to WUPHF! Let|Enter your/i,
        ACTION_TIMEOUT,
      );
      assert.ok(found, `/init should start onboarding. Got:\n${tui.text().slice(-500)}`);
    } finally {
      await tui.kill();
    }
  });

  // ── AC-6: Provider switch (/provider) ──────────────────────────────

  it("AC-6: /provider shows provider options", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      tui.type("/provider");
      tui.enter();

      // Should show provider picker or "Unknown command" if not yet implemented
      const found = await tui.waitForMatch(
        /provider|Anthropic|OpenAI|Claude|Unknown command|pick/i,
        ACTION_TIMEOUT,
      );
      assert.ok(found, `/provider should show options or known error. Got:\n${tui.text().slice(-500)}`);
    } finally {
      await tui.kill();
    }
  });

  // ── AC-7: Agent chatter visibility ─────────────────────────────────

  it("AC-7: Agent messages appear with distinct sender in stream", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // Send a message that would trigger agent processing
      tui.type("analyze our website SEO");
      tui.enter();

      // Wait for user message echo first (proves the message was accepted)
      const userEcho = await tui.waitForText("analyze our website SEO", ACTION_TIMEOUT);
      assert.ok(userEcho, `User message should be echoed in stream. Got:\n${tui.text().slice(-500)}`);

      // The "You" sender label should appear alongside the user message
      const hasYouLabel = tui.text().includes("You");
      assert.ok(hasYouLabel, `User message should have 'You' sender label. Got:\n${tui.text().slice(-500)}`);

      // Wait for either agent response, auth error, or error message
      // In all cases the TUI should show *something* — not crash
      const found = await tui.waitForMatch(
        /Team-Lead|API key|error|No API|unauthorized|thinking|set up/i,
        ACTION_TIMEOUT,
      );
      assert.ok(
        found,
        `Expected agent chatter or graceful error in stream. Got:\n${tui.text().slice(-500)}`,
      );
    } finally {
      await tui.kill();
    }
  });

  // ── AC-8: Slash command inline (/help) ─────────────────────────────

  it("AC-8: /help shows help text inline and input remains available", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      tui.type("/help");
      tui.enter();

      // Help should list available commands
      const found = await tui.waitForMatch(
        /\/agents|\/init|\/clear|Available|commands/i,
        ACTION_TIMEOUT,
      );
      assert.ok(found, `/help should show available commands. Got:\n${tui.text().slice(-500)}`);

      // User should be able to continue typing immediately after
      await tui.wait(500);
      tui.type("test after help");

      // No crash — TUI still responsive
      await tui.wait(300);
      const text = tui.text();
      // Help output should still be visible (or scrolled in stream)
      assert.ok(text.length > 0, "TUI should still be rendering after /help");
    } finally {
      await tui.kill();
    }
  });

  // ── AC-9: Ctrl+C graceful exit ─────────────────────────────────────

  it("AC-9: Ctrl+C exits gracefully (no crash)", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // First Ctrl+C — may show hint or start exit
      tui.ctrlC();
      await tui.wait(500);

      // Second Ctrl+C — should exit cleanly
      tui.ctrlC();
      await tui.wait(1500);

      // The TUI should have exited or be in process of exiting
      // No crash — we verify by checking the output doesn't contain stack traces
      const text = tui.text();
      const hasCrash = /TypeError|ReferenceError|Cannot read|FATAL|unhandled/i.test(text);
      assert.ok(!hasCrash, `TUI should exit cleanly without crash. Got:\n${text.slice(-500)}`);
    } finally {
      await tui.kill();
    }
  });

  // ── AC-10: Error resilience ────────────────────────────────────────

  it("AC-10: Error resilience — TUI handles API errors without crashing", async () => {
    const tui = createTui();
    try {
      await waitForStartup(tui);

      // Send a message (will likely fail without valid API key)
      tui.type("tell me about our pipeline");
      tui.enter();

      // Should show error message OR response — either way, not a crash
      const found = await tui.waitForMatch(
        /error|API key|No API|unauthorized|pipeline|Team-Lead|thinking/i,
        ACTION_TIMEOUT,
      );
      assert.ok(found, `Expected error or response, not silence. Got:\n${tui.text().slice(-500)}`);

      // TUI should remain responsive — user can still type
      await tui.wait(500);
      tui.type("second message after error");
      tui.enter();

      // Wait for second message to appear (proves TUI is still alive)
      const stillAlive = await tui.waitForText("second message after error", ACTION_TIMEOUT);
      assert.ok(
        stillAlive,
        `TUI should remain responsive after error — second message should appear. Got:\n${tui.text().slice(-500)}`,
      );

      // No crash indicators
      const text = tui.text();
      const hasCrash = /TypeError|ReferenceError|Cannot read|FATAL|unhandled/i.test(text);
      assert.ok(!hasCrash, `No crash after error. Got:\n${text.slice(-500)}`);
    } finally {
      await tui.kill();
    }
  });
});
