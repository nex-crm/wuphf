import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../../api/wiki";
import Wiki from "./Wiki";

describe("<Wiki>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(api, "subscribeEditLog").mockImplementation(() => () => {});
    vi.spyOn(api, "subscribeSectionsUpdated").mockImplementation(
      () => () => {},
    );
    vi.spyOn(api, "fetchSections").mockResolvedValue([]);
    vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: [] });
  });

  it("shows the catalog when no article is selected", async () => {
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([
      {
        path: "people/nazz",
        title: "Nazz",
        author_slug: "pm",
        last_edited_ts: new Date().toISOString(),
        group: "people",
      },
    ]);
    render(<Wiki articlePath={null} onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByTestId("wk-catalog")).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "Team Wiki" }),
    ).toBeInTheDocument();
  });

  it("shows an article when a path is provided", async () => {
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([]);
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "people/customer-x",
      title: "Customer X",
      content: "Body text.",
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["ceo"],
      backlinks: [],
      word_count: 10,
      categories: [],
    });
    render(<Wiki articlePath="people/customer-x" onNavigate={() => {}} />);
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
  });

  it("refreshes the article catalog after a live section update", async () => {
    let sectionHandler: ((event: api.WikiSectionsUpdatedEvent) => void) | null =
      null;
    vi.spyOn(api, "subscribeSectionsUpdated").mockImplementation((handler) => {
      sectionHandler = handler;
      return () => {};
    });
    vi.spyOn(api, "fetchCatalog")
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          path: "team/templates/brief.md",
          title: "Brief Template",
          author_slug: "pm",
          last_edited_ts: new Date().toISOString(),
          group: "templates",
        },
      ]);

    render(<Wiki articlePath={null} onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByTestId("wk-catalog")).toBeInTheDocument(),
    );

    await act(async () => {
      sectionHandler?.({
        sections: [
          {
            slug: "templates",
            title: "Templates",
            article_paths: ["team/templates/brief.md"],
            article_count: 1,
            first_seen_ts: new Date().toISOString(),
            last_update_ts: new Date().toISOString(),
            from_schema: false,
          },
        ],
        timestamp: new Date().toISOString(),
      });
      await Promise.resolve();
    });

    await waitFor(() =>
      expect(screen.getAllByText("Brief Template").length).toBeGreaterThan(0),
    );
  });
});
