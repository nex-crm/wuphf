import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForShellReady,
} from "./_helpers";

// Regression pins for behaviours that broke during the TanStack Router
// migration and were caught in PR #634 review. The router migration
// reshaped how every surface reads "the active channel", so this file
// guards against silent semantics drift in the layers above the route
// switch — the things route-matrix.spec.ts can't see because it only
// asserts that each URL mounts.
//
// Each test names the symptom it would have flagged in main *before* the
// fix, so a future contributor reverting the workaround sees a failure
// that says what they broke and where.

const TEST_CHANNEL = "launch";

async function seedTestChannel(page: Page): Promise<void> {
  // 409 (already exists) is fine — the broker reset between tests strips
  // user-created channels, but a previous test in the same run may have
  // left state intact. Anything else fails loudly so a broken /channels
  // endpoint can't masquerade as a passing test.
  const resp = await page.request.post("/api/channels", {
    data: {
      action: "create",
      slug: TEST_CHANNEL,
      name: TEST_CHANNEL,
      description: "Cross-route regression channel",
    },
  });
  if (!resp.ok() && resp.status() !== 409) {
    throw new Error(
      `seed channel failed: ${resp.status()} ${await resp.text()}`,
    );
  }
}

async function gotoShell(page: Page, route: string): Promise<void> {
  await page.goto(route);
  await waitForShellReady(page);
}

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("PR #634 review pins", () => {
  test("Console keeps the user's last-visited channel after nav off-conversation", async ({
    page,
  }) => {
    // Repro: legacy `s.currentChannel` retained the last-visited slug
    // across non-conversation navigation. After the TanStack migration,
    // ConsoleApp read `useChannelSlug() ?? "general"` directly off the
    // URL, so opening Console from #launch silently snapped to #general.
    // useFallbackChannelSlug threads URL → lastConversationalChannel →
    // "general" so the user's working channel survives the hop.
    const getErrors = collectReactErrors(page);
    await seedTestChannel(page);
    await gotoShell(page, "/");
    await gotoShell(page, `/#/channels/${TEST_CHANNEL}`);
    await expect(page.locator(".composer-input")).toHaveAttribute(
      "placeholder",
      `Message #${TEST_CHANNEL}`,
    );

    await page.goto(`/#/apps/console`);
    const consolePage = page.getByTestId("app-page-console");
    await expect(consolePage).toBeVisible({ timeout: 10_000 });
    // ConsoleApp echoes the channel in two visible places: the header
    // chip and the prompt. Both should track #launch, not #general.
    await expect(consolePage).toContainText(`#${TEST_CHANNEL}`);
    await expect(consolePage).toContainText(`wuphf:${TEST_CHANNEL}$`);
    await expect(consolePage).not.toContainText("wuphf:general$");

    await expectNoReactErrors(page, getErrors, "during console fallback");
  });

  test("Requests query targets the user's last-visited channel, not #general", async ({
    page,
  }) => {
    // Repro: the broker /requests endpoint is channel-scoped. With the
    // bare `useChannelSlug() ?? "general"` collapse, opening
    // /apps/requests from #launch silently fetched general's request
    // queue and hid the user's actual pending requests. The fix routes
    // through useFallbackChannelSlug too. We assert against the network
    // contract (the query string the broker actually sees) since the
    // surface itself doesn't render the channel name.
    const getErrors = collectReactErrors(page);
    await seedTestChannel(page);
    await gotoShell(page, "/");
    await gotoShell(page, `/#/channels/${TEST_CHANNEL}`);

    const seenChannels = new Set<string>();
    await page.route("**/api/requests*", async (route) => {
      const url = new URL(route.request().url());
      const channel = url.searchParams.get("channel");
      if (channel) seenChannels.add(channel);
      await route.continue();
    });

    await page.goto(`/#/apps/requests`);
    await expect(page.getByTestId("app-page-requests")).toBeVisible({
      timeout: 10_000,
    });

    // Wait for at least one channel-scoped fetch to land. RequestsApp
    // refetches on a 5s interval, but the initial mount fires
    // synchronously — bound the wait so a regression failure is loud
    // instead of a flaky timeout.
    await expect
      .poll(() => Array.from(seenChannels), { timeout: 10_000 })
      .toContain(TEST_CHANNEL);
    expect(
      seenChannels.has("general"),
      `Requests should not have queried #general while last-visited was #${TEST_CHANNEL}; saw ${[
        ...seenChannels,
      ].join(", ")}`,
    ).toBe(false);

    await expectNoReactErrors(page, getErrors, "during requests fallback");
  });

  test("AgentPanel hides the per-channel toggle when no conversation channel is active", async ({
    page,
  }) => {
    // Repro: AgentPanel.AgentPanelView used `useChannelSlug() ?? "general"`
    // and rendered an "Enabled in #general" toggle that POSTed to
    // /channel-members for #general — even when the user opened the
    // panel from /apps/console while last-viewing #launch. The fix
    // narrows currentChannel to URL-only (no fallback) and gates the
    // toggle UI on a real conversation route.
    //
    // The broker's lead agent (slug "ceo") has the toggle hidden by
    // design (canRemove/canToggle are false for built-in members), so
    // pick the FIRST non-CEO agent for this test. The seeded roster
    // always has one (founder/operator/builder/reviewer in the default
    // manifest).
    const getErrors = collectReactErrors(page);
    await gotoShell(page, "/");

    const nonLeadAgent = page
      .locator('button[data-agent-slug]:not([data-agent-slug="ceo"])')
      .first();
    await expect(nonLeadAgent).toBeVisible({ timeout: 10_000 });

    // Sanity: on a conversation route, the toggle is visible.
    await nonLeadAgent.click();
    await expect(page.locator(".agent-panel")).toBeVisible();
    await expect(page.locator(".agent-toggle")).toBeVisible();
    // Close the panel before navigating so the route-change effect
    // doesn't auto-close inside the assertion window.
    await page.locator(".agent-panel-close").click();
    await expect(page.locator(".agent-panel")).toHaveCount(0);

    // Navigate to an off-conversation surface, then re-open the panel.
    await page.goto("/#/apps/console");
    await expect(page.getByTestId("app-page-console")).toBeVisible({
      timeout: 10_000,
    });
    await nonLeadAgent.click();
    await expect(page.locator(".agent-panel")).toBeVisible();
    // The toggle MUST be gone — both the slider and its label.
    await expect(page.locator(".agent-toggle")).toHaveCount(0);
    await expect(
      page.locator(".agent-panel-stat-label", { hasText: /Enabled in/ }),
    ).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "agent panel off-conversation");
  });

  test("ThreadPanel keeps its originating channel after off-conversation nav", async ({
    page,
  }) => {
    // Repro: activeThreadId was a bare string on the store; ThreadPanel
    // resolved its channel via `useChannelSlug() ?? "general"`. Open a
    // thread on #launch, navigate to /apps/console, and the panel
    // header silently flipped to "#general" — and any reply posted from
    // there would land in general. The fix promotes activeThread to
    // {id, channelSlug} captured at open time so the panel header (and
    // postMessage target) stay pinned to the originating channel.
    const getErrors = collectReactErrors(page);
    await seedTestChannel(page);
    await gotoShell(page, "/");
    await gotoShell(page, `/#/channels/${TEST_CHANNEL}`);

    // Seed a thread parent the user can open. Two messages so the broker
    // marks the parent as a thread head once the reply attaches.
    const parentText = `route-regression parent ${Date.now()}`;
    const replyText = `route-regression reply ${Date.now()}`;
    const parent = await page.request.post("/api/messages", {
      data: { from: "human", channel: TEST_CHANNEL, content: parentText },
    });
    expect(parent.ok()).toBeTruthy();
    // Broker response shape is `{id: <msg-id>, total: <n>}` — no nested
    // message envelope. See internal/team/broker_messages.go:259.
    const parentBody = (await parent.json()) as { id?: string };
    const parentId = parentBody.id;
    expect(parentId, "broker did not return parent message id").toBeTruthy();

    const reply = await page.request.post("/api/messages", {
      data: {
        from: "human",
        channel: TEST_CHANNEL,
        content: replyText,
        reply_to: parentId,
      },
    });
    expect(reply.ok()).toBeTruthy();

    // Open the thread by clicking the reply-count chip on the parent
    // bubble. The MessageBubble for a parent with replies renders a
    // dedicated trigger.
    const parentBubble = page
      .locator(".message", { hasText: parentText })
      .first();
    await expect(parentBubble).toBeVisible({ timeout: 10_000 });
    // MessageBubble renders an `.inline-thread-toggle` button under any
    // parent message with replies (replyCount > 0). See
    // web/src/components/messages/MessageBubble.tsx:217.
    const threadOpen = parentBubble.locator(".inline-thread-toggle");
    await expect(threadOpen).toBeVisible({ timeout: 10_000 });
    await threadOpen.click();

    const threadPanel = page.locator(".thread-panel");
    await expect(threadPanel).toBeVisible();
    await expect(threadPanel.locator(".thread-panel-channel")).toHaveText(
      `#${TEST_CHANNEL}`,
    );

    // Navigate to a non-conversation route. ThreadPanel is mounted in
    // Shell so it persists across navigation.
    await page.goto("/#/apps/console");
    await expect(page.getByTestId("app-page-console")).toBeVisible({
      timeout: 10_000,
    });

    // The panel must still be open AND still pinned to the originating
    // channel. Pre-fix: the channel chip flipped to "#general".
    await expect(threadPanel).toBeVisible();
    await expect(threadPanel.locator(".thread-panel-channel")).toHaveText(
      `#${TEST_CHANNEL}`,
    );

    await expectNoReactErrors(page, getErrors, "thread panel cross-route");
  });

  test("/apps/threads no longer mounts a Threads app panel", async ({
    page,
  }) => {
    // Repro: routeRegistry.APP_PANEL_IDS still listed "threads" after
    // the legacy /threads alias was dropped, so /apps/threads kept
    // mounting the orphaned ThreadsApp surface. Removing it from the
    // panel id list moves the URL into the unknown-app fallback.
    const getErrors = collectReactErrors(page);
    await page.goto("/#/apps/threads");

    // The route still resolves (the generic /apps/$appId route accepts
    // any id), but MainContent now narrows via isAppPanelId and renders
    // the unknown-app surface instead of ThreadsApp.
    const panel = page.getByTestId("app-page-threads");
    await expect(panel).toBeVisible({ timeout: 10_000 });
    await expect(panel).toContainText("Unknown app: threads");
    // ThreadsApp's "All threads" header must NOT render.
    await expect(panel).not.toContainText(/All threads|active thread/i);

    await expectNoReactErrors(page, getErrors, "threads app removal");
  });

  test("StatusBar and ChannelHeader show identical titles on wiki / notebooks / reviews", async ({
    page,
  }) => {
    // Repro: the channelLabel switch in StatusBar drifted from
    // ChannelHeader.headerTitleAndDesc — wiki-lookup rendered as the
    // raw route key "wiki-lookup" while the header showed "Wiki",
    // and notebooks/reviews were lower-cased on one side and
    // capitalized on the other. Aligning them means the user sees one
    // canonical title for each surface.
    const getErrors = collectReactErrors(page);

    const surfaces = [
      { route: "/#/wiki", label: "Wiki" },
      { route: "/#/wiki/lookup?q=test", label: "Wiki" },
      { route: "/#/notebooks", label: "Notebooks" },
      { route: "/#/reviews", label: "Reviews" },
    ];

    for (const { route, label } of surfaces) {
      await page.goto(route);
      await expect(page.locator(".channel-title")).toBeVisible({
        timeout: 10_000,
      });
      const headerText = (
        await page.locator(".channel-title").textContent()
      )?.trim();
      // The status bar layout is: <StatusPill /> <channelLabel /> <modeLabel />
      // where StatusPill is also `.status-bar-item` (workspace pill). The
      // route label is the SECOND .status-bar-item — pin to that index
      // so we don't accidentally read the workspace pill's content.
      const statusBarItem = page
        .locator(".status-bar > .status-bar-item")
        .nth(1);
      const statusText = (await statusBarItem.textContent())?.trim();
      expect(
        headerText,
        `ChannelHeader for ${route} should show ${label}`,
      ).toBe(label);
      expect(
        statusText,
        `StatusBar for ${route} should match the header label`,
      ).toBe(label);
    }

    await expectNoReactErrors(page, getErrors, "header/status parity");
  });

  test("NotFoundSurface link routes to #general (smoke, not an epicentric pin)", async ({
    page,
  }) => {
    // NOTE: this is a smoke check on the not-found "Go to #general"
    // affordance, NOT a regression pin. The fix in this PR swapped a
    // hardcoded `<a href="#/channels/general">` for a typed
    // `<Link to="/channels/$channelSlug">`, but under hash history both
    // forms resolve to the same href — verified empirically by running
    // this test against the pre-fix tree (it passes there too). The
    // future-proofing benefit (router-strategy independence) is outside
    // the matrix this spec exercises. Keeping the test for surface
    // coverage of the not-found affordance itself.
    const getErrors = collectReactErrors(page);
    await page.goto("/#/missing-route");
    const notFound = page.getByTestId("route-not-found");
    await expect(notFound).toBeVisible();
    const link = notFound.locator("a");
    await expect(link).toHaveText(/general/i);
    const href = await link.getAttribute("href");
    expect(href).toBeTruthy();
    // Hash history → href starts with "#/" or "/#/" depending on how
    // TanStack renders. Either is fine; what matters is it resolves to
    // /channels/general.
    expect(href).toMatch(/#\/channels\/general$/);

    await link.click();
    await expect(page).toHaveURL(/#\/channels\/general$/);
    await expect(page.locator(".composer-input")).toHaveAttribute(
      "placeholder",
      "Message #general",
    );

    await expectNoReactErrors(page, getErrors, "not-found link");
  });

  test("hash with a query-string suffix doesn't bump unread for the active channel", async ({
    page,
  }) => {
    // Repro: useBrokerEvents.activeBrokerChannel parsed
    // window.location.hash with a bare path.split("/"). When the hash
    // carried a search-string suffix
    // (e.g. "#/channels/general?modal=settings") the slug bled into the
    // next segment as "general?modal=settings", the comparison failed,
    // and every inbound message bumped #general's unread counter while
    // the user was staring at it. The fix splits on "?" before parsing.
    //
    // We can't easily fake a future suffix the app will write (no code
    // generates that yet). But we can simulate it: visit #general, then
    // append "?ts=…" via location.hash and post a message. Pre-fix:
    // unread for #general bumps to 1. Post-fix: stays 0.
    const getErrors = collectReactErrors(page);
    await gotoShell(page, "/");
    await gotoShell(page, "/#/channels/general");

    // Append a synthetic query string to the hash. Avoids `page.goto`
    // round-trips so the channel's unread state isn't reset by a fresh
    // mount.
    await page.evaluate(() => {
      window.location.hash = "#/channels/general?probe=1";
    });
    // Give the SSE handler a tick to observe the new hash before the
    // next message lands.
    await page.waitForTimeout(50);

    const payload = `unread-suppression probe ${Date.now()}`;
    const post = await page.request.post("/api/messages", {
      data: { from: "ceo", channel: "general", content: payload },
    });
    expect(post.ok()).toBeTruthy();

    // The freshly-posted message must render in the feed (the human IS
    // watching #general). If unread bumped, MessageFeed would still show
    // the message but the sidebar would carry a non-zero badge.
    await expect(
      page.locator(".message", { hasText: payload }).first(),
    ).toBeVisible({ timeout: 10_000 });

    // Sidebar #general entry — read its unread badge. ChannelList renders
    // the count inside .sidebar-channel; absence means zero.
    const generalRow = page
      .locator(".sidebar-channels button", { hasText: /general/i })
      .first();
    await expect(generalRow).toBeVisible();
    const badge = generalRow.locator(".sidebar-badge");
    // 0 unread → no badge. Anything else means the query-string slipped
    // past the parser.
    await expect(badge).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "unread suppression");
  });
});
