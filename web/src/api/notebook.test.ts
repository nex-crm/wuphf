import { describe, expect, it } from "vitest";

import {
  fetchAgentEntries,
  fetchCatalog,
  fetchEntry,
  fetchReview,
  fetchReviews,
  promoteEntry,
} from "./notebook";

/**
 * These tests exercise the default (mock) code path. `VITE_NOTEBOOK_MOCK`
 * is undefined in the test env, which treats as TRUE (mock on).
 */

describe("notebook API — mock mode", () => {
  it("fetches a catalog with expected agent count", async () => {
    const catalog = await fetchCatalog();
    expect(catalog.total_agents).toBe(catalog.agents.length);
    expect(catalog.agents.some((a) => a.agent_slug === "pm")).toBe(true);
    expect(catalog.total_entries).toBeGreaterThan(0);
  });

  it("loads entries for a known agent", async () => {
    const { agent, entries } = await fetchAgentEntries("pm");
    expect(agent?.agent_slug).toBe("pm");
    expect(entries.length).toBeGreaterThanOrEqual(6);
    expect(entries.every((e) => e.agent_slug === "pm")).toBe(true);
  });

  it("returns empty for an agent with no entries", async () => {
    const { agent, entries } = await fetchAgentEntries("researcher");
    expect(agent?.agent_slug).toBe("researcher");
    expect(entries).toHaveLength(0);
  });

  it("loads a specific entry by slug", async () => {
    const entry = await fetchEntry("pm", "customer-acme-rough-notes");
    expect(entry).not.toBeNull();
    expect(entry?.title).toBe("Customer Acme — rough notes");
  });

  it("returns null when the entry is unknown", async () => {
    const entry = await fetchEntry("pm", "does-not-exist");
    expect(entry).toBeNull();
  });

  it("fetches the review list with all five Kanban states represented", async () => {
    const reviews = await fetchReviews();
    expect(reviews.length).toBeGreaterThanOrEqual(4);
    const states = new Set(reviews.map((r) => r.state));
    expect(states.has("pending")).toBe(true);
    expect(states.has("in-review")).toBe(true);
    expect(states.has("changes-requested")).toBe(true);
    expect(states.has("approved")).toBe(true);
  });

  it("loads a review by id", async () => {
    const reviews = await fetchReviews();
    const target = reviews[0];
    const fetched = await fetchReview(target.id);
    expect(fetched?.id).toBe(target.id);
  });

  it("promoteEntry returns a synthetic pending review card in mock mode", async () => {
    const review = await promoteEntry("pm", "customer-acme-rough-notes");
    expect(review).not.toBeNull();
    expect(review?.state).toBe("pending");
    expect(review?.agent_slug).toBe("pm");
  });

  it("promoteEntry returns null for unknown entry", async () => {
    const review = await promoteEntry("pm", "missing-slug");
    expect(review).toBeNull();
  });
});
