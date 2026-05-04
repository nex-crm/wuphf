import { describe, expect, it } from "vitest";

import {
  appRoute,
  channelRoute,
  dmRoute,
  notebookAgentRoute,
  notebookEntryRoute,
  notebooksRoute,
  reviewsRoute,
  rootRoute,
  wikiArticleRoute,
  wikiIndexRoute,
  wikiLookupRoute,
} from "../lib/router";
import { type CurrentRoute, deriveCurrentRoute } from "./useCurrentRoute";

// Pure dispatch unit test — pins the URL→state mapping for each route
// without spinning up a RouterProvider. Catches the kind of bug where
// someone refactors a route id or directChannelSlug ordering and a
// silent path falls into kind: "unknown".

describe("deriveCurrentRoute (URL → discriminated union)", () => {
  it.each<
    [
      string,
      string,
      Record<string, string | undefined>,
      Record<string, unknown>,
      CurrentRoute,
    ]
  >([
    [
      "channel",
      channelRoute.id,
      { channelSlug: "launch" },
      {},
      { kind: "channel", channelSlug: "launch" },
    ],
    [
      "channel default fallback",
      channelRoute.id,
      {},
      {},
      { kind: "channel", channelSlug: "general" },
    ],
    [
      "dm pairs lower__higher (human < pm)",
      dmRoute.id,
      { agentSlug: "pm" },
      {},
      { kind: "dm", agentSlug: "pm", channelSlug: "human__pm" },
    ],
    [
      "dm pairs lower__higher (ceo < human)",
      dmRoute.id,
      { agentSlug: "ceo" },
      {},
      { kind: "dm", agentSlug: "ceo", channelSlug: "ceo__human" },
    ],
    [
      "app",
      appRoute.id,
      { appId: "tasks" },
      {},
      { kind: "app", appId: "tasks" },
    ],
    ["wiki index", wikiIndexRoute.id, {}, {}, { kind: "wiki" }],
    [
      "wiki article splat",
      wikiArticleRoute.id,
      { _splat: "companies/acme" },
      {},
      { kind: "wiki-article", articlePath: "companies/acme" },
    ],
    [
      "wiki article empty splat short-circuits to wiki",
      wikiArticleRoute.id,
      { _splat: "" },
      {},
      { kind: "wiki" },
    ],
    [
      "wiki lookup with q",
      wikiLookupRoute.id,
      {},
      { q: "renewal owner" },
      { kind: "wiki-lookup", query: "renewal owner" },
    ],
    [
      "wiki lookup without q",
      wikiLookupRoute.id,
      {},
      {},
      { kind: "wiki-lookup", query: null },
    ],
    [
      "notebook catalog",
      notebooksRoute.id,
      {},
      {},
      { kind: "notebook-catalog" },
    ],
    [
      "notebook agent",
      notebookAgentRoute.id,
      { agentSlug: "pm" },
      {},
      { kind: "notebook-agent", agentSlug: "pm" },
    ],
    [
      "notebook entry",
      notebookEntryRoute.id,
      { agentSlug: "pm", entrySlug: "handoff" },
      {},
      { kind: "notebook-entry", agentSlug: "pm", entrySlug: "handoff" },
    ],
    ["reviews", reviewsRoute.id, {}, {}, { kind: "reviews" }],
    [
      "unmatched route id falls through to unknown",
      "/this/route/does/not/exist",
      {},
      {},
      { kind: "unknown" },
    ],
    [
      "root-only id (e.g. /console after legacy alias removal) is unknown",
      rootRoute.id,
      {},
      {},
      { kind: "unknown" },
    ],
  ])("%s", (_label, routeId, params, search, expected) => {
    expect(deriveCurrentRoute(routeId, params, search)).toEqual(expected);
  });

  it("ignores non-string q on wiki-lookup", () => {
    // TanStack's validateSearch already narrows q to string|undefined,
    // but defense-in-depth: a malformed search shouldn't blow up the
    // dispatch.
    expect(deriveCurrentRoute(wikiLookupRoute.id, {}, { q: 42 })).toEqual({
      kind: "wiki-lookup",
      query: null,
    });
    expect(
      deriveCurrentRoute(wikiLookupRoute.id, {}, { q: undefined }),
    ).toEqual({ kind: "wiki-lookup", query: null });
  });
});
