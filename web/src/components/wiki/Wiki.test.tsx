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
import Wiki, { WIKI_LAST_VIEWED_KEY } from "./Wiki";

describe("<Wiki>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    globalThis.localStorage?.clear();
    vi.spyOn(api, "subscribeEditLog").mockImplementation(() => () => {});
    vi.spyOn(api, "subscribeSectionsUpdated").mockImplementation(
      () => () => {},
    );
    vi.spyOn(api, "fetchSections").mockResolvedValue([]);
    vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: [] });
    // The persistent page-tree sidebar mounts on every view.
    vi.spyOn(api, "fetchWikiTree").mockResolvedValue([]);
  });

  it("shows the overview with the page tree when no article is selected", async () => {
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
      screen.getByRole("heading", { name: "Company Brain" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("wk-home-search")).toBeInTheDocument();
    // The page tree sidebar is THE navigation — visible next to the overview.
    expect(screen.getByTestId("wk-nav-sidebar")).toBeInTheDocument();
    expect(screen.getByTestId("wk-tree")).toBeInTheDocument();
  });

  it("resumes on the last-viewed article when the wiki opens bare", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
    globalThis.localStorage.setItem(
      WIKI_LAST_VIEWED_KEY,
      "team/people/nazz.md",
    );
    const onNavigate = vi.fn();
    render(<Wiki articlePath={null} onNavigate={onNavigate} />);
    await waitFor(() =>
      expect(onNavigate).toHaveBeenCalledWith("team/people/nazz.md"),
    );
  });

  it("records the open article as last-viewed for the next session", async () => {
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
      expect(globalThis.localStorage.getItem(WIKI_LAST_VIEWED_KEY)).toBe(
        "people/nazz",
      ),
    );
  });

  it("lands legacy _files deep links on the overview (tree is always visible)", async () => {
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
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("wk-tree")).toBeInTheDocument();
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

  it("folds the left page tree to a rail and back, persisting the choice", async () => {
    vi.spyOn(api, "fetchCatalogStrict").mockResolvedValue([]);
    const { container } = render(
      <Wiki articlePath={null} onNavigate={() => {}} />,
    );
    await waitFor(() =>
      expect(screen.getByTestId("wk-home")).toBeInTheDocument(),
    );

    const layout = container.querySelector(".wiki-layout");
    expect(layout).toHaveAttribute("data-left-collapsed", "false");
    expect(screen.getByTestId("wk-sidebar-home")).toBeInTheDocument();

    // Collapse → the layout reports the fold and the menu yields to a rail.
    fireEvent.click(
      screen.getByRole("button", { name: "Collapse Pages panel" }),
    );
    expect(layout).toHaveAttribute("data-left-collapsed", "true");
    expect(screen.queryByTestId("wk-sidebar-home")).not.toBeInTheDocument();
    expect(globalThis.localStorage.getItem("wuphf:wiki:panels")).toContain(
      '"left":true',
    );

    // Expand from the rail restores the full sidebar.
    fireEvent.click(screen.getByRole("button", { name: "Expand Pages panel" }));
    expect(layout).toHaveAttribute("data-left-collapsed", "false");
    expect(screen.getByTestId("wk-sidebar-home")).toBeInTheDocument();
  });

  it("folds the right details rail on an article page", async () => {
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
    const { container } = render(
      <Wiki articlePath="people/customer-x" onNavigate={() => {}} />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );

    const layout = container.querySelector(".wiki-layout");
    expect(layout).toHaveAttribute("data-right-collapsed", "false");

    fireEvent.click(
      screen.getByRole("button", { name: "Collapse Details panel" }),
    );
    expect(layout).toHaveAttribute("data-right-collapsed", "true");
    expect(
      screen.getByRole("button", { name: "Expand Details panel" }),
    ).toBeInTheDocument();
    expect(globalThis.localStorage.getItem("wuphf:wiki:panels")).toContain(
      '"right":true',
    );
  });
});
