import { expect, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// @mention rendering parity. The TUI suite has no clean equivalent — terminal
// "chips" are styled spans which don't survive screenshot diffs reliably.
// Web has a deterministic DOM signal: parseMentions/renderMentionTokens
// (web/src/lib/mentions.tsx) emits `<span class="mention">@<slug></span>` for
// every recognised slug. If a regression collapses the chip back to plain
// text — or fails to recognise a known slug — the visible message rendering
// silently degrades. Worth catching before PH.
//
// Untagged-vs-routed broker behavior is covered (in part) by the TUI's
// agent-response-e2e.sh; this spec focuses purely on the *render* path.

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("wuphf web mention chips", () => {
  test('typing "@<slug> ..." renders a mention chip in the human bubble', async ({
    page,
  }) => {
    test.setTimeout(45_000);
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    // Pull a known slug from the seeded sidebar — same idea as chat.spec.ts.
    const firstAgent = page.locator("button[data-agent-slug]").first();
    await expect(firstAgent).toBeVisible({ timeout: 10_000 });
    const agentSlug = await firstAgent.getAttribute("data-agent-slug");
    expect(
      agentSlug,
      "sidebar must expose at least one agent slug",
    ).toBeTruthy();

    const tail = `mention chip test ${Date.now()}`;
    const payload = `@${agentSlug} ${tail}`;

    const composer = page.locator(".composer-input");
    await composer.fill(payload);
    await page.locator(".composer-send").click();

    await expect(composer).toHaveValue("", { timeout: 10_000 });

    // Match the bubble by the unique tail so prior session messages don't
    // satisfy the locator. The chip is a `.mention` span inside `.message-text`.
    const bubble = page.locator(".message", { hasText: tail }).first();
    await expect(bubble).toBeVisible({ timeout: 10_000 });

    const chip = bubble.locator(".message-text .mention").first();
    await expect(chip).toBeVisible({ timeout: 5_000 });
    await expect(chip).toHaveText(`@${agentSlug}`);

    // Negative assertion: the message-text should NOT contain the literal
    // `@<slug>` as plain text alongside the chip — that would mean the
    // parser ran but the renderer kept the original token too.
    const rawText = (await bubble.locator(".message-text").textContent()) ?? "";
    const occurrences = rawText.split(`@${agentSlug}`).length - 1;
    expect(
      occurrences,
      "expected exactly one rendering of @<slug> (the chip), got duplicates",
    ).toBe(1);

    await expectNoReactErrors(page, getErrors, "during mention render");
  });

  test("a non-agent @-token stays as plain text (no chip)", async ({
    page,
  }) => {
    // Negative coverage: parseMentions is whitelisted to known slugs
    // (mentions.tsx:42 — only emits a chip when the slug is in knownSlugs).
    // A regression that turns it into a permissive parser would render
    // chips for arbitrary @-strings — bad for security (XSS via injected
    // mention styles) and UX (chip styling on garbage input).
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    const tail = `unknown mention ${Date.now()}`;
    const payload = `@nope-not-an-agent ${tail}`;

    const composer = page.locator(".composer-input");
    await composer.fill(payload);
    await page.locator(".composer-send").click();

    await expect(composer).toHaveValue("", { timeout: 10_000 });
    const bubble = page.locator(".message", { hasText: tail }).first();
    await expect(bubble).toBeVisible({ timeout: 10_000 });

    // No chip should render for the unknown slug.
    await expect(bubble.locator(".message-text .mention")).toHaveCount(0);
    // The literal text MUST still be visible to the human — this is the
    // path where a "drop unknown @-tokens" regression would silently
    // delete user input.
    await expect(bubble.locator(".message-text")).toContainText(
      "@nope-not-an-agent",
    );

    await expectNoReactErrors(page, getErrors, "during unknown mention render");
  });
});
