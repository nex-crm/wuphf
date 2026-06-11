import type { ReactElement, ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render as rtlRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Wiki renders WikiArticle, whose delete control reads the wiki-tree React
// Query, so renders need a QueryClient in context. Wrap via the `wrapper`
// option; fresh client per render isolates tests, retries off so error states
// surface immediately.
function render(ui: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return rtlRender(ui, { wrapper: Wrapper });
}

// Stub the lazy file-viewer dispatcher so the article-vs-file routing can be
// asserted without loading the heavy per-format viewer chunks.
vi.mock("./viewers/FileViewer", () => ({
  __esModule: true,
  default: ({ path }: { path: string }) => (
    <div data-testid="stub-file-viewer" data-path={path} />
  ),
  isMarkdownPath: (path: string) => /\.(md|markdown)$/i.test(path),
}));

// Stub the embedded app viewer so the app-routing branch can be asserted
// without mounting an iframe or touching the sidebar store.
vi.mock("./WebsiteViewer", () => ({
  __esModule: true,
  default: ({ path }: { path: string }) => (
    <div data-testid="stub-website-viewer" data-path={path} />
  ),
}));

import * as api from "../../api/wiki";
import { APP_NAV_PREFIX } from "./tree/WikiTree";
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

  it("shows the search-first home when no article is selected", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([
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
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "Team Wiki" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wk-home-search")).toBeInTheDocument();
  });

  it("keeps the legacy tree + catalog behind the All files escape hatch", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([
      {
        path: "people/nazz",
        title: "Nazz",
        author_slug: "pm",
        last_edited_ts: new Date().toISOString(),
        group: "people",
      },
    ]);
    render(<Wiki articlePath="_files" onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByTestId("wk-catalog")).toBeInTheDocument(),
    );
  });

  it("renders a category index page for a _category path", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([
      {
        path: "team/people/nazz.md",
        title: "Nazz",
        author_slug: "pm",
        last_edited_ts: new Date().toISOString(),
        group: "people",
      },
    ]);
    render(<Wiki articlePath="_category/people" onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByTestId("wk-category-page")).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("heading", { name: "Category: People" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Nazz" })).toBeInTheDocument();
  });

  it("shows an article when a path is provided", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
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
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
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

  it("renders the embedded app viewer for an APP_NAV_PREFIX path", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
    const articleSpy = vi.spyOn(api, "fetchArticle");

    render(
      <Wiki
        articlePath={`${APP_NAV_PREFIX}team/site/dashboard`}
        onNavigate={() => {}}
      />,
    );

    await waitFor(() =>
      expect(screen.getByTestId("stub-website-viewer")).toBeInTheDocument(),
    );
    // The sentinel prefix is stripped before the folder path reaches the viewer.
    expect(screen.getByTestId("stub-website-viewer")).toHaveAttribute(
      "data-path",
      "team/site/dashboard",
    );
    // App folders are not articles or files — neither of those views fires.
    expect(articleSpy).not.toHaveBeenCalled();
    expect(screen.queryByTestId("stub-file-viewer")).not.toBeInTheDocument();
  });

  it("keeps the article view for a bare slug (no extension)", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
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
    // Initial load is empty; the live refresh returns the new article.
    vi.spyOn(api, "fetchCatalogStrict")
      .mockResolvedValueOnce([])
      .mockResolvedValue([
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
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
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
    // Initial load succeeds; the LIVE refresh (also strict) fails.
    vi.spyOn(api, "fetchCatalogStrict")
      .mockResolvedValueOnce([
        {
          path: "team/playbooks/pricing.md",
          title: "Pricing Playbook",
          author_slug: "pm",
          last_edited_ts: new Date().toISOString(),
          group: "playbooks",
        },
      ])
      .mockRejectedValue(new Error("broker down"));

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

  it("shows a loading state — never '0 articles' — while the catalog is pending (C4)", () => {
    vi.spyOn(api, "fetchCatalogStrict").mockImplementation(
      () => new Promise(() => {}),
    );
    render(<Wiki articlePath={null} onNavigate={() => {}} />);
    expect(screen.getByTestId("wk-catalog-loading")).toBeInTheDocument();
    expect(screen.queryByText(/0 articles/)).toBeNull();
    expect(screen.queryByTestId("wk-catalog")).toBeNull();
  });

  it("shows broker-not-responding + Retry when the catalog load fails (C4)", async () => {
    const fetchSpy = vi
      .spyOn(api, "fetchCatalogStrict")
      .mockRejectedValueOnce(
        new Error("Broker not responding — request timed out."),
      )
      .mockResolvedValueOnce([
        {
          path: "team/people/nazz.md",
          title: "Nazz",
          author_slug: "pm",
          last_edited_ts: new Date().toISOString(),
          group: "people",
        },
      ]);
    render(<Wiki articlePath={null} onNavigate={() => {}} />);

    await waitFor(() =>
      expect(screen.getByTestId("wk-catalog-error")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("wk-catalog-error")).toHaveTextContent(
      /broker not responding/i,
    );
    expect(screen.queryByText(/0 articles/)).toBeNull();

    // Retry recovers to the real catalog.
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() =>
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
    );
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });

  it("shows the true empty state only after a successful empty load (C4)", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
    render(<Wiki articlePath={null} onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
    );
    // The load actually finished — "0 articles" is now honest.
    expect(screen.getByText(/0 articles/)).toBeInTheDocument();
  });
});
