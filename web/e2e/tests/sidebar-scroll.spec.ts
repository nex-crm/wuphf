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
        issues: true,
        apps: true,
        recent: true,
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

/**
 * The sidebar refactor (PR #919) collapsed per-section scroll regions
 * into one parent scroll container `.sidebar-scroll`. Sections themselves
 * now hug their content; reachability is verified by wheel-scrolling
 * `.sidebar-scroll` until each target is in viewport.
 */
async function expectWheelCanReach(
  page: Page,
  label: string,
  targets: Locator[],
): Promise<void> {
  const scroll = page.locator(".sidebar-scroll");
  await expect(scroll, "sidebar scroll region should be visible").toBeVisible();
  await scroll.evaluate((el) => {
    el.scrollTop = 0;
  });

  const before = await scrollMetrics(scroll);
  expect(
    before.scrollHeight,
    `sidebar should overflow in the crowded fixture (${label})`,
  ).toBeGreaterThan(before.clientHeight + 1);
  expect(
    ["auto", "scroll"].includes(before.overflowY),
    `sidebar-scroll should own vertical scrolling, found overflow-y: ${before.overflowY}`,
  ).toBe(true);

  await scroll.hover();
  for (const target of targets) {
    for (let i = 0; i < 24; i += 1) {
      const isInView = await target.evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return rect.top >= 0 && rect.bottom <= window.innerHeight;
      });
      if (isInView) break;
      await page.mouse.wheel(0, 600);
      await waitForScrollSettle(scroll);
      const current = await scrollMetrics(scroll);
      if (current.scrollTop + current.clientHeight >= current.scrollHeight - 2) {
        break;
      }
    }
    await expect(
      target,
      `${label} target should be reachable by scrolling .sidebar-scroll`,
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

      await expect(
        page.locator("aside.sidebar:not(.sidebar-collapsed)"),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Open settings" }),
      ).toBeInViewport();
      await expect(page.locator("button[data-agent-slug]")).toHaveCount(24);
      await expect(
        page.locator(".sidebar-channels button.sidebar-item"),
      ).toHaveCount(25);
      const appItems = page.locator(".sidebar-apps button.sidebar-item");
      await expect(appItems.first()).toBeVisible();

      await expectWheelCanReach(page, "Team", [
        page.locator('button[data-agent-slug="agent-24"]'),
        page.locator(".sidebar-agents .sidebar-add-btn"),
      ]);
      await expectWheelCanReach(page, "Channels", [
        page.getByRole("button", { name: "Channel 24" }),
        page.locator(".sidebar-channels .sidebar-add-btn"),
      ]);
      await expectWheelCanReach(page, "Apps", [appItems.last()]);

      await expectNoReactErrors(page, getErrors, "while scrolling the sidebar");
    });
  }
});
