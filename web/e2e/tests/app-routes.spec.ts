import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

const APP_CASES = [
  // Tasks were consolidated into Issues (#1002): no "Tasks" sidebar slot
  // remains. /tasks redirects are covered in route-matrix.spec.ts.
  // Phase 2b retired the standalone RequestsApp surface; /apps/requests
  // now redirects to /inbox. Coverage moved to the unified-inbox E2E.
  {
    app: "graph",
    label: "Graph",
    content: /Entity Graph/i,
  },
  {
    app: "policies",
    label: "Policies",
    content: /Office operating rules/i,
  },
  {
    app: "routines",
    label: "Scheduled Tasks",
    content:
      /Loading scheduled tasks|Could not load|No scheduled tasks|Scheduled Tasks/i,
  },
  {
    app: "skills",
    label: "Skills",
    content: /Loading skills|Could not load skills|Skills/i,
  },
  {
    // The `activity` app id keeps its historical slug; the v3 sidebar label
    // is "Dashboard" (see APP_LABELS in routeRegistry.ts).
    app: "activity",
    label: "Dashboard",
    content: /Loading office activity|Office activity/i,
  },
  {
    app: "health-check",
    label: "Access & Health",
    content: /Checking health|Could not reach health endpoint|Access & Health/i,
  },
  {
    app: "integrations",
    label: "Integrations",
    content: /Integrations|PATCH BAY/i,
  },
] as const;

async function expectAppRoute(
  page: Page,
  app: (typeof APP_CASES)[number]["app"],
  content: RegExp,
): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`#/apps/${app}$`));
  const appPage = page.getByTestId(`app-page-${app}`);
  await expect(appPage).toBeVisible({
    timeout: 10_000,
  });
  await expect(appPage).toContainText(content, { timeout: 10_000 });
}

test.describe("app route isolation", () => {
  test("each sidebar app renders its own page", async ({ page }) => {
    const getErrors = collectReactErrors(page);

    for (const appCase of APP_CASES) {
      await page.goto(`/#/apps/${appCase.app}`);
      await waitForReactMount(page);
      await expectAppRoute(page, appCase.app, appCase.content);
      await expect(
        page.locator(".sidebar-apps .sidebar-item.active"),
      ).toContainText(appCase.label);
    }

    // Switch between two real sidebar apps to confirm app-route swapping
    // still works.
    await page.goto("/#/apps/graph");
    await waitForReactMount(page);
    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Policies" })
      .click();
    await expectAppRoute(page, "policies", /Office operating rules/i);
    await expect(page.getByTestId("app-page-graph")).toHaveCount(0);

    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Graph" })
      .click();
    await expectAppRoute(page, "graph", /Entity Graph/i);
    await expect(page.getByTestId("app-page-policies")).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "while switching app routes");
  });
});
