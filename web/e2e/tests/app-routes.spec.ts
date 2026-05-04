import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

const APP_CASES = [
  {
    app: "console",
    label: "Console",
    content: /wuphf office|Slash/i,
  },
  {
    app: "tasks",
    label: "Tasks",
    content: /Loading tasks|No tasks yet|Office tasks|Could not load tasks/i,
  },
  {
    app: "requests",
    label: "Requests",
    content:
      /Loading requests|No requests right now|Pending|Answered|Failed to load requests/i,
  },
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
    app: "calendar",
    label: "Calendar",
    content:
      /Loading schedule|Could not load schedule|Schedule|No scheduled jobs/i,
  },
  {
    app: "skills",
    label: "Skills",
    content: /Loading skills|Could not load skills|Skills/i,
  },
  {
    app: "activity",
    label: "Activity",
    content: /Loading office activity|Office activity/i,
  },
  {
    app: "receipts",
    label: "Receipts",
    content: /Receipts|Loading|No receipts|Could not load receipts/i,
  },
  {
    app: "health-check",
    label: "Health Check",
    content: /Checking health|Could not reach health endpoint|Health Check/i,
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

    await page.goto("/#/apps/console");
    await waitForReactMount(page);
    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Tasks" })
      .click();
    await expectAppRoute(
      page,
      "tasks",
      /Loading tasks|No tasks yet|Office tasks|Could not load tasks/i,
    );
    await expect(page.getByTestId("console-app")).toHaveCount(0);

    await page
      .locator(".sidebar-apps .sidebar-item", { hasText: "Console" })
      .click();
    await expectAppRoute(page, "console", /wuphf office|Slash/i);
    await expect(page.getByTestId("tasks-app")).toHaveCount(0);

    await expectNoReactErrors(page, getErrors, "while switching app routes");
  });

  test("console input accepts typing and slash command insertion", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/#/apps/console");
    await waitForReactMount(page);

    const consoleApp = page.getByTestId("console-app");
    const input = consoleApp.getByTestId("console-input");
    await expect(input).toBeVisible();
    await expect(consoleApp.getByText("Apps", { exact: true })).toHaveCount(0);

    await input.click();
    await input.pressSequentially("hello");
    await expect(input).toHaveValue("hello");
    await input.fill("");

    await consoleApp.locator(".console-command", { hasText: "/ask" }).click();
    await expect(input).toHaveValue("/ask ");
    await input.pressSequentially("route-check");
    await expect(input).toHaveValue("/ask route-check");
    await input.press("Enter");
    await expect(input).toHaveValue("");
    await expect(
      consoleApp.locator(".console-line-content", {
        hasText: "/ask route-check",
      }),
    ).toBeVisible();

    await expectNoReactErrors(page, getErrors, "while using console input");
  });
});
