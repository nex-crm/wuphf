import { expect, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// 1:1 DM parity with tests/uat/agent-sidebar-dm-e2e.sh.
// Flow: click an agent in the sidebar → AgentPanel mounts → click "Open DM"
// → composer + feed switch from #general to the agent's direct channel
// (slug shape `<agent>__human` or `human__<agent>`, see directChannelSlug
// in stores/app.ts:15). The TUI test exercises the same sequence via the
// agent-sidebar shortcut. Web had zero coverage of this path.
//
// Why this matters: DM is the steering surface — when a user wants to
// course-correct a single agent without involving the rest of the team.
// A regression that breaks `enterDM` or DMView leaves users without that
// affordance, and PH-launch traffic will hit it on day one.

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("wuphf web 1:1 DM", () => {
  test("the /dm route addresses the agent's direct channel and accepts a send", async ({
    page,
  }) => {
    test.setTimeout(45_000);
    const getErrors = collectReactErrors(page);

    // v3 replaced the AgentPanel "Open DM" overlay with per-agent subspaces +
    // the canonical /#/dm/$slug route; a sidebar agent click now navigates to
    // the subspace. Read a real seeded slug from the sidebar, then open its DM
    // via the route — that is the steering surface this test guards.
    await page.goto("/#/channels/general");
    await waitForShellReady(page);

    const firstAgent = page.locator("button[data-agent-slug]").first();
    await expect(firstAgent).toBeVisible({ timeout: 10_000 });
    const agentSlug = await firstAgent.getAttribute("data-agent-slug");
    expect(
      agentSlug,
      "sidebar must expose at least one agent slug",
    ).toBeTruthy();

    await page.goto(`/#/dm/${agentSlug}`);
    await waitForShellReady(page);

    // The DM composer addresses the agent's direct channel (slug shape
    // `<agent>__human` or `human__<agent>`, see directChannelSlug). Match
    // either ordering AND require the agent slug appear, so we don't pass when
    // the placeholder happens to read "Message #human-something".
    const composer = page.locator(".composer-input");
    await expect(composer).toHaveAttribute(
      "placeholder",
      new RegExp(`^Message #(${agentSlug}__human|human__${agentSlug})$`),
      { timeout: 10_000 },
    );

    // Send a DM-only message. The bubble must land in the DM feed; if
    // routing leaks back to #general the assertion below fails because the
    // feed swap is keyed on `currentChannel` in MessageFeed.tsx:41.
    const payload = `dm test ${Date.now()}`;
    await composer.fill(payload);
    await page.locator(".composer-send").click();

    await expect(composer).toHaveValue("", { timeout: 10_000 });
    const bubble = page.locator(".message", { hasText: payload }).first();
    await expect(bubble).toBeVisible({ timeout: 10_000 });
    await expect(bubble.locator(".message-author")).toHaveText("You");

    await expectNoReactErrors(page, getErrors, "during DM open + send");
  });

  test("live stream section collapses without unmounting the stream", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/#/dm/ceo");
    await waitForShellReady(page);

    const liveStream = page.getByTestId("collapsible-live-stream");
    await expect(liveStream).toBeVisible({ timeout: 10_000 });

    const toggle = liveStream.getByRole("button", { name: /live stream/i });
    const body = liveStream.locator(".collapsible-section-body");

    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(body).toBeVisible();

    await toggle.click();

    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(body).toBeAttached();
    await expect(body).toBeHidden();

    await expectNoReactErrors(page, getErrors, "while collapsing live stream");
  });
});
