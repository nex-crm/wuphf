import { createMemoryHistory } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import {
  agentDetailRoute,
  agentsRoute,
  appRoute,
  appTaskDetailRoute,
  channelRoute,
  createAppRouter,
  inboxRoute,
  indexRoute,
  legacyWorkbenchAgentRoute,
  legacyWorkbenchRoute,
  legacyWorkbenchTaskRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  rootRoute,
  taskDecisionRoute,
  taskDetailRoute,
  tasksRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import {
  APP_LABELS,
  APP_PANEL_IDS,
  FIRST_CLASS_APP_IDS,
  isAppPanelId,
  isFirstClassAppId,
  ROUTE_CONTRACTS,
  ROUTE_PATHS,
  SIDEBAR_APP_IDS,
  SIDEBAR_TOOLS,
  sidebarAppRouteKind,
} from "./routeRegistry";

function unique<T>(values: readonly T[]): T[] {
  return Array.from(new Set(values));
}

describe("route registry", () => {
  it("keeps app panel ids unique and validated", () => {
    expect(unique(APP_PANEL_IDS)).toHaveLength(APP_PANEL_IDS.length);
    expect(isAppPanelId("console")).toBe(true);
    // `tasks` is now a first-class surface (lives at /tasks), not an
    // /apps/$id panel.
    expect(isAppPanelId("tasks")).toBe(false);
    expect(isAppPanelId("wiki")).toBe(false);
    expect(isAppPanelId("notebooks")).toBe(false);
  });

  it("classifies every sidebar app as either a routed panel or a first-class surface", () => {
    expect(SIDEBAR_APP_IDS).toContain("wiki");
    expect(SIDEBAR_APP_IDS).toContain("settings");

    for (const id of SIDEBAR_APP_IDS) {
      expect(sidebarAppRouteKind(id), id).not.toBeNull();
    }
  });

  it("provides a human label for every routed sidebar id", () => {
    for (const id of APP_PANEL_IDS) {
      expect(APP_LABELS[id], id).toBeTruthy();
    }
    for (const id of FIRST_CLASS_APP_IDS) {
      expect(APP_LABELS[id], id).toBeTruthy();
    }
  });

  it("drives sidebar TOOLS labels from the route registry", () => {
    // Every entry in the rendered TOOLS list is either an app-panel id
    // (routed at /apps/$id) or a first-class app id (routed at /$id).
    // Labels come from APP_LABELS, never an out-of-band constant.
    for (const tool of SIDEBAR_TOOLS) {
      expect(sidebarAppRouteKind(tool.id), tool.id).toBe(tool.kind);
      expect(tool.label).toBe(APP_LABELS[tool.id]);
    }
  });

  it("recognises first-class app ids that live outside /apps", () => {
    expect(isFirstClassAppId("wiki")).toBe(true);
    expect(isFirstClassAppId("inbox")).toBe(true);
    expect(isFirstClassAppId("tasks")).toBe(true);
    expect(isFirstClassAppId("console")).toBe(false);
  });

  it("keeps the planned route contracts unique", () => {
    const keys = ROUTE_CONTRACTS.map((route) => route.key);
    const paths = ROUTE_CONTRACTS.map((route) => route.path);

    expect(unique(keys)).toHaveLength(keys.length);
    expect(unique(paths)).toHaveLength(paths.length);
    expect(keys).toEqual(Object.keys(ROUTE_PATHS));
  });
});

describe("TanStack route tree", () => {
  const expectedLeafRoutes = [
    ["/", indexRoute.id],
    ["/channels/launch", channelRoute.id],
    ["/agents", agentsRoute.id],
    ["/agents/pm", agentDetailRoute.id],
    ["/apps/tasks", appRoute.id],
    ["/tasks", tasksRoute.id],
    ["/tasks/task-7", taskDetailRoute.id],
    ["/apps/tasks/task-7", appTaskDetailRoute.id],
    ["/apps/workbench", legacyWorkbenchRoute.id],
    ["/apps/workbench/pm", legacyWorkbenchAgentRoute.id],
    ["/apps/workbench/pm/tasks/task-7", legacyWorkbenchTaskRoute.id],
    ["/wiki", wikiIndexRoute.id],
    ["/wiki/lookup", wikiLookupRoute.id],
    ["/wiki/companies/acme", wikiArticleRoute.id],
    ["/notebooks", notebooksRoute.id],
    ["/notebooks/pm", notebookAgentRoute.id],
    ["/notebooks/pm/handoff", notebookEntryRoute.id],
    ["/reviews", reviewsRoute.id],
    ["/inbox", inboxRoute.id],
    ["/task/task-2741", taskDecisionRoute.id],
  ] as const;

  it.each(
    expectedLeafRoutes,
  )("matches %s to the expected leaf route", (path, routeId) => {
    const router = createAppRouter(
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const leaf = router.matchRoutes(path).at(-1);

    expect(leaf?.routeId).toBe(routeId);
  });

  it("captures wiki article splats for nested article paths", () => {
    const router = createAppRouter(
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const leaf = router.matchRoutes("/wiki/companies/acme").at(-1);

    expect(leaf?.routeId).toBe(wikiArticleRoute.id);
    expect(leaf?.params).toMatchObject({
      _splat: "companies/acme",
      "*": "companies/acme",
    });
  });

  it("uses hash history by default for static-file-server compatibility", () => {
    const href = createAppRouter().history.createHref("/channels/general");

    expect(href).toBe("/#/channels/general");
  });

  it.each([
    ["/console"],
    ["/threads"],
  ])("does not match the dropped legacy alias %s", (path) => {
    const router = createAppRouter(
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const leaf = router.matchRoutes(path).at(-1);

    // /console and /threads were temporary aliases during phase 0. They
    // should now only match the root (no leaf), so the canonical
    // /apps/console URL is the single source of truth (and /apps/threads
    // is no longer a recognized app panel — see APP_PANEL_IDS).
    expect(leaf?.routeId).toBe(rootRoute.id);
  });

  it.each([
    ["/apps/wiki", "wiki"],
    ["/apps/inbox", "inbox"],
  ])("routes %s through the generic app route so MainContent can redirect to the first-class surface", (path, expectedAppId) => {
    // /apps/wiki and /apps/inbox match the generic /apps/$appId route
    // because the router has no opinion on which app ids are
    // first-class. MainContent narrows via isFirstClassAppId() and
    // mounts FirstClassAppRedirect, which navigates to /wiki or
    // /inbox respectively. Asserting both halves here keeps the
    // aliasing contract pinned: a future drop of either side would
    // silently bring the "Page not found" regression back.
    const router = createAppRouter(
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const leaf = router.matchRoutes(path).at(-1);

    expect(leaf?.routeId).toBe(appRoute.id);
    expect((leaf?.params as { appId?: string })?.appId).toBe(expectedAppId);
    expect(isFirstClassAppId(expectedAppId)).toBe(true);
  });

  it("treats /apps/threads as an unknown app panel", () => {
    // After `threads` was removed from APP_PANEL_IDS, the URL still
    // matches the generic `/apps/$appId` route — the router has no
    // opinion on which app ids are first-party. MainContent
    // narrows via isAppPanelId() and renders UnknownAppPanel for
    // anything that fails the check.
    const router = createAppRouter(
      createMemoryHistory({ initialEntries: ["/"] }),
    );
    const leaf = router.matchRoutes("/apps/threads").at(-1);

    expect(leaf?.routeId).toBe(appRoute.id);
    expect((leaf?.params as { appId?: string })?.appId).toBe("threads");
  });
});
