import { createMemoryHistory } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import {
  appRoute,
  channelRoute,
  consoleAliasRoute,
  createAppRouter,
  dmRoute,
  indexRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  threadsAliasRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import {
  APP_PANEL_IDS,
  isAppPanelId,
  ROUTE_CONTRACTS,
  ROUTE_PATHS,
  SIDEBAR_APP_IDS,
  sidebarAppRouteKind,
} from "./routeRegistry";

function unique<T>(values: readonly T[]): T[] {
  return Array.from(new Set(values));
}

describe("route registry", () => {
  it("keeps app panel ids unique and validated", () => {
    expect(unique(APP_PANEL_IDS)).toHaveLength(APP_PANEL_IDS.length);
    expect(isAppPanelId("tasks")).toBe(true);
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
    ["/dm/pm", dmRoute.id],
    ["/apps/tasks", appRoute.id],
    ["/console", consoleAliasRoute.id],
    ["/threads", threadsAliasRoute.id],
    ["/wiki", wikiIndexRoute.id],
    ["/wiki/lookup", wikiLookupRoute.id],
    ["/wiki/companies/acme", wikiArticleRoute.id],
    ["/notebooks", notebooksRoute.id],
    ["/notebooks/pm", notebookAgentRoute.id],
    ["/notebooks/pm/handoff", notebookEntryRoute.id],
    ["/reviews", reviewsRoute.id],
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
});
