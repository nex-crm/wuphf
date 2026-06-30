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
  test("index renders the operator surface (the product front door)", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/");

    // Operator-as-index: for an onboarded user the root URL renders the
    // operator product in place (no redirect), reached through the normal
    // boot + onboarding gate so it has a live broker token. The office Shell
    // is no longer the landing surface; it stays reachable via deep routes
    // (#/channels, #/wiki, #/tasks). The operator root mounting at the root
    // URL is the proof it landed (a redirect would mount another surface).
    // See isHomeRoute in routes/RootRoute.tsx.
    await expect(page).toHaveURL(/localhost:\d+\/(#\/?)?$/);
    await expect(page.getByTestId("operator-root")).toBeVisible({
      timeout: 10_000,
    });
    await expectNoReactErrors(page, getErrors, "while rendering /");
  });

  test("conversation routes mount their message surfaces", async ({ page }) => {
    // The /dm/$agent route was removed in the task-scoped restructure (DMs
    // fold into task channels). The channel conversation surface remains.
    await expectCanonicalRoute(page, "/#/channels/general", async (p) => {
      await expect(p.locator(".composer-input")).toHaveAttribute(
        "placeholder",
        "Message #general",
      );
    });
  });

  test("every registered app panel route mounts", async ({ page }) => {
    for (const appId of APP_PANEL_IDS) {
      // /#/apps/requests redirects to /tasks (the Inbox was consolidated
      // into the Task board; requests fold into its Needs-human lane)
      // instead of rendering a dedicated panel. Verify the redirect by
      // URL, not by panel testid.
      if (appId === "requests") {
        await page.goto(`/#/apps/${appId}`);
        await expect(page).toHaveURL(/#\/tasks$/, { timeout: 10_000 });
        continue;
      }
      await expectCanonicalRoute(page, `/#/apps/${appId}`, async (p) => {
        await expect(p.getByTestId(`app-page-${appId}`)).toBeVisible({
          timeout: 10_000,
        });
      });
    }
  });

  test("legacy workbench URLs redirect through to the Tasks surface", async ({
    page,
  }) => {
    const getErrors = collectReactErrors(page);
    await gotoRoute(page, "/#/apps/workbench/pm/tasks/task-7");

    // workbench → legacy task redirect → /tasks/$id detail (see
    // legacyWorkbenchTaskRoute in lib/router.ts).
    await expect(page).toHaveURL(/#\/tasks/, { timeout: 10_000 });
    await expect(page.getByTestId("route-not-found")).toHaveCount(0);
    await expectNoReactErrors(
      page,
      getErrors,
      "while redirecting legacy workbench task route",
    );
  });

  test("wiki routes mount their first-class surfaces", async ({ page }) => {
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

  test("a business task's channel redirects to its task; #general stays", async ({
    page,
    request,
  }) => {
    // ARCH-H1: a business task's channel is reached through the task, not as a
    // parallel chat surface, so /channels/$slug for a business-owned channel
    // redirects to the task detail. System channels (#general, owned by the
    // archived Backup & Migration task) stay directly readable.
    const resp = await request.post("/api/task-plan", {
      data: {
        channel: "general",
        created_by: "human",
        tasks: [{ title: "channel redirect probe", assignee: "ceo" }],
      },
    });
    expect(resp.ok(), `task-plan failed: ${resp.status()}`).toBeTruthy();
    const created = (await resp.json()) as {
      tasks?: { id?: string; channel?: string }[];
    };
    const biz = created.tasks?.[0];
    expect(
      biz?.channel,
      "business task should mint its own channel",
    ).toBeTruthy();
    expect(biz?.id, "business task should have an id").toBeTruthy();

    // Business channel → redirects to its task detail.
    await page.goto(`/#/channels/${biz?.channel}`);
    await expect(page).toHaveURL(new RegExp(`#/tasks/${biz?.id}`), {
      timeout: 10_000,
    });
    await expect(page.getByTestId("route-not-found")).toHaveCount(0);

    // System channel #general → stays on the conversation view (no redirect).
    await gotoRoute(page, "/#/channels/general");
    await expect(page.locator(".composer-input")).toHaveAttribute(
      "placeholder",
      "Message #general",
    );
  });
});
