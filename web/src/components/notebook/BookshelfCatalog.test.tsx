import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { NotebookCatalogSummary } from "../../api/notebook";
import BookshelfCatalog from "./BookshelfCatalog";

const CATALOG: NotebookCatalogSummary = {
  total_agents: 2,
  total_entries: 5,
  pending_promotion: 1,
  agents: [
    {
      agent_slug: "pm",
      name: "PM",
      role: "Product Manager · agent",
      entries: [
        {
          entry_slug: "e1",
          title: "Entry one",
          last_edited_ts: new Date().toISOString(),
          status: "draft",
        },
      ],
      total: 1,
      promoted_count: 0,
      last_updated_ts: new Date().toISOString(),
    },
    {
      agent_slug: "ceo",
      name: "CEO",
      role: "CEO · agent",
      entries: [],
      total: 0,
      promoted_count: 0,
      last_updated_ts: new Date().toISOString(),
    },
  ],
};

describe("<BookshelfCatalog>", () => {
  it("renders the Team notebooks header and meta", () => {
    render(
      <BookshelfCatalog
        catalog={CATALOG}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Team notebooks" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("2 agents · 5 entries · 1 pending promotion"),
    ).toBeInTheDocument();
  });

  it("renders one AgentShelf row per agent", () => {
    render(
      <BookshelfCatalog
        catalog={CATALOG}
        onOpenAgent={() => {}}
        onOpenEntry={() => {}}
      />,
    );
    expect(screen.getByText("PM's notebook")).toBeInTheDocument();
    expect(screen.getByText("CEO's notebook")).toBeInTheDocument();
  });
});
