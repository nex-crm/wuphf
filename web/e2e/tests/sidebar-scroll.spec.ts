import {
  expect,
  type Locator,
  type Page,
  type Route,
  test,
} from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

const VIEWPORTS = [
  { width: 1280, height: 900 },
  { width: 1024, height: 640 },
  { width: 390, height: 700 },
] as const;

const crowdedMembers = [
  { slug: "human", name: "You", role: "human", status: "active" },
  ...Array.from({ length: 24 }, (_, index) => {
    const n = index + 1;
    return {
      slug: `agent-${n}`,
      name: `Agent ${n}`,
      role: "agent",
      status: "active",
      task: `Working on sidebar scroll coverage ${n}`,
      provider: "codex",
    };
  }),
];

const crowdedChannels = Array.from({ length: 24 }, (_, index) => {
  const n = index + 1;
  return {
    slug: n === 1 ? "general" : `channel-${n}`,
    name: n === 1 ? "General" : `Channel ${n}`,
    description: `Sidebar scroll channel ${n}`,
  };
});

async function fulfillJson(route: Route, body: unknown): Promise<void> {
  await route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function stubCrowdedSidebarData(page: Page): Promise<void> {
  await page.addInitScript(() => {
    localStorage.setItem(
      "wuphf-sidebar-sections",
      JSON.stringify({
        agents: true,
        channels: true,
        apps: true,
      }),
    );
    localStorage.removeItem("wuphf-sidebar-bg");
  });

  await page.route("**/api/office-members*", (route) =>
    fulfillJson(route, { members: crowdedMembers }),
  );
  await page.route("**/api/channels*", (route) =>
    fulfillJson(route, { channels: crowdedChannels }),
  );
  await page.route("**/api/requests*", (route) =>
    fulfillJson(route, { requests: [] }),
  );
  await page.route(/\/api\/review\/list(?:\?|$)/, (route) =>
    fulfillJson(route, { reviews: [] }),
  );
}

async function scrollMetrics(section: Locator): Promise<{
  clientHeight: number;
  overflowY: string;
  scrollHeight: number;
  scrollTop: number;
}> {
  return section.evaluate((el) => {
    const styles = window.getComputedStyle(el);
    return {
      clientHeight: el.clientHeight,
      overflowY: styles.overflowY,
      scrollHeight: el.scrollHeight,
      scrollTop: el.scrollTop,
    };
  });
}

async function waitForScrollSettle(section: Locator): Promise<void> {
  await section.evaluate(
    (el) =>
      new Promise<void>((resolve) => {
        let lastScrollTop = el.scrollTop;
        let stableFrames = 0;
        let frames = 0;

        const check = () => {
          frames += 1;
          const currentScrollTop = el.scrollTop;
          stableFrames =
            currentScrollTop === lastScrollTop ? stableFrames + 1 : 0;
          lastScrollTop = currentScrollTop;

          if (stableFrames >= 2 || frames >= 20) {
            resolve();
            return;
          }
          requestAnimationFrame(check);
        };

        requestAnimationFrame(check);
      }),
  );
}

async function ensureSidebarExpanded(page: Page): Promise<void> {
  const expandedSidebar = page.locator("aside.sidebar:not(.sidebar-collapsed)");
  const isVisible = await expandedSidebar.isVisible();
  if (!isVisible) {
    await page.getByRole("button", { name: "Expand sidebar" }).click();
  }
  await expect(expandedSidebar).toBeVisible();
}

/**
 * Reachability check for the crowded sidebar. The scroll architecture has
 * moved more than once (PR #919 unified it into `.sidebar-scroll`; the v3
 * layout gives the channels/apps sections their own flex-bounded regions),
 * so which element actually scrolls depends on the section. Rather than
 * couple to one container, ask the browser to bring each target into view
 * via its real scrollable ancestor and assert it lands in the viewport. An
 * item that is clipped or orphaned with no scrollable path still fails —
 * which is the regression this guards.
 */
async function expectWheelCanReach(
  page: Page,
  label: string,
  targets: Locator[],
): Promise<void> {
  const scroll = page.locator(".sidebar-scroll");
  await expect(scroll, "sidebar scroll region should be visible").toBeVisible();

  // The root scroll region still owns vertical overflow at the CSS level
  // (layout.css — `.sidebar-scroll { overflow-y: auto }`), even when a
  // section bounds its own inner scroll.
  const metrics = await scrollMetrics(scroll);
  expect(
    ["auto", "scroll"].includes(metrics.overflowY),
    `sidebar-scroll should own vertical scrolling, found overflow-y: ${metrics.overflowY}`,
  ).toBe(true);

  for (const target of targets) {
    await target.scrollIntoViewIfNeeded();
    await waitForScrollSettle(scroll);
    await expect(
      target,
      `${label} target should be reachable within the sidebar`,
    ).toBeInViewport({ ratio: 0.2 });
  }
}

test.describe("left sidebar scrolling", () => {
  for (const viewport of VIEWPORTS) {
    test(`all expanded menu sections are reachable at ${viewport.width}x${viewport.height}`, async ({
      page,
    }) => {
      const getErrors = collectReactErrors(page);
      await page.setViewportSize(viewport);
      await stubCrowdedSidebarData(page);

      await page.goto("/");
      await waitForReactMount(page);
      await ensureSidebarExpanded(page);
      await expect(
        page
          .locator("aside.sidebar")
          .getByRole("button", { name: "Open settings" }),
      ).toBeInViewport();
      await expect(
        page.locator(".sidebar-agents button[data-agent-slug]"),
      ).toHaveCount(24);
      await expect(
        page.locator(".sidebar-channels button.sidebar-item"),
      ).toHaveCount(25);
      const appItems = page.locator(".sidebar-apps button.sidebar-item");
      await expect(appItems.first()).toBeVisible();

      await expectWheelCanReach(page, "Team", [
        page.locator('.sidebar-agents button[data-agent-slug="agent-24"]'),
        page.locator(".sidebar-agents .sidebar-add-btn"),
      ]);
      await expectWheelCanReach(page, "Channels", [
        page.getByRole("button", { name: "Channel 24" }),
        page.locator(".sidebar-channels .sidebar-add-btn"),
      ]);
      await expectWheelCanReach(page, "Apps", [appItems.last()]);

      await expectNoReactErrors(page, getErrors, "while scrolling the sidebar");
    });

    test(`section header pins data-stuck when scrolled past at ${viewport.width}x${viewport.height}`, async ({
      page,
    }) => {
      const getErrors = collectReactErrors(page);
      await page.setViewportSize(viewport);
      await stubCrowdedSidebarData(page);

      await page.goto("/");
      await waitForReactMount(page);
      await ensureSidebarExpanded(page);

      const scroll = page.locator(".sidebar-scroll");
      await expect(scroll).toBeVisible();
      // Each section's title bar is the sticky chrome; we want to assert
      // that scrolling far enough flips data-stuck on the title bar of a
      // later section (Tools) which only pins once Channels has fully
      // slid behind it.
      const channelsTitleBar = page
        .locator(".sidebar-section", { has: page.getByText("Channels") })
        .locator(".sidebar-section-title-bar");
      await expect(channelsTitleBar).toBeVisible();
      // Pre-scroll: no headers should be stuck except possibly the very
      // first (Agents). Channels is mid-list, so it should read false.
      await expect(channelsTitleBar).toHaveAttribute("data-stuck", "false");

      // Scroll far enough that Channels' header has reached the top of
      // .sidebar-scroll. The crowded fixture has 24 agents above it; a
      // generous wheel push lands us safely past that.
      await scroll.hover();
      for (let i = 0; i < 12; i += 1) {
        const stuck = await channelsTitleBar.getAttribute("data-stuck");
        if (stuck === "true") break;
        await page.mouse.wheel(0, 800);
        await waitForScrollSettle(scroll);
      }
      await expect(channelsTitleBar).toHaveAttribute("data-stuck", "true");

      await expectNoReactErrors(
        page,
        getErrors,
        "while asserting sticky pin behavior",
      );
    });

    test(`collapsed section body is removed from a11y tree and tab order at ${viewport.width}x${viewport.height}`, async ({
      page,
    }) => {
      const getErrors = collectReactErrors(page);
      await page.setViewportSize(viewport);
      await stubCrowdedSidebarData(page);

      await page.goto("/");
      await waitForReactMount(page);
      await ensureSidebarExpanded(page);

      // Collapse the Channels section.
      const channelsToggle = page
        .locator(".sidebar-section", { has: page.getByText("Channels") })
        .locator("button.sidebar-section-toggle");
      await expect(channelsToggle).toHaveAttribute("aria-expanded", "true");
      await channelsToggle.click();
      await expect(channelsToggle).toHaveAttribute("aria-expanded", "false");

      // The collapsible body should be marked inert and aria-hidden so
      // screen readers skip it and Tab can't land on its buttons.
      const channelsBody = page
        .locator(".sidebar-section", { has: page.getByText("Channels") })
        .locator(".sidebar-collapsible");
      await expect(channelsBody).toHaveAttribute("aria-hidden", "true");
      await expect(channelsBody).toHaveAttribute("inert", "");

      await expectNoReactErrors(
        page,
        getErrors,
        "while asserting collapsed-section a11y",
      );
    });
  }
});
