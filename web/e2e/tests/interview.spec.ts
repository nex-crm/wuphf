import { expect, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// Human-Interview parity with tests/e2e/session-full-e2e.sh (which exercises
// blocking signals + the TUI's Esc-to-pause flow).
//
// Wuphf's signature differentiator: agents can BLOCK on a human answer. The
// broker holds a request, the web client polls /requests, and InterviewBar +
// HumanInterviewOverlay render an actionable prompt above the composer. While
// any blocking request is pending, /api/messages returns 409 (broker.go —
// handlePostMessage), and Composer maps that to the "Answer the interview
// above to send messages." toast. Zero web coverage of this loop today —
// which is exactly the moment that wins in demos and breaks silently when
// useRequests / answerRequest plumbing rots.
//
// Test strategy: synthesize a blocking request through the same /api/requests
// endpoint the broker uses internally (handlePostRequest, broker.go:9754).
// Drive the answer through the bar, then assert it dismisses and sends are
// unblocked.

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("wuphf web human interview", () => {
  test("blocking request renders the InterviewBar and answering dismisses it", async ({
    page,
    request,
  }) => {
    test.setTimeout(60_000);
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    // Seed a blocking request via the same endpoint the broker uses to
    // emit interviews. `from` must be a real seeded agent slug so the
    // bar's `@<from>` chip renders against a known role.
    const firstAgent = page.locator("button[data-agent-slug]").first();
    await expect(firstAgent).toBeVisible({ timeout: 10_000 });
    const agentSlug = await firstAgent.getAttribute("data-agent-slug");
    expect(agentSlug).toBeTruthy();

    const question = `should we ship feature X? (interview ${Date.now()})`;
    const resp = await request.post("/api/requests", {
      data: {
        action: "create",
        from: agentSlug,
        channel: "general",
        title: "Approval needed",
        question,
        blocking: true,
        required: true,
        options: [
          { id: "yes", label: "Yes — ship it" },
          { id: "no", label: "Hold off" },
        ],
        recommended_id: "yes",
      },
    });
    expect(
      resp.ok(),
      `broker rejected synthetic interview: ${resp.status()} ${await resp.text()}`,
    ).toBeTruthy();

    // useRequests refetches every 5s (REQUEST_REFETCH_MS in useRequests.ts:10),
    // so the bar appears within that window. Use a 12s budget to absorb one
    // full poll cycle plus React commit.
    const bar = page.locator('.interview-bar[role="region"]');
    await expect(bar).toBeVisible({ timeout: 12_000 });

    // Sanity-check the bar content reflects the seeded request, not stale state.
    await expect(bar).toContainText("BLOCKING");
    await expect(bar).toContainText(`@${agentSlug}`);
    await expect(bar).toContainText(question);

    // While blocking, the broker rejects new messages with 409 → composer
    // surfaces "Answer the interview above..." (Composer.tsx:336). Send
    // through the UI and assert the toast — this is the contract the TUI
    // exercises under session-full-e2e.sh's Esc/blocking path.
    const composer = page.locator(".composer-input");
    await composer.fill("attempt during block");
    await page.locator(".composer-send").click();
    await expect(
      page
        .locator(".animate-fade")
        .filter({ hasText: /answer the interview above/i })
        .first(),
    ).toBeVisible({ timeout: 5_000 });

    // Answer the interview by clicking the recommended option.
    await bar.getByRole("button", { name: /Yes — ship it/ }).click();

    // The bar dismisses once the answer resolves and useRequests invalidates.
    await expect(bar).toBeHidden({ timeout: 10_000 });

    // Sends now succeed — the contract closes the loop.
    const payload = `post-interview send ${Date.now()}`;
    await composer.fill(payload);
    await page.locator(".composer-send").click();
    await expect(composer).toHaveValue("", { timeout: 10_000 });
    await expect(
      page.locator(".message", { hasText: payload }).first(),
    ).toBeVisible({
      timeout: 10_000,
    });

    await expectNoReactErrors(page, getErrors, "during interview round-trip");
  });
});
