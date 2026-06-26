import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  resetBroker,
  waitForReactMount,
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
  await waitForReactMount(page);
  // Wait on the always-present sidebar, not the channel composer: the index
  // route ("/") now renders the home composer (no `.composer-input`), and
  // these tests assert channel-specific composer placeholders explicitly
  // where they need them.
  await expect(page.locator("aside.sidebar")).toBeVisible({ timeout: 10_000 });
}

test.afterEach(async ({ request }) => {
  await resetBroker(request);
});

test.describe("PR #634 review pins", () => {
  test("/apps/console no longer mounts a Console app panel", async ({
    page,
  }) => {
    // The Console surface was intentionally removed in #1055. The generic
    // /apps/$appId route still accepts the URL, but MainContent must narrow
    // it into the unknown-app fallback instead of trying to mount stale UI.
    const getErrors = collectReactErrors(page);
    await page.goto(`/#/apps/console`);
    const consolePage = page.getByTestId("app-page-console");
    await expect(consolePage).toBeVisible({ timeout: 10_000 });
    await expect(consolePage).toContainText("Unknown app: console");

    await expectNoReactErrors(page, getErrors, "console app removal");
  });

  test("Legacy /#/apps/requests redirects to the Tasks board", async ({
    page,
  }) => {
    // The standalone RequestsApp was retired and the Inbox consolidated
    // into the Task board; /apps/requests now renders TasksRedirect which
    // navigates to /tasks. The last-visited-channel regression that the
    // prior test guarded (RequestsApp re-fetching general's queue) is gone
    // with the surface — the board doesn't fetch by channel.
    const getErrors = collectReactErrors(page);
    await page.goto(`/#/apps/requests`);
    // The TasksRedirect testid mounts then unmounts as soon as the
    // useEffect fires, so racing toBeVisible against the redirect is
    // flaky. URL change is the stable assertion.
    await expect(page).toHaveURL(/#\/tasks$/, { timeout: 5_000 });
    await expectNoReactErrors(
      page,
      getErrors,
      "during legacy requests redirect",
    );
  });

  test("AgentPanel's per-channel toggle never renders off a conversation route", async ({
    page,
  }) => {
    // Repro (#634): AgentPanel.AgentPanelView used `useChannelSlug() ??
    // "general"` and rendered an "Enabled in #general" toggle that POSTed to
    // /channel-members for #general — even when opened off-conversation. v3
    // reshaped the surface: the panel is opened from the channel participant
    // list (ChannelParticipants → onOpenAgent), the toggle is gated on a
    // URL-derived channel (no fallback), and leaving the conversation route
    // closes the panel outright. Together those make a stale-channel toggle
    // structurally unreachable — which is exactly what this pins.
    //
    // The lead agent (slug "ceo") hides the toggle by design (built-ins are
    // not per-channel toggleable), so open a NON-lead participant. The seeded
    // roster always has one (planner/executor/reviewer in the default manifest).
    const getErrors = collectReactErrors(page);
    await gotoShell(page, "/#/channels/general");

    const nonLeadParticipant = page
      .locator(".channel-participant-main")
      .filter({ hasNotText: /ceo/i })
      .first();
    await expect(nonLeadParticipant).toBeVisible({ timeout: 10_000 });
    await nonLeadParticipant.click();

    // On a conversation route the toggle renders, scoped to the URL channel.
    await expect(page.locator(".agent-panel")).toBeVisible({ timeout: 10_000 });
    await expect(page.locator(".agent-toggle")).toBeVisible();
    await expect(
      page.locator(".agent-panel-stat-label", { hasText: /Enabled in/ }),
    ).toContainText("#general");

    // Leaving the conversation surface closes the panel, so the per-channel
    // toggle can never render against a non-conversation route.
    await page.goto("/#/apps/graph");
    await expect(page.getByTestId("app-page-graph")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.locator(".agent-panel")).toHaveCount(0);
    await expect(page.locator(".agent-toggle")).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "agent panel off-conversation");
  });

  test("ThreadPanel keeps its originating channel after off-conversation nav", async ({
    page,
  }) => {
    // Repro: activeThreadId was a bare string on the store; ThreadPanel
    // resolved its channel via `useChannelSlug() ?? "general"`. Open a
    // thread on #launch, navigate to /apps/graph, and the panel
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
    await page.goto("/#/apps/graph");
    await expect(page.getByTestId("app-page-graph")).toBeVisible({
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
      const breadcrumbLeaf = page.locator(".breadcrumb-link-active");
      await expect(breadcrumbLeaf).toBeVisible({ timeout: 10_000 });
      const headerText = (await breadcrumbLeaf.textContent())?.trim();
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
});
