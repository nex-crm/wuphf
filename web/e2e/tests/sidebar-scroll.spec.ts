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
      JSON.stringify({ agents: true, channels: true, apps: true }),
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

async function expectWheelCanReach(
  page: Page,
  section: Locator,
  label: string,
  targets: Locator[],
): Promise<void> {
  await expect(
    section,
    `${label} scroll region should be visible`,
  ).toBeVisible();
  await section.evaluate((el) => {
    el.scrollTop = 0;
  });

  const before = await scrollMetrics(section);
  expect(
    before.scrollHeight,
    `${label} should overflow in the crowded sidebar fixture`,
  ).toBeGreaterThan(before.clientHeight + 1);
  expect(
    ["auto", "scroll"].includes(before.overflowY),
    `${label} should own vertical scrolling, found overflow-y: ${before.overflowY}`,
  ).toBe(true);

  await section.hover();
  for (let i = 0; i < 8; i += 1) {
    await page.mouse.wheel(0, 1600);
    const current = await scrollMetrics(section);
    if (current.scrollTop + current.clientHeight >= current.scrollHeight - 2) {
      break;
    }
  }

  const after = await scrollMetrics(section);
  expect(
    after.scrollTop,
    `${label} should respond to wheel scrolling`,
  ).toBeGreaterThan(0);
  expect(
    after.scrollTop + after.clientHeight,
    `${label} should scroll to its last items`,
  ).toBeGreaterThanOrEqual(after.scrollHeight - 2);

  for (const target of targets) {
    await expect(target).toBeInViewport({ ratio: 0.2 });
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

      await expectWheelCanReach(page, page.locator(".sidebar-agents"), "Team", [
        page.locator('button[data-agent-slug="agent-24"]'),
        page.locator(".sidebar-agents .sidebar-add-btn"),
      ]);
      await expectWheelCanReach(
        page,
        page.locator(".sidebar-channels"),
        "Channels",
        [
          page.getByRole("button", { name: "Channel 24" }),
          page.locator(".sidebar-channels .sidebar-add-btn"),
        ],
      );
      await expectWheelCanReach(page, page.locator(".sidebar-apps"), "Apps", [
        appItems.last(),
      ]);

      await expectNoReactErrors(page, getErrors, "while scrolling the sidebar");
    });
  }
});
