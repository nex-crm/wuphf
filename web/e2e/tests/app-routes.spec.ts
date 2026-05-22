import { expect, type Page, test } from "@playwright/test";

import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

// Primary tools render inline as rail icons; secondary tools live behind
// the More-tools popover and surface their active state through the
// trigger button instead of the per-app icon.
const PRIMARY_TOOL_IDS = new Set([
  "overview",
  "wiki",
  "calendar",
  "skills",
]);

const APP_CASES = [
  {
    app: "console",
    label: "Console",
    content: /wuphf office|Slash/i,
  },
  // Tasks sidebar entry was retired; /#/apps/tasks now redirects to
  // /#/issues. Coverage for that redirect lives in route-matrix.spec.ts.
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
    app: "calendar",
    label: "Calendar",
    content: /Loading calendar|Could not load calendar|Nothing scheduled|Mon|Tue|Wed/i,
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
    label: "Access & Health",
    content: /Checking health|Could not reach health endpoint|Access & Health/i,
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

async function fetchBrokerCommandNames(page: Page): Promise<string[]> {
  return page.evaluate(async () => {
    const response = await fetch("/api/commands");
    if (!response.ok) {
      throw new Error(`GET /api/commands failed: ${response.status}`);
    }
    const commands = (await response.json()) as Array<{ name?: unknown }>;
    return commands
      .map((command) => {
        if (typeof command.name !== "string" || !command.name.trim()) {
          throw new Error("GET /api/commands returned an invalid command row");
        }
        return `/${command.name}`;
      })
      .sort((a, b) => a.localeCompare(b));
  });
}

test.describe("app route isolation", () => {
  test("each app renders its own page and lights up its rail tool", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);

    for (const appCase of APP_CASES) {
      await page.goto(`/#/apps/${appCase.app}`);
      await waitForReactMount(page);
      await expectAppRoute(page, appCase.app, appCase.content);
      // Tools moved from the sidebar to the WorkspaceRail. Primary tools
      // (overview / wiki / calendar / skills) render inline and flip
      // `aria-current="page"`. Secondary tools live behind the
      // More-tools popover — when one is active, the trigger button
      // takes the active state instead.
      if (PRIMARY_TOOL_IDS.has(appCase.app)) {
        await expect(
          page.getByTestId(`workspace-rail-tool-${appCase.app}`),
        ).toHaveAttribute("aria-current", "page");
      } else {
        await expect(
          page.getByTestId("workspace-rail-more-trigger"),
        ).toHaveAttribute("aria-current", "page");
      }
    }

    // Switch between two app routes via the rail to confirm app-route
    // swapping still works end-to-end. Console and Graph are both
    // secondary tools, so they live in the More-tools popover.
    await page.goto("/#/apps/console");
    await waitForReactMount(page);
    await page.getByTestId("workspace-rail-more-trigger").click();
    await page.getByTestId("workspace-rail-tool-graph").click();
    await expectAppRoute(page, "graph", /Entity Graph/i);
    await expect(page.getByTestId("console-app")).toHaveCount(0);

    await page.getByTestId("workspace-rail-more-trigger").click();
    await page.getByTestId("workspace-rail-tool-console").click();
    await expectAppRoute(page, "console", /wuphf office|Slash/i);
    await expect(page.getByTestId("app-page-graph")).toHaveCount(0);

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

    await consoleApp.locator('[data-command="/reset-dm"]').click();
    await expect(input).toHaveValue("/reset-dm ");
    await input.fill("");

    await consoleApp.locator('[data-command="/ask"]').click();
    await expect(input).toHaveValue("/ask ");
    await input.fill("/ask route-check");
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

  test("console renders every broker command", async ({ page }) => {
    const getErrors = collectReactErrors(page);

    await page.goto("/#/apps/console");
    await waitForReactMount(page);

    const expectedCommands = await fetchBrokerCommandNames(page);
    expect(expectedCommands).toContain("/reset-dm");

    const consoleApp = page.getByTestId("console-app");
    const rows = consoleApp.locator(".console-command");
    await expect(rows).toHaveCount(expectedCommands.length, {
      timeout: 10_000,
    });

    const renderedCommands = (
      await rows.evaluateAll((nodes) =>
        nodes.map((node) => node.getAttribute("data-command") ?? ""),
      )
    ).sort((a, b) => a.localeCompare(b));

    expect(renderedCommands).toEqual(expectedCommands);
    await expectNoReactErrors(
      page,
      getErrors,
      "while rendering console commands",
    );
  });
});
