import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

async function expectAppRoute(
  page: Page,
  app: "console" | "tasks",
): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`#/apps/${app}$`));
  await expect(page.getByTestId(`${app}-app`)).toBeVisible({
    timeout: 10_000,
  });
}

test.describe("app route isolation", () => {
  test("console and tasks render as separate app pages", async ({ page }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/#/apps/console");
    await waitForReactMount(page);
    await expectAppRoute(page, "console");
    await expect(page.getByTestId("tasks-app")).toHaveCount(0);

    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Tasks" })
      .click();
    await expectAppRoute(page, "tasks");
    await expect(page.getByTestId("console-app")).toHaveCount(0);

    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Console" })
      .click();
    await expectAppRoute(page, "console");
    await expect(page.getByTestId("tasks-app")).toHaveCount(0);

    await page.goto("/#/apps/tasks");
    await expectAppRoute(page, "tasks");
    await expect(page.getByTestId("console-app")).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "while switching app routes");
  });
});
