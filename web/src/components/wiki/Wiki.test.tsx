import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Stub the lazy file-viewer dispatcher so the article-vs-file routing can be
// asserted without loading the heavy per-format viewer chunks.
vi.mock("./viewers/FileViewer", () => ({
  __esModule: true,
  default: ({ path }: { path: string }) => (
    <div data-testid="stub-file-viewer" data-path={path} />
  ),
  isMarkdownPath: (path: string) => /\.(md|markdown)$/i.test(path),
}));

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

  it("renders the file viewer for a non-markdown path", async () => {
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([]);
    const articleSpy = vi.spyOn(api, "fetchArticle");

    render(<Wiki articlePath="team/assets/report.pdf" onNavigate={() => {}} />);

    await waitFor(() =>
      expect(screen.getByTestId("stub-file-viewer")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("stub-file-viewer")).toHaveAttribute(
      "data-path",
      "team/assets/report.pdf",
    );
    // The article fetch must not fire for a file path.
    expect(articleSpy).not.toHaveBeenCalled();
  });

  it("keeps the article view for a bare slug (no extension)", async () => {
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([]);
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "team/people/nazz.md",
      title: "Nazz",
      content: "Body.",
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["ceo"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });

    render(<Wiki articlePath="people/nazz" onNavigate={() => {}} />);

    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Nazz" })).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("stub-file-viewer")).not.toBeInTheDocument();
  });

  it("refreshes the article catalog after a live section update", async () => {
    let sectionHandler: ((event: api.WikiSectionsUpdatedEvent) => void) | null =
      null;
    vi.spyOn(api, "subscribeSectionsUpdated").mockImplementation((handler) => {
      sectionHandler = handler;
      return () => {};
    });
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([]);
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([
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

  it("keeps the current catalog when a live refresh fails", async () => {
    let sectionHandler: ((event: api.WikiSectionsUpdatedEvent) => void) | null =
      null;
    vi.spyOn(api, "subscribeSectionsUpdated").mockImplementation((handler) => {
      sectionHandler = handler;
      return () => {};
    });
    vi.spyOn(api, "fetchCatalog").mockResolvedValue([
      {
        path: "team/playbooks/pricing.md",
        title: "Pricing Playbook",
        author_slug: "pm",
        last_edited_ts: new Date().toISOString(),
        group: "playbooks",
      },
    ]);
    vi.spyOn(api, "fetchCatalogStrict").mockRejectedValue(
      new Error("broker down"),
    );

    render(<Wiki articlePath={null} onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getAllByText("Pricing Playbook").length).toBeGreaterThan(0),
    );

    await act(async () => {
      sectionHandler?.({
        sections: [
          {
            slug: "playbooks",
            title: "Playbooks",
            article_paths: ["team/playbooks/pricing.md"],
            article_count: 1,
            first_seen_ts: new Date().toISOString(),
            last_update_ts: new Date().toISOString(),
            from_schema: true,
          },
        ],
        timestamp: new Date().toISOString(),
      });
      await Promise.resolve();
    });

    expect(screen.getAllByText("Pricing Playbook").length).toBeGreaterThan(0);
  });
});
