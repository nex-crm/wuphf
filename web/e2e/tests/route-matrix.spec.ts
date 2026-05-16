import { expect, type Page, test } from "@playwright/test";

import { APP_PANEL_IDS } from "../../src/routes/routeRegistry";
import {
  collectReactErrors,
  expectNoReactErrors,
  waitForReactMount,
} from "./_helpers";

async function gotoRoute(page: Page, route: string): Promise<void> {
  await page.goto(route);
  await waitForReactMount(page);
}

async function expectCanonicalRoute(
  page: Page,
  route: string,
  assertMounted: (targetPage: Page) => Promise<void>,
): Promise<void> {
  const getErrors = collectReactErrors(page);
  await gotoRoute(page, route);
  await expect(page.getByTestId("route-not-found")).toHaveCount(0);
  await assertMounted(page);
  await expectNoReactErrors(page, getErrors, `while rendering ${route}`);
}

test.describe("canonical route matrix", () => {
  test("index redirects to the default channel", async ({ page }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/");

    await expect(page).toHaveURL(/#\/channels\/general$/);
    await expect(page.locator(".composer-input")).toHaveAttribute(
      "placeholder",
      "Message #general",
    );
    await expectNoReactErrors(page, getErrors, "while redirecting /");
  });

  test("conversation routes mount their message surfaces", async ({ page }) => {
    await expectCanonicalRoute(page, "/#/channels/general", async (p) => {
      await expect(p.locator(".composer-input")).toHaveAttribute(
        "placeholder",
        "Message #general",
      );
    });

    await expectCanonicalRoute(page, "/#/dm/pm", async (p) => {
      await expect(p.locator(".composer-input")).toHaveAttribute(
        "placeholder",
        "Message #human__pm",
      );
      // The DM workbench surfaces the agent's SSE feed under a "Live stream"
      // collapsible section (see AgentWorkbenchPane). The title is the
      // user-visible affordance proving the stream surface mounted on the
      // DM route.
      await expect(p.getByText("Live stream")).toBeVisible();
    });
  });

  test("every registered app panel route mounts", async ({ page }) => {
    for (const appId of APP_PANEL_IDS) {
      // Phase 2b: /#/apps/requests redirects to /inbox instead of
      // rendering a dedicated panel. Verify the redirect by URL,
      // not by panel testid.
      if (appId === "requests") {
        await page.goto(`/#/apps/${appId}`);
        await expect(page).toHaveURL(/#\/inbox$/, { timeout: 10_000 });
        continue;
      }
      await expectCanonicalRoute(page, `/#/apps/${appId}`, async (p) => {
        await expect(p.getByTestId(`app-page-${appId}`)).toBeVisible({
          timeout: 10_000,
        });
      });
    }
  });

  test("task detail route variants mount from URL state", async ({ page }) => {
    for (const route of [
      "/#/tasks",
      "/#/tasks/task-7",
      "/#/apps/tasks/task-7",
    ]) {
      await expectCanonicalRoute(page, route, async (p) => {
        await expect(p.getByTestId("tasks-app")).toBeVisible();
      });
    }
  });

  test("legacy workbench URLs redirect to task routes", async ({ page }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/#/apps/workbench/pm/tasks/task-7");

    await expect(page).toHaveURL(/#\/tasks\/task-7$/);
    await expect(page.getByTestId("route-not-found")).toHaveCount(0);
    await expect(page.getByTestId("tasks-app")).toBeVisible();
    await expectNoReactErrors(
      page,
      getErrors,
      "while redirecting legacy workbench task route",
    );
  });

  test("wiki, notebook, and review routes mount their first-class surfaces", async ({
    page,
  }) => {
    await expectCanonicalRoute(page, "/#/wiki", async (p) => {
      await expect(p.getByTestId("wiki-root")).toBeVisible();
    });

    await expectCanonicalRoute(page, "/#/wiki/lookup?q=renewal", async (p) => {
      await expect(p.locator(".wk-cited-answer")).toBeVisible({
        timeout: 10_000,
      });
    });

    await expectCanonicalRoute(page, "/#/wiki/companies/acme", async (p) => {
      await expect(p.getByTestId("wiki-root")).toBeVisible();
    });

    await expectCanonicalRoute(page, "/#/notebooks", async (p) => {
      await expect(p.getByTestId("notebook-surface")).toBeVisible();
    });

    await expectCanonicalRoute(page, "/#/notebooks/pm", async (p) => {
      await expect(p.getByTestId("notebook-surface")).toBeVisible();
    });

    await expectCanonicalRoute(page, "/#/notebooks/pm/handoff", async (p) => {
      await expect(p.getByTestId("notebook-surface")).toBeVisible();
    });

    // Phase 2b: /#/reviews now redirects to /inbox instead of mounting
    // ReviewQueueKanban. Verify the URL change, not the briefly-mounted
    // InboxRedirect testid (it unmounts as soon as the redirect fires).
    await page.goto("/#/reviews");
    await expect(page).toHaveURL(/#\/inbox$/, { timeout: 10_000 });
  });

  test("dropped legacy aliases and unknown routes render not found", async ({
    page,
  }) => {
    for (const route of ["/#/console", "/#/threads", "/#/missing-route"]) {
      const getErrors = collectReactErrors(page);
      await gotoRoute(page, route);
      await expect(page.getByTestId("route-not-found")).toBeVisible();
      await expectNoReactErrors(page, getErrors, `while rendering ${route}`);
    }
  });
});
