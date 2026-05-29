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
  test("index redirects to the CEO subspace", async ({ page }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/");

    // v3 MVP default landing is the CEO's subspace (Chat tab), not #general
    // (see indexRoute.beforeLoad in lib/router.ts). Channels are demoted to
    // "Legacy".
    await expect(page).toHaveURL(/#\/agents\/ceo$/);
    await expect(page.getByTestId("agent-subspace")).toBeVisible({
      timeout: 10_000,
    });
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

  test("legacy /tasks URLs redirect to the Issues surface", async ({
    page,
  }) => {
    // Tasks were consolidated into Issues (#1002): top-level /tasks and
    // /tasks/$id redirect to the Issues list, which renders IssuesList
    // (kanban or its empty state). The /apps/tasks app-panel still mounts
    // separately and is covered by the app-panel matrix test above.
    for (const route of ["/#/tasks", "/#/tasks/task-7"]) {
      await page.goto(route);
      await expect(page).toHaveURL(/#\/issues$/, { timeout: 10_000 });
      await expect(
        page.locator("[data-testid^='issues-list']").first(),
      ).toBeVisible({ timeout: 10_000 });
      await expect(page.getByTestId("route-not-found")).toHaveCount(0);
    }
  });

  test("legacy workbench URLs redirect through to the Issues surface", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/#/apps/workbench/pm/tasks/task-7");

    // workbench → legacy task redirect → Issues surface (#1002 consolidation).
    await expect(page).toHaveURL(/#\/issues/, { timeout: 10_000 });
    await expect(page.getByTestId("route-not-found")).toHaveCount(0);
    await expect(
      page.locator("[data-testid^='issues-list']").first(),
    ).toBeVisible({ timeout: 10_000 });
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

    await expectCanonicalRoute(page, "/#/reviews", async (p) => {
      await expect(p.getByTestId("review-queue-surface")).toBeVisible();
    });
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
