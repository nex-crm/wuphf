import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { DiscoveredSection, WikiCatalogEntry } from "../../api/wiki";
import WikiSidebar from "./WikiSidebar";

const CATALOG: WikiCatalogEntry[] = [
  {
    path: "people/nazz",
    title: "Nazz",
    author_slug: "pm",
    last_edited_ts: new Date().toISOString(),
    group: "people",
  },
  {
    path: "people/sarah",
    title: "Sarah",
    author_slug: "ceo",
    last_edited_ts: new Date().toISOString(),
    group: "people",
  },
  {
    path: "playbooks/churn",
    title: "Churn prevention",
    author_slug: "cmo",
    last_edited_ts: new Date().toISOString(),
    group: "playbooks",
  },
];

describe("<WikiSidebar> — legacy catalog-grouping path", () => {
  it("renders grouped articles", () => {
    render(<WikiSidebar catalog={CATALOG} onNavigate={() => {}} />);
    expect(screen.getByText("people")).toBeInTheDocument();
    expect(screen.getByText("playbooks")).toBeInTheDocument();
    expect(screen.getByText("Nazz")).toBeInTheDocument();
  });

  it("marks the current article", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        currentPath="people/nazz"
        onNavigate={() => {}}
      />,
    );
    const li = screen.getByText("Nazz").closest("li");
    expect(li).toHaveClass("current");
  });

  it("calls onNavigate when an article link is clicked", () => {
    const onNavigate = vi.fn();
    render(<WikiSidebar catalog={CATALOG} onNavigate={onNavigate} />);
    fireEvent.click(screen.getByText("Churn prevention"));
    expect(onNavigate).toHaveBeenCalledWith("playbooks/churn");
  });

  it("filters articles by the search query", () => {
    render(<WikiSidebar catalog={CATALOG} onNavigate={() => {}} />);
    const search = screen.getByPlaceholderText("Search wiki…");
    fireEvent.change(search, { target: { value: "churn" } });
    expect(screen.getByText("Churn prevention")).toBeInTheDocument();
    expect(screen.queryByText("Nazz")).not.toBeInTheDocument();
  });
});

// ── Dynamic sections — v1.3 ──────────────────────────────────────────

const nowIso = () => new Date().toISOString();
const daysAgoIso = (days: number) =>
  new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();

const SECTIONS: DiscoveredSection[] = [
  {
    slug: "people",
    title: "People",
    article_paths: ["team/people/nazz.md", "team/people/sarah.md"],
    article_count: 2,
    first_seen_ts: daysAgoIso(30),
    last_update_ts: nowIso(),
    from_schema: true,
  },
  {
    slug: "playbooks",
    title: "Playbooks",
    article_paths: ["team/playbooks/churn.md"],
    article_count: 1,
    first_seen_ts: daysAgoIso(14),
    last_update_ts: nowIso(),
    from_schema: true,
  },
  {
    slug: "retrospectives",
    title: "Retrospectives",
    article_paths: [],
    article_count: 0,
    first_seen_ts: daysAgoIso(2),
    last_update_ts: nowIso(),
    from_schema: false,
  },
];

describe("<WikiSidebar> — dynamic sections", () => {
  it("renders sections in the order provided", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    const headers = screen.getAllByRole("heading", { level: 3 });
    expect(
      headers.map(
        (h) =>
          h.dataset.sectionSlug ??
          h.closest("[data-section-slug]")?.getAttribute("data-section-slug"),
      ),
    ).toEqual(["people", "playbooks", "retrospectives"]);
  });

  it("distinguishes schema-declared from discovered sections via class", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    const peopleHeader = screen.getByText(/people/i).closest("h3");
    const retroHeader = screen.getByText(/retrospectives/i).closest("h3");
    expect(peopleHeader).toHaveClass("wk-section-schema");
    expect(retroHeader).toHaveClass("wk-section-discovered");
  });

  it('marks recently-discovered sections with a "new" indicator', () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    // retrospectives was first seen 2 days ago — under the 7-day window.
    expect(screen.getByLabelText("New section")).toBeInTheDocument();
  });

  it("does not mark blueprint-declared sections as new even if recent", () => {
    const recent: DiscoveredSection[] = [
      {
        ...SECTIONS[0],
        first_seen_ts: daysAgoIso(1),
        from_schema: true,
      },
    ];
    render(
      <WikiSidebar catalog={CATALOG} sections={recent} onNavigate={() => {}} />,
    );
    expect(screen.queryByLabelText("New section")).toBeNull();
  });

  it('shows an "add to blueprint" banner when a discovered section header is clicked', () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    expect(screen.queryByTestId("section-banner")).toBeNull();
    const retroHeader = screen.getByText(/retrospectives/i).closest("h3")!;
    fireEvent.click(retroHeader);
    expect(screen.getByTestId("section-banner")).toBeInTheDocument();
    expect(screen.getByTestId("section-banner")).toHaveTextContent(
      "retrospectives",
    );
  });

  it("does not show the banner for blueprint-declared sections", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    const peopleHeader = screen.getByText(/people/i).closest("h3")!;
    fireEvent.click(peopleHeader);
    expect(screen.queryByTestId("section-banner")).toBeNull();
  });

  it("renders empty sections with a placeholder row", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    // retrospectives has no articles — placeholder should be visible.
    expect(screen.getByText("No articles yet")).toBeInTheDocument();
  });

  it("filters by search within the section structure", () => {
    render(
      <WikiSidebar
        catalog={CATALOG}
        sections={SECTIONS}
        onNavigate={() => {}}
      />,
    );
    const search = screen.getByPlaceholderText("Search wiki…");
    fireEvent.change(search, { target: { value: "churn" } });
    expect(screen.getByText("Churn prevention")).toBeInTheDocument();
    expect(screen.queryByText("Nazz")).toBeNull();
    // An empty section ("retrospectives") is hidden during search.
    expect(screen.queryByText(/retrospectives/i)).toBeNull();
  });
});
