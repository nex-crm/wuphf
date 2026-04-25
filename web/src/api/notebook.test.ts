import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as client from "./client";
import {
  fetchAgentEntries,
  fetchCatalog,
  fetchEntry,
  fetchReview,
  fetchReviews,
  promoteEntry,
} from "./notebook";

describe("notebook API - mock mode", () => {
  beforeEach(() => {
    vi.stubEnv("VITE_NOTEBOOK_MOCK", "true");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("fetches a catalog with expected agent count", async () => {
    const catalog = await fetchCatalog();
    expect(catalog.total_agents).toBe(catalog.agents.length);
    expect(catalog.agents.some((a) => a.agent_slug === "planner")).toBe(true);
    expect(catalog.total_entries).toBeGreaterThan(0);
  });

  it("loads entries for a known agent", async () => {
    const { agent, entries } = await fetchAgentEntries("planner");
    expect(agent?.agent_slug).toBe("planner");
    expect(entries.length).toBeGreaterThanOrEqual(3);
    expect(entries.every((e) => e.agent_slug === "planner")).toBe(true);
  });

  it("returns empty for an agent with no entries", async () => {
    const { agent, entries } = await fetchAgentEntries("researcher");
    expect(agent?.agent_slug).toBe("researcher");
    expect(entries).toHaveLength(0);
  });

  it("loads a specific entry by slug", async () => {
    const entry = await fetchEntry("planner", "reliable-handoffs");
    expect(entry).not.toBeNull();
    expect(entry?.title).toBe("Reliable handoffs");
  });

  it("returns null when the entry is unknown", async () => {
    const entry = await fetchEntry("planner", "does-not-exist");
    expect(entry).toBeNull();
  });

  it("fetches the review list with all active Kanban states represented", async () => {
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
    const [target] = reviews;
    const fetched = await fetchReview(target.id);
    expect(fetched?.id).toBe(target.id);
  });

  it("promoteEntry returns a synthetic pending review card in explicit mock mode", async () => {
    const review = await promoteEntry("planner", "reliable-handoffs");
    expect(review).not.toBeNull();
    expect(review?.state).toBe("pending");
    expect(review?.agent_slug).toBe("planner");
    expect(review?.proposed_wiki_path).toBe(
      "team/drafts/planner-reliable-handoffs.md",
    );
  });

  it("promoteEntry returns null for unknown entry", async () => {
    const review = await promoteEntry("planner", "missing-slug");
    expect(review).toBeNull();
  });
});

describe("notebook API - live backend mode", () => {
  const ts = "2026-04-25T10:00:00.000Z";

  beforeEach(() => {
    vi.stubEnv("VITE_NOTEBOOK_MOCK", "false");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("reads real notebook entries as text/plain", async () => {
    vi.spyOn(client, "getText").mockResolvedValue(
      "# Reliable handoffs\n\nBody",
    );
    vi.spyOn(client, "get").mockImplementation(async (path: string) => {
      if (path.startsWith("/notebook/list")) {
        return {
          entries: [
            {
              path: "agents/planner/notebook/reliable-handoffs.md",
              title: "Reliable handoffs",
              modified: ts,
            },
          ],
        };
      }
      if (path === "/review/list?scope=all") return { reviews: [] };
      return {};
    });

    const entry = await fetchEntry("planner", "reliable-handoffs");

    expect(entry?.body_md).toBe("# Reliable handoffs\n\nBody");
    expect(entry?.title).toBe("Reliable handoffs");
  });

  it("keeps terminal failed review states from looking promoted", async () => {
    vi.spyOn(client, "getText").mockResolvedValue(
      "# Reliable handoffs\n\nBody",
    );
    vi.spyOn(client, "get").mockImplementation(async (path: string) => {
      if (path.startsWith("/notebook/list")) {
        return {
          entries: [
            {
              path: "agents/planner/notebook/reliable-handoffs.md",
              title: "Reliable handoffs",
              modified: ts,
            },
          ],
        };
      }
      if (path === "/review/list?scope=all") {
        return {
          reviews: [
            {
              id: "rvw-expired",
              state: "expired",
              source_slug: "planner",
              source_path: "agents/planner/notebook/reliable-handoffs.md",
              target_path: "team/playbooks/reliable-handoffs.md",
              reviewer_slug: "reviewer",
              created_at: ts,
              updated_at: ts,
              comments: [],
            },
          ],
        };
      }
      return {};
    });

    const entry = await fetchEntry("planner", "reliable-handoffs");
    const reviews = await fetchReviews();

    expect(reviews[0].state).toBe("expired");
    expect(entry?.status).toBe("discarded");
  });

  it("normalizes backend Promotion records for the review queue", async () => {
    vi.spyOn(client, "get").mockResolvedValue({
      reviews: [
        {
          id: "rvw-1",
          state: "pending",
          source_slug: "planner",
          source_path: "agents/planner/notebook/reliable-handoffs.md",
          target_path: "team/playbooks/reliable-handoffs.md",
          rationale: "Useful for every agent.",
          reviewer_slug: "reviewer",
          created_at: ts,
          updated_at: ts,
          comments: [
            {
              id: "c1",
              author_slug: "reviewer",
              body: "Reading now.",
              created_at: ts,
            },
          ],
        },
      ],
    });

    const reviews = await fetchReviews();

    expect(reviews[0]).toMatchObject({
      id: "rvw-1",
      agent_slug: "planner",
      entry_slug: "reliable-handoffs",
      entry_title: "Reliable Handoffs",
      proposed_wiki_path: "team/playbooks/reliable-handoffs.md",
      reviewer_slug: "reviewer",
      state: "pending",
    });
    expect(reviews[0].comments[0].body_md).toBe("Reading now.");
  });

  it("submits real promotions to team/ paths and fetches the created review", async () => {
    const postSpy = vi.spyOn(client, "post").mockResolvedValue({
      promotion_id: "rvw-1",
      reviewer_slug: "reviewer",
      state: "pending",
      human_only: false,
    });
    vi.spyOn(client, "get").mockResolvedValue({
      id: "rvw-1",
      state: "pending",
      source_slug: "planner",
      source_path: "agents/planner/notebook/reliable-handoffs.md",
      target_path: "team/drafts/planner-reliable-handoffs.md",
      reviewer_slug: "reviewer",
      created_at: ts,
      updated_at: ts,
      comments: [],
    });

    const review = await promoteEntry("planner", "reliable-handoffs");

    expect(postSpy).toHaveBeenCalledWith("/notebook/promote", {
      my_slug: "planner",
      source_path: "agents/planner/notebook/reliable-handoffs.md",
      target_wiki_path: "team/drafts/planner-reliable-handoffs.md",
      rationale: "Ready for team wiki review.",
      reviewer_slug: undefined,
    });
    expect(review?.id).toBe("rvw-1");
  });

  it("does not fall back to sample reviews when live promotion fails", async () => {
    vi.spyOn(client, "post").mockRejectedValue(
      new Error("review backend is not active"),
    );

    await expect(promoteEntry("planner", "reliable-handoffs")).rejects.toThrow(
      "review backend is not active",
    );
  });
});
