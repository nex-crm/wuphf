import { afterEach, describe, expect, it } from "vitest";

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
import type { ChannelMeta } from "../stores/app";
import { useAppStore } from "../stores/app";
import {
  applyMatchToStore,
  deriveNavTarget,
  fillPath,
  isUnmatchedRoute,
  navTargetSearchString,
} from "./routeSync";

afterEach(() => {
  useAppStore.setState({
    currentChannel: "general",
    currentApp: null,
    channelMeta: {},
    activeThreadId: null,
    lastMessageId: null,
    clearedMessageIdsByChannel: {},
    unreadByChannel: {},
    activeAgentSlug: null,
    searchOpen: false,
    composerSearchInitialQuery: "",
    composerHelpOpen: false,
    onboardingComplete: false,
    wikiPath: null,
    wikiLookupQuery: null,
    notebookAgentSlug: null,
    notebookEntrySlug: null,
  });
});

describe("applyMatchToStore (URL → store)", () => {
  it("hydrates currentChannel and resets currentApp for /channels/$channelSlug", () => {
    useAppStore.setState({ currentApp: "tasks" });

    applyMatchToStore(channelRoute.id, { channelSlug: "launch" }, {});

    const s = useAppStore.getState();
    expect(s.currentChannel).toBe("launch");
    expect(s.currentApp).toBeNull();
    expect(s.lastMessageId).toBeNull();
  });

  it("hydrates DM channel slug and writes channelMeta for /dm/$agentSlug", () => {
    applyMatchToStore(dmRoute.id, { agentSlug: "pm" }, {});

    const s = useAppStore.getState();
    expect(s.currentChannel).toBe("human__pm");
    expect(s.currentApp).toBeNull();
    expect(s.channelMeta.human__pm).toEqual({
      type: "D",
      agentSlug: "pm",
    });
    expect(s.unreadByChannel.human__pm).toBe(0);
  });

  it("hydrates currentApp for /apps/$appId", () => {
    applyMatchToStore(appRoute.id, { appId: "tasks" }, {});

    expect(useAppStore.getState().currentApp).toBe("tasks");
  });

  it("hydrates wiki-lookup currentApp and query atomically", () => {
    let snapshots = 0;
    let interleaved = false;
    const unsub = useAppStore.subscribe((state) => {
      snapshots++;
      // The bridge race the reviewer flagged: a write where currentApp
      // and wikiLookupQuery land on different commits would let a
      // subscriber observe one without the other. Atomic setState must
      // emit a single notification with both fields populated together.
      const hasCurrentApp = state.currentApp === "wiki-lookup";
      const hasQuery = state.wikiLookupQuery === "renewal";
      if (hasCurrentApp !== hasQuery) interleaved = true;
    });

    applyMatchToStore(wikiLookupRoute.id, {}, { q: "renewal" });
    unsub();

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("wiki-lookup");
    expect(s.wikiLookupQuery).toBe("renewal");
    expect(snapshots).toBe(1);
    expect(interleaved).toBe(false);
  });

  it("preserves splat path for /wiki/$", () => {
    applyMatchToStore(wikiArticleRoute.id, { _splat: "companies/acme" }, {});

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("wiki");
    expect(s.wikiPath).toBe("companies/acme");
  });

  it("hydrates wiki index for /wiki", () => {
    useAppStore.setState({ currentApp: "tasks", wikiPath: "stale" });

    applyMatchToStore(wikiIndexRoute.id, {}, {});

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("wiki");
    expect(s.wikiPath).toBeNull();
  });

  it("hydrates notebook catalog for /notebooks", () => {
    useAppStore.setState({
      notebookAgentSlug: "stale",
      notebookEntrySlug: "stale",
    });

    applyMatchToStore(notebooksRoute.id, {}, {});

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("notebooks");
    expect(s.notebookAgentSlug).toBeNull();
    expect(s.notebookEntrySlug).toBeNull();
  });

  it("hydrates notebook agent for /notebooks/$agentSlug", () => {
    applyMatchToStore(notebookAgentRoute.id, { agentSlug: "pm" }, {});

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("notebooks");
    expect(s.notebookAgentSlug).toBe("pm");
    expect(s.notebookEntrySlug).toBeNull();
  });

  it("hydrates both notebook params for /notebooks/$agentSlug/$entrySlug", () => {
    applyMatchToStore(
      notebookEntryRoute.id,
      { agentSlug: "pm", entrySlug: "handoff" },
      {},
    );

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("notebooks");
    expect(s.notebookAgentSlug).toBe("pm");
    expect(s.notebookEntrySlug).toBe("handoff");
  });

  it("hydrates currentApp=reviews for /reviews", () => {
    applyMatchToStore(reviewsRoute.id, {}, {});

    expect(useAppStore.getState().currentApp).toBe("reviews");
  });

  it("is a no-op for unmatched routes (does not blank existing state)", () => {
    useAppStore.setState({ currentApp: "tasks", currentChannel: "launch" });

    applyMatchToStore(rootRoute.id, {}, {});
    applyMatchToStore("/this/route/does/not/exist", {}, {});

    const s = useAppStore.getState();
    expect(s.currentApp).toBe("tasks");
    expect(s.currentChannel).toBe("launch");
  });
});

describe("deriveNavTarget (store → URL)", () => {
  function slice(
    overrides: Partial<{
      currentApp: string | null;
      currentChannel: string;
      channelMeta: Record<string, ChannelMeta>;
      wikiPath: string | null;
      wikiLookupQuery: string | null;
      notebookAgentSlug: string | null;
      notebookEntrySlug: string | null;
    }> = {},
  ) {
    return {
      currentApp: null,
      currentChannel: "general",
      channelMeta: {},
      wikiPath: null,
      wikiLookupQuery: null,
      notebookAgentSlug: null,
      notebookEntrySlug: null,
      ...overrides,
    };
  }

  it("derives /channels/$slug for plain channels", () => {
    const target = deriveNavTarget(slice({ currentChannel: "launch" }));
    expect(target.to).toBe("/channels/$channelSlug");
    expect(fillPath(target)).toBe("/channels/launch");
  });

  it("derives /dm/$agent when channelMeta marks a DM", () => {
    const target = deriveNavTarget(
      slice({
        currentChannel: "human__pm",
        channelMeta: { human__pm: { type: "D", agentSlug: "pm" } },
      }),
    );
    expect(target.to).toBe("/dm/$agentSlug");
    expect(fillPath(target)).toBe("/dm/pm");
  });

  it("derives /apps/$appId for app-panel currentApp values", () => {
    const target = deriveNavTarget(slice({ currentApp: "tasks" }));
    expect(target.to).toBe("/apps/$appId");
    expect(fillPath(target)).toBe("/apps/tasks");
  });

  it("derives /wiki/$ for wiki article path", () => {
    const target = deriveNavTarget(
      slice({ currentApp: "wiki", wikiPath: "companies/acme" }),
    );
    expect(target.to).toBe("/wiki/$");
    // encodeURIComponent encodes `/` inside path segments — that's what
    // TanStack Router does for splat params too, so the comparison stays
    // consistent.
    expect(fillPath(target)).toBe("/wiki/companies%2Facme");
  });

  it("derives /wiki for wiki catalog when wikiPath is null", () => {
    const target = deriveNavTarget(slice({ currentApp: "wiki" }));
    expect(target.to).toBe("/wiki");
  });

  it("derives /wiki/lookup with q search param when query is present", () => {
    const target = deriveNavTarget(
      slice({ currentApp: "wiki-lookup", wikiLookupQuery: "renewal owner" }),
    );
    expect(target.to).toBe("/wiki/lookup");
    expect(target.search).toEqual({ q: "renewal owner" });
    expect(navTargetSearchString(target)).toBe("?q=renewal+owner");
  });

  it("derives /notebooks/$agentSlug/$entrySlug when both notebook params set", () => {
    const target = deriveNavTarget(
      slice({
        currentApp: "notebooks",
        notebookAgentSlug: "pm",
        notebookEntrySlug: "handoff",
      }),
    );
    expect(target.to).toBe("/notebooks/$agentSlug/$entrySlug");
    expect(fillPath(target)).toBe("/notebooks/pm/handoff");
  });
});

describe("isUnmatchedRoute", () => {
  it("flags root-only matches and undefined ids", () => {
    expect(isUnmatchedRoute(rootRoute.id)).toBe(true);
    expect(isUnmatchedRoute(undefined)).toBe(true);
    expect(isUnmatchedRoute("")).toBe(true);
  });

  it("does not flag real routes as unmatched", () => {
    expect(isUnmatchedRoute(channelRoute.id)).toBe(false);
    expect(isUnmatchedRoute(appRoute.id)).toBe(false);
    expect(isUnmatchedRoute(wikiArticleRoute.id)).toBe(false);
  });
});
