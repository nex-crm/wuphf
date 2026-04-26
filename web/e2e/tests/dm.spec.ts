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
  test("clicking an agent → Open DM → composer addresses the DM channel", async ({
    page,
  }) => {
    test.setTimeout(45_000);
    const getErrors = collectReactErrors(page);

    await page.goto("/");
    await waitForShellReady(page);

    // Composer placeholder reflects the active channel (Composer.tsx:548 —
    // `Message #${currentChannel}`). Capture the pre-DM value so we can
    // assert it actually changes.
    const composer = page.locator(".composer-input");
    const beforePlaceholder = await composer.getAttribute("placeholder");
    expect(
      beforePlaceholder,
      "composer placeholder should reflect a channel name",
    ).toMatch(/^Message #/);
    expect(beforePlaceholder).not.toContain("__"); // pre-DM = #general or similar

    // Click the first agent and read its slug — same selector smoke uses.
    const firstAgent = page.locator("button[data-agent-slug]").first();
    await expect(firstAgent).toBeVisible({ timeout: 10_000 });
    const agentSlug = await firstAgent.getAttribute("data-agent-slug");
    expect(
      agentSlug,
      "sidebar must expose at least one agent slug",
    ).toBeTruthy();
    await firstAgent.click();

    // AgentPanel mounts → "Open DM" button is visible. The button text is
    // literal in AgentPanel.tsx:296 and toggles to "Opening..." while the
    // request is in flight, so anchor by role+name.
    const openDM = page.getByRole("button", { name: "Open DM" });
    await expect(openDM).toBeVisible({ timeout: 10_000 });
    await openDM.click();

    // After enterDM, currentChannel is the direct slug (e.g. "ceo__human"
    // or "human__ceo" depending on lexical order — see directChannelSlug).
    // The composer placeholder updates synchronously off the store. Match
    // either ordering AND require the agent slug appear, so we don't pass
    // when the placeholder happens to read "Message #human-something".
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
});
