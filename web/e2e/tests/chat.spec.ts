import { expect, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// Message round-trip parity with the TUI shell suite (tests/e2e/agent-response-e2e.sh).
// The TUI side asserts: user can send, broker accepts, an agent reply lands in
// the channel pane. The Web side has no equivalent — smoke.spec.ts only checks
// that the shell renders. This file closes the gap on the web surface.
//
// CI runs wuphf with a stub `claude` (see ci.yml — "Install claude CLI stub"),
// so real agent replies do not arrive. To still cover the inbound-render path,
// the second test injects a synthetic agent message through the same broker
// API the real CEO would use. That guards the MessageFeed render path against
// regressions independent of LLM availability.
//
// Like the existing specs, this one assumes wuphf was started with
// ~/.wuphf/onboarded.json pre-seeded so the app lands in the Shell.

// Tests run serially against the same wuphf process. Reset between tests so
// earlier sends do not pollute later assertions (especially `getByText`).
test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("wuphf web chat round-trip", () => {
  test('typed message lands in the feed as a "You" bubble', async ({
    page,
  }) => {
    // Mirrors the first half of agent-response-e2e.sh: human posts to the
    // channel, broker accepts, the message becomes visible. We do not depend
    // on an agent replying — that is covered by the inbound-render test below.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    // Unique payload so a stale session message can never satisfy the assertion.
    const payload = `playwright round-trip ${Date.now()}`;

    const composer = page.locator(".composer-input");
    await composer.fill(payload);
    await page.locator(".composer-send").click();

    // Composer clears on successful send (Composer.tsx — resetComposer).
    // If the broker rejects (e.g. 409 interview pending), the composer keeps
    // the text and showNotice fires — that fails this assertion loud.
    await expect(composer).toHaveValue("", { timeout: 10_000 });

    // The "You" bubble carries .badge-neutral with text "human" and the typed
    // content in .message-text. Anchor on the unique payload, not the badge,
    // so this stays robust if seeded fixtures introduce other human messages.
    const bubble = page.locator(".message", { hasText: payload }).first();
    await expect(bubble).toBeVisible({ timeout: 10_000 });
    await expect(bubble.locator(".message-author")).toHaveText("You");
    await expect(bubble.locator(".badge-neutral")).toHaveText("human");

    await expectNoReactErrors(page, getErrors, "during message send");
  });

  test("agent reply renders with role badge when broker emits one", async ({
    page,
    request,
  }) => {
    // 30s default budget is tight: cold-mount + waitForShellReady can eat
    // 10s, then the message poll has another 15s to land.
    test.setTimeout(45_000);

    // Inbound-render guard. The broker is the source of truth for messages;
    // the web client subscribes via SSE (see App.tsx + useMessages). This
    // test impersonates the broker by POSTing a message attributed to a
    // built-in agent through the same /api/messages endpoint the real broker
    // uses internally. If MessageFeed cannot render an agent message
    // (markdown path, role-badge lookup, avatar/harness resolution), this
    // fails — even when no real LLM is wired up.
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    // Pick an agent that the seeded broker is guaranteed to know about. Read
    // the slug AND the user-facing name from the sidebar so the assertion
    // tracks the actual seeded roster instead of hard-coding "ceo".
    const firstAgent = page.locator("button[data-agent-slug]").first();
    await expect(firstAgent).toBeVisible({ timeout: 10_000 });
    const agentSlug = await firstAgent.getAttribute("data-agent-slug");
    expect(
      agentSlug,
      "sidebar must expose at least one agent slug",
    ).toBeTruthy();
    const agentName = (
      await firstAgent.locator(".sidebar-agent-name").textContent()
    )?.trim();
    expect(agentName, "sidebar agent must have a visible name").toBeTruthy();

    const payload = `synthetic agent reply ${Date.now()}`;

    // Same-origin proxy injects the broker token; no bearer needed from the
    // test. Use the request fixture's baseURL (set in playwright.config.ts).
    const resp = await request.post("/api/messages", {
      data: { from: agentSlug, channel: "general", content: payload },
    });
    expect(
      resp.ok(),
      `broker rejected synthetic agent message: ${resp.status()} ${await resp.text()}`,
    ).toBeTruthy();

    // Positive attribution: the bubble renders with the seeded agent's name
    // (NOT a fallback slug, NOT "You"). A regression in useOfficeMembers
    // would render `message.from` raw and fail this exact-match assertion.
    const bubble = page.locator(".message", { hasText: payload }).first();
    await expect(bubble).toBeVisible({ timeout: 15_000 });
    await expect(bubble.locator(".message-author")).toHaveText(agentName!);
    // Agent bubbles never carry the human badge.
    await expect(bubble.locator(".badge-neutral")).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "during agent message render");
  });
});
