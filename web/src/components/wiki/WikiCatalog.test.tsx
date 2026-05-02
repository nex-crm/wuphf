import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { WikiCatalogEntry } from "../../api/wiki";
import WikiCatalog from "./WikiCatalog";

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
    title: "Churn",
    author_slug: "cmo",
    last_edited_ts: new Date().toISOString(),
    group: "playbooks",
  },
];

describe("<WikiCatalog>", () => {
  it("renders thematic groups with article counts", () => {
    render(<WikiCatalog catalog={CATALOG} onNavigate={() => {}} />);
    expect(
      screen.getByRole("heading", { name: "Team Wiki" }),
    ).toBeInTheDocument();
    expect(screen.getByText("people")).toBeInTheDocument();
    expect(screen.getByText("playbooks")).toBeInTheDocument();
    expect(screen.getByText("Nazz")).toBeInTheDocument();
    expect(screen.getByText("Churn")).toBeInTheDocument();
  });

  it("invokes onNavigate when an article title is clicked", async () => {
    const onNavigate = vi.fn();
    const user = userEvent.setup();
    render(<WikiCatalog catalog={CATALOG} onNavigate={onNavigate} />);
    await user.click(screen.getByText("Nazz"));
    expect(onNavigate).toHaveBeenCalledWith("people/nazz");
  });

  it("paints the verbose prune-signal badge on top-decile entries", () => {
    // Ten entries: one obvious outlier (10000) + nine smaller scores.
    // floor(10 * 0.1) = 1 → cutoff is the second-highest score (500), so
    // only the top entry sits at-or-above the threshold AND above zero.
    const scored: WikiCatalogEntry[] = Array.from({ length: 10 }, (_, i) => ({
      path: `playbooks/p-${i}`,
      title: `Playbook ${i}`,
      author_slug: "pm",
      last_edited_ts: new Date().toISOString(),
      group: "playbooks",
      word_count: 200,
      prune_score: i === 0 ? 10000 : 100 + i * 50,
    }));
    render(<WikiCatalog catalog={scored} onNavigate={() => {}} />);
    const badges = screen.getAllByTestId("wk-prune-verbose-badge");
    expect(badges).toHaveLength(1);
  });

  it("never paints the verbose badge when no entry has a positive score", () => {
    // All-zero corpus (newly bootstrapped wiki, never read) → no badges.
    const all_zero: WikiCatalogEntry[] = CATALOG.map((c) => ({
      ...c,
      word_count: 100,
      prune_score: 0,
    }));
    render(<WikiCatalog catalog={all_zero} onNavigate={() => {}} />);
    expect(screen.queryByTestId("wk-prune-verbose-badge")).toBeNull();
  });

  it("uses provided stats in the header", () => {
    render(
      <WikiCatalog
        catalog={CATALOG}
        onNavigate={() => {}}
        articlesCount={32}
        commitsCount={128}
        agentsCount={6}
      />,
    );
    expect(screen.getByText(/32 articles/)).toBeInTheDocument();
    expect(screen.getByText(/128 commits/)).toBeInTheDocument();
    expect(screen.getByText(/6 agents writing/)).toBeInTheDocument();
  });
});
