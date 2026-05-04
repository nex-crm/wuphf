import { describe, expect, it } from "vitest";

import {
  type LegacyRouteState,
  legacyRouteToStatePatch,
  legacyStateToHash,
  parseLegacyHash,
} from "./legacyHash";

const baseState: LegacyRouteState = {
  currentApp: null,
  currentChannel: "general",
  channelMeta: {},
  wikiPath: null,
  wikiLookupQuery: null,
  notebookAgentSlug: null,
  notebookEntrySlug: null,
};

describe("parseLegacyHash", () => {
  it.each([
    ["#/channels/launch%20plan", { view: "channel", channel: "launch plan" }],
    ["#/dm/pm", { view: "dm", agent: "pm" }],
    ["#/apps/health-check", { view: "app", app: "health-check" }],
    ["#/console", { view: "app", app: "console" }],
    ["#/threads", { view: "app", app: "threads" }],
    ["#/wiki/companies/acme", { view: "wiki", articlePath: "companies/acme" }],
    ["#/wiki", { view: "wiki", articlePath: null }],
    [
      "#/wiki/lookup?q=renewal%20owner",
      { view: "wiki-lookup", query: "renewal owner" },
    ],
    [
      "#/notebooks/pm/handoff",
      { view: "notebooks", agentSlug: "pm", entrySlug: "handoff" },
    ],
    ["#/notebooks/pm", { view: "notebooks", agentSlug: "pm", entrySlug: null }],
    ["#/notebooks", { view: "notebooks", agentSlug: null, entrySlug: null }],
    ["#/reviews", { view: "reviews" }],
    ["#/unknown", { view: "channel", channel: "general" }],
  ])("parses %s", (hash, expected) => {
    expect(parseLegacyHash(hash)).toEqual(expected);
  });

  it("prefers page search params for the current wiki lookup compatibility path", () => {
    expect(parseLegacyHash("#/wiki/lookup?q=hash", "?q=page")).toEqual({
      view: "wiki-lookup",
      query: "page",
    });
  });
});

describe("legacyStateToHash", () => {
  it.each([
    [{ currentChannel: "launch plan" }, "#/channels/launch%20plan"],
    [
      {
        currentChannel: "ceo__human",
      },
      "#/dm/ceo",
    ],
    [{ currentApp: "settings" }, "#/apps/settings"],
    [
      { currentApp: "wiki", wikiPath: "companies/acme" },
      "#/wiki/companies/acme",
    ],
    [
      { currentApp: "wiki", wikiPath: "people/sarah chen.md" },
      "#/wiki/people/sarah%20chen.md",
    ],
    [
      { currentApp: "wiki-lookup", wikiLookupQuery: "renewal owner" },
      "#/wiki/lookup?q=renewal%20owner",
    ],
    [{ currentApp: "notebooks" }, "#/notebooks"],
    [{ currentApp: "notebooks", notebookAgentSlug: "pm" }, "#/notebooks/pm"],
    [
      {
        currentApp: "notebooks",
        notebookAgentSlug: "pm",
        notebookEntrySlug: "handoff notes",
      },
      "#/notebooks/pm/handoff%20notes",
    ],
    [{ currentApp: "reviews" }, "#/reviews"],
  ])("serializes %# to %s", (state, expected) => {
    expect(
      legacyStateToHash({
        ...baseState,
        channelMeta: {
          ceo__human: { type: "D", agentSlug: "ceo" },
        },
        ...state,
      }),
    ).toBe(expected);
  });

  it("round-trips the canonical wiki lookup hash emitted by state serialization", () => {
    const hash = legacyStateToHash({
      ...baseState,
      currentApp: "wiki-lookup",
      wikiLookupQuery: "who owns renewal?",
    });

    expect(parseLegacyHash(hash)).toEqual({
      view: "wiki-lookup",
      query: "who owns renewal?",
    });
  });
});

describe("legacyRouteToStatePatch", () => {
  it("derives the broker direct-channel slug for DM routes", () => {
    expect(legacyRouteToStatePatch({ view: "dm", agent: "pm" })).toEqual({
      kind: "dm",
      agentSlug: "pm",
      channelSlug: "human__pm",
    });
  });
});
