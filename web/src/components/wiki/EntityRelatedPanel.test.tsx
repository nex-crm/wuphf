import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/entity";
import EntityRelatedPanel from "./EntityRelatedPanel";

describe("<EntityRelatedPanel>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "subscribeEntityEvents").mockImplementation(() => () => {});
  });

  it("renders the empty state when the graph has no edges", async () => {
    vi.spyOn(api, "fetchEntityGraph").mockResolvedValue([]);
    render(<EntityRelatedPanel kind="people" slug="sarah" />);
    await waitFor(() =>
      expect(screen.getByText(/No related entities yet/i)).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: /related/i }),
    ).toBeInTheDocument();
  });

  it("lists up to 5 out-edges as wikilinks", async () => {
    const edges: api.GraphEdge[] = [
      {
        from_kind: "people",
        from_slug: "sarah",
        to_kind: "companies",
        to_slug: "acme",
        first_seen_fact_id: "f1",
        last_seen_ts: "2026-04-20T00:00:00Z",
        occurrence_count: 2,
      },
      {
        from_kind: "people",
        from_slug: "sarah",
        to_kind: "customers",
        to_slug: "globex",
        first_seen_fact_id: "f2",
        last_seen_ts: "2026-04-19T00:00:00Z",
        occurrence_count: 1,
      },
    ];
    vi.spyOn(api, "fetchEntityGraph").mockResolvedValue(edges);
    render(<EntityRelatedPanel kind="people" slug="sarah" />);
    await screen.findByText("companies/acme");
    expect(screen.getByText("customers/globex")).toBeInTheDocument();
    // Occurrence count only shown when >1.
    expect(screen.getByText("×2")).toBeInTheDocument();
    expect(screen.queryByText("×1")).toBeNull();
    // Render as a data-wikilink anchor so the wiki router can intercept.
    const link = screen.getByText("companies/acme").closest("a");
    expect(link).toHaveAttribute("data-wikilink", "true");
    expect(link?.getAttribute("href")).toContain("companies/acme.md");
  });

  it("caps the list at 5 entries", async () => {
    const edges: api.GraphEdge[] = Array.from({ length: 8 }, (_v, i) => ({
      from_kind: "people" as const,
      from_slug: "sarah",
      to_kind: "companies" as const,
      to_slug: `target-${i}`,
      first_seen_fact_id: `f${i}`,
      last_seen_ts: `2026-04-2${i}T00:00:00Z`,
      occurrence_count: 1,
    }));
    vi.spyOn(api, "fetchEntityGraph").mockResolvedValue(edges);
    render(<EntityRelatedPanel kind="people" slug="sarah" />);
    await screen.findByText("companies/target-0");
    expect(screen.getByText("companies/target-4")).toBeInTheDocument();
    expect(screen.queryByText("companies/target-5")).toBeNull();
  });

  it("surfaces a load error", async () => {
    vi.spyOn(api, "fetchEntityGraph").mockRejectedValue(new Error("boom"));
    render(<EntityRelatedPanel kind="people" slug="sarah" />);
    await waitFor(() => expect(screen.getByText("boom")).toBeInTheDocument());
  });

  // The panel's whole value proposition is real-time updates when agents
  // record facts. This test captures the fact_recorded callback and fires
  // it, then asserts the panel re-queries the graph API and re-renders
  // with the new edge. Without this, the SSE wiring is structurally
  // present but behaviorally unverified.
  it("re-fetches the graph on entity:fact_recorded for this entity", async () => {
    let capturedOnFact: (() => void) | null = null;
    let capturedKind: unknown = null;
    let capturedSlug: unknown = null;
    vi.spyOn(api, "subscribeEntityEvents").mockImplementation(
      (kind, slug, onFact) => {
        capturedKind = kind;
        capturedSlug = slug;
        capturedOnFact = onFact as () => void;
        return () => {};
      },
    );

    const initial: api.GraphEdge[] = [
      {
        from_kind: "people",
        from_slug: "sarah",
        to_kind: "companies",
        to_slug: "acme",
        first_seen_fact_id: "f1",
        last_seen_ts: "2026-04-20T00:00:00Z",
        occurrence_count: 1,
      },
    ];
    const updated: api.GraphEdge[] = [
      ...initial,
      {
        from_kind: "people",
        from_slug: "sarah",
        to_kind: "customers",
        to_slug: "globex",
        first_seen_fact_id: "f2",
        last_seen_ts: "2026-04-21T00:00:00Z",
        occurrence_count: 1,
      },
    ];
    const fetchSpy = vi
      .spyOn(api, "fetchEntityGraph")
      .mockResolvedValueOnce(initial)
      .mockResolvedValueOnce(updated);

    render(<EntityRelatedPanel kind="people" slug="sarah" />);
    await screen.findByText("companies/acme");
    expect(screen.queryByText("customers/globex")).toBeNull();
    expect(fetchSpy).toHaveBeenCalledTimes(1);

    // Confirm the subscriber was scoped to this entity's kind+slug, so the
    // broker-side filter will only hand us events for sarah/people.
    expect(capturedKind).toBe("people");
    expect(capturedSlug).toBe("sarah");

    // Fire the fact_recorded callback as the SSE layer would.
    expect(capturedOnFact).not.toBeNull();
    capturedOnFact!();

    await screen.findByText("customers/globex");
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });
});
