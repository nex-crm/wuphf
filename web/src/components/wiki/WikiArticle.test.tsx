import type { ReactElement, ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  fireEvent,
  render as rtlRender,
  screen,
  waitFor,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as richApi from "../../api/richArtifacts";
import * as api from "../../api/wiki";
import { requestOpenInEdit } from "./openInEditTarget";
import WikiArticle from "./WikiArticle";

// WikiArticle now reads the wiki-tree React Query (the article delete control
// invalidates it so a deleted page leaves the sidebar index), so renders need
// a QueryClient in context. Wrap via the `wrapper` option so `rerender` keeps
// the provider. Fresh client per render isolates tests; retries off so error
// states surface immediately.
function render(ui: ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return rtlRender(ui, { wrapper: Wrapper });
}

// WikiArticle's Edit tab mounts the WYSIWYG WikiEditor, which lazy-loads the
// real Tiptap/ProseMirror stack (RefCloneEditor). Stub that module with a
// controlled textarea that mirrors the props.onChange contract so the
// article-level save/refresh flow is exercised without booting ProseMirror in
// tests. (The legacy ./editor/TiptapWikiEditor is also stubbed for any path
// that still references it.)
const editorStub = ({
  content,
  onChange,
}: {
  content: string;
  onChange: (markdown: string) => void;
}) => (
  <textarea
    data-testid="wk-tiptap-stub"
    value={content}
    onChange={(e) => onChange(e.target.value)}
  />
);
vi.mock("./editor/refclone/RefCloneEditor", () => ({ default: editorStub }));
vi.mock("./editor/TiptapWikiEditor", () => ({ default: editorStub }));

const CATALOG: api.WikiCatalogEntry[] = [
  {
    path: "people/sarah-chen",
    title: "Sarah Chen",
    author_slug: "ceo",
    last_edited_ts: new Date().toISOString(),
    group: "people",
  },
];

const STUB_ARTICLE: api.WikiArticle = {
  path: "people/customer-x",
  title: "Customer X",
  content: "**Customer X** is a pilot.",
  last_edited_by: "ceo",
  last_edited_ts: new Date().toISOString(),
  revisions: 3,
  contributors: ["ceo", "pm"],
  backlinks: [],
  word_count: 100,
  categories: [],
};

beforeEach(() => {
  vi.restoreAllMocks();
  // Default history stub — individual tests override as needed.
  vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: [] });
  vi.spyOn(richApi, "fetchWikiVisualArtifact").mockResolvedValue(null);
  // Default the by-id artifact fetch to a 404-style rejection so articles
  // without inline `visual-artifact:<id>` markers never accidentally embed
  // one. Tests that exercise inline markers override this with a resolved
  // detail. restoreAllMocks in this same hook resets the spy each test, so
  // the per-test spyOn re-installs cleanly (this mirrors the proven pattern
  // in NotebookEntry.test.tsx).
  vi.spyOn(richApi, "fetchRichArtifact").mockRejectedValue(
    new Error("404 not found"),
  );
});

describe("<WikiArticle content>", () => {
  it("fetches an article, renders its markdown, and distinguishes broken wikilinks", async () => {
    // Arrange
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "people/customer-x",
      title: "Customer X",
      content:
        "**Customer X** is a mid-market logistics company. See [[people/sarah-chen|Sarah Chen]] and [[missing|Missing page]].",
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 47,
      contributors: ["ceo", "pm"],
      backlinks: [],
      word_count: 100,
      categories: ["Active pilot"],
    });

    // Act
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );

    // Assert
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByText(/mid-market logistics company/i),
    ).toBeInTheDocument();

    const okLink = await screen.findByText("Sarah Chen");
    expect(okLink.closest("a")).toHaveAttribute("data-wikilink", "true");
    expect(okLink.closest("a")).toHaveAttribute("data-broken", "false");

    const brokenLink = await screen.findByText("Missing page");
    expect(brokenLink.closest("a")).toHaveAttribute("data-broken", "true");
  });

  it("switches to raw markdown tab and shows the source", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "a/b",
      title: "A",
      content: "## Heading A\n\nBody.\n\n### Sub\n\nMore.",
      last_edited_by: "pm",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["pm"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });
    const { getByRole, findByText, getByText } = render(
      <WikiArticle path="a/b" catalog={[]} onNavigate={() => {}} />,
    );
    await findByText(/Body\./);
    getByRole("button", { name: "Raw markdown" }).click();
    await waitFor(() => expect(getByText(/## Heading A/)).toBeInTheDocument());
    getByRole("button", { name: "History" }).click();
    // The History tab now mounts the real VersionHistory panel. With the
    // default empty fetchHistory stub it lands on its empty state.
    await waitFor(() =>
      expect(getByText(/no version history yet/i)).toBeInTheDocument(),
    );
  });

  it("renders a paired visual artifact inline at the top of the Article tab (no separate Visual tab)", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "team/drafts/visual-plan.md",
      title: "Visual Plan",
      content: "# Visual Plan\n\nSummary.",
      last_edited_by: "pm",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["pm"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });
    vi.spyOn(richApi, "fetchWikiVisualArtifact").mockResolvedValue({
      artifact: {
        id: "ra_0123456789abcdef",
        kind: "wiki_visual",
        title: "Visual Plan",
        summary: "A richer plan.",
        trustLevel: "promoted",
        representation: "html",
        htmlPath: "wiki/visual-artifacts/ra_0123456789abcdef.html",
        promotedWikiPath: "team/drafts/visual-plan.md",
        createdBy: "pm",
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        contentHash: "hash",
        sanitizerVersion: "sandbox-v1",
      },
      html: "<h1>Visual artifact</h1>",
    });

    render(
      <WikiArticle
        path="team/drafts/visual-plan.md"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    // The Visual tab is gone — folded into Article. The article tab is the
    // default, so the embed should appear without any tab interaction.
    expect(screen.queryByRole("button", { name: "Visual" })).toBeNull();

    const embed = await screen.findByLabelText("Visual Plan", {
      selector: "rich-artifact-embed",
    });
    expect(embed).toBeInTheDocument();
    expect(embed.closest("figure")).not.toBeNull();

    // The right sidebar is preserved (we dropped the dedicated visual mode).
    expect(document.querySelector(".wk-right-sidebar")).not.toBeNull();

    // No iframe anywhere — strict inline embed.
    expect(document.querySelector("iframe")).toBeNull();
  });
});

describe("<WikiArticle inline visual-artifact markers>", () => {
  it("strips an inline visual-artifact marker and embeds the referenced artifact", async () => {
    // Repro of the live bug: the agent hand-wrote a standalone
    // `visual-artifact:<id>` marker into the article body via team_wiki_write
    // instead of promoting. The marker must never render as raw text; the
    // referenced artifact must be embedded inline.
    const inlineId = "ra_19dcb5cac5a2bd3a";
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "team/drafts/ceo-coffee-extraction-control-chart.md",
      title: "Coffee Extraction Control Chart",
      content: `# Coffee Extraction\n\nBody copy.\n\nvisual-artifact:${inlineId}\n\nMore copy.`,
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["ceo"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });
    // Override the beforeEach 404 default: this id resolves to a draft
    // artifact referenced only by the hand-written marker.
    const fetchByIdSpy = vi
      .spyOn(richApi, "fetchRichArtifact")
      .mockResolvedValue({
        artifact: {
          id: inlineId,
          kind: "wiki_visual",
          title: "Coffee Extraction Control Chart",
          summary: "",
          trustLevel: "draft",
          representation: "html",
          htmlPath: `wiki/visual-artifacts/${inlineId}.html`,
          createdBy: "ceo",
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          contentHash: "hash",
          sanitizerVersion: "sandbox-v1",
        },
        html: "<svg aria-label='inline-control-chart'></svg>",
      });

    render(
      <WikiArticle
        path="team/drafts/ceo-coffee-extraction-control-chart.md"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    // The artifact is fetched by id and embedded inline (scoped to this
    // render's body to avoid sibling-test shadow-DOM leakage).
    const body = await screen.findByTestId("wk-article-body");
    const embed = await screen.findByTestId("rich-artifact-embed");
    expect(body.contains(embed)).toBe(true);
    expect(fetchByIdSpy).toHaveBeenCalledWith(inlineId);

    // The raw marker must never render as literal text in the body.
    expect(body.textContent ?? "").not.toContain("visual-artifact:");
    expect(body.textContent ?? "").not.toContain(inlineId);
    // Surrounding prose is preserved.
    expect(body.textContent ?? "").toContain("Body copy.");
    expect(body.textContent ?? "").toContain("More copy.");
  });

  it("does not double-embed when the promoted artifact is also referenced inline", async () => {
    // Distinct from the strip test's id so a leaked shadow-DOM node from a
    // sibling test cannot masquerade as this render's embed.
    const sharedId = "ra_5ed0000012345abc";
    const detail: richApi.RichArtifactDetail = {
      artifact: {
        id: sharedId,
        kind: "wiki_visual",
        title: "Shared Chart",
        summary: "",
        trustLevel: "promoted",
        representation: "html",
        htmlPath: `wiki/visual-artifacts/${sharedId}.html`,
        promotedWikiPath: "team/reference/coffee.md",
        createdBy: "ceo",
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
        contentHash: "hash",
        sanitizerVersion: "sandbox-v1",
      },
      html: "<svg aria-label='shared-chart'></svg>",
    };
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "team/reference/coffee.md",
      title: "Coffee",
      content: `# Coffee\n\nBody copy.\n\nvisual-artifact:${sharedId}\n`,
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["ceo"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });
    // The shared id resolves through BOTH the promoted-path fetch
    // (fetchWikiVisualArtifact) and the inline-marker fetch
    // (fetchRichArtifact). The dedupe must collapse them to a single embed.
    vi.spyOn(richApi, "fetchWikiVisualArtifact").mockResolvedValue(detail);
    vi.spyOn(richApi, "fetchRichArtifact").mockResolvedValue(detail);

    render(
      <WikiArticle
        path="team/reference/coffee.md"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    const body = await screen.findByTestId("wk-article-body");
    // Wait for the inline embed to mount, then assert exactly one embed lives
    // inside this render's body. Scoping to body (rather than screen) keeps
    // the count honest even if a sibling test leaked a custom-element node;
    // the distinct id above means no leaked node could match anyway.
    const embed = await screen.findByTestId("rich-artifact-embed");
    expect(body.contains(embed)).toBe(true);
    const embedsInBody = Array.from(
      body.querySelectorAll('[data-testid="rich-artifact-embed"]'),
    );
    expect(embedsInBody).toHaveLength(1);
  });

  it("skips an inline marker whose artifact 404s without leaking raw text", async () => {
    const missingId = "ra_00000000deadbeef";
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      path: "team/reference/coffee.md",
      title: "Coffee",
      content: `# Coffee\n\nBefore.\n\nvisual-artifact:${missingId}\n\nAfter.`,
      last_edited_by: "ceo",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["ceo"],
      backlinks: [],
      word_count: 5,
      categories: [],
    });
    // beforeEach already defaults fetchRichArtifact to a 404 rejection; this
    // test relies on that default, so no extra spy is needed.

    render(
      <WikiArticle
        path="team/reference/coffee.md"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    const body = await screen.findByTestId("wk-article-body");
    // No embed, but also no leaked marker text — the stripped line keeps the
    // body clean and the failed fetch degrades to nothing visible.
    await waitFor(() => {
      expect(body.textContent ?? "").toContain("Before.");
    });
    expect(body.textContent ?? "").not.toContain("visual-artifact:");
    expect(body.textContent ?? "").not.toContain(missingId);
    expect(screen.queryByTestId("rich-artifact-embed")).toBeNull();
  });
});

describe("<WikiArticle content (cont.)>", () => {
  it("keeps the selected tab during same-path refreshes without a visual artifact", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);

    const { rerender } = render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
        externalRefreshNonce={0}
      />,
    );

    await screen.findByRole("heading", { name: "Customer X" });
    fireEvent.click(screen.getByRole("button", { name: "History" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "History" })).toHaveClass(
        "active",
      ),
    );

    rerender(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
        externalRefreshNonce={1}
      />,
    );

    await waitFor(() =>
      expect(richApi.fetchWikiVisualArtifact).toHaveBeenCalledTimes(2),
    );
    expect(screen.getByRole("button", { name: "History" })).toHaveClass(
      "active",
    );
  });

  it("renders an error state when fetchArticle rejects", async () => {
    vi.spyOn(api, "fetchArticle").mockRejectedValue(new Error("network down"));
    render(<WikiArticle path="broken" catalog={[]} onNavigate={() => {}} />);
    await waitFor(() =>
      expect(screen.getByText(/network down/)).toBeInTheDocument(),
    );
    // The error state must expose a retry affordance so the user is not
    // stuck on a dead end if the broker recovers.
    expect(
      screen.getByRole("button", { name: /retry loading article/i }),
    ).toBeInTheDocument();
  });
});

describe("<WikiArticle loader timeout and retry>", () => {
  it("transitions Loading article… to an error+retry state on timeout and retries on click", async () => {
    // Arrange — first call hangs forever to simulate a stalled broker.
    type Resolve = (v: api.WikiArticle) => void;
    let firstResolve: Resolve | null = null;
    const fetchSpy = vi.spyOn(api, "fetchArticle");
    fetchSpy.mockImplementationOnce(
      () =>
        new Promise<api.WikiArticle>((r) => {
          firstResolve = r as Resolve;
        }),
    );
    // Second call (after Retry) resolves successfully.
    fetchSpy.mockResolvedValueOnce({
      path: "team/about/README.md",
      title: "About",
      content: "Body after retry.",
      last_edited_by: "pm",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["pm"],
      backlinks: [],
      word_count: 3,
      categories: [],
    });

    vi.useFakeTimers();
    try {
      render(
        <WikiArticle
          path="team/about/README.md"
          catalog={[]}
          onNavigate={() => {}}
        />,
      );
      expect(screen.getByText(/Loading article/i)).toBeInTheDocument();

      // Advance past the 5s timeout — loader should flip to the error state.
      await vi.advanceTimersByTimeAsync(5_001);
      expect(
        screen.getByText(/Still waiting on the broker/i),
      ).toBeInTheDocument();
      const retry = screen.getByRole("button", {
        name: /retry loading article/i,
      });
      expect(retry).toBeInTheDocument();

      // Click Retry — the second fetch resolves and the article renders.
      retry.click();
      // Release the dangling first promise so it cannot stomp later state.
      const releaseFirst = firstResolve as Resolve | null;
      releaseFirst?.({
        path: "team/about/README.md",
        title: "About",
        content: "stale",
        last_edited_by: "pm",
        last_edited_ts: new Date().toISOString(),
        revisions: 1,
        contributors: ["pm"],
        backlinks: [],
        word_count: 1,
        categories: [],
      });
      await vi.runAllTimersAsync();
    } finally {
      vi.useRealTimers();
    }
    await waitFor(() =>
      expect(screen.getByText(/Body after retry/)).toBeInTheDocument(),
    );
    expect(fetchSpy).toHaveBeenCalledTimes(2);
  });

  it("shows a loading state before the fetch resolves", async () => {
    // Arrange
    type Resolve = (v: api.WikiArticle) => void;
    let resolveFn: Resolve | null = null;
    vi.spyOn(api, "fetchArticle").mockImplementation(
      () =>
        new Promise<api.WikiArticle>((r) => {
          resolveFn = r as Resolve;
        }),
    );
    // Act
    render(<WikiArticle path="a" catalog={[]} onNavigate={() => {}} />);
    expect(screen.getByText(/Loading article/i)).toBeInTheDocument();
    // Finalize
    const finish = resolveFn as Resolve | null;
    finish?.({
      path: "a",
      title: "A",
      content: "body",
      last_edited_by: "pm",
      last_edited_ts: new Date().toISOString(),
      revisions: 1,
      contributors: ["pm"],
      backlinks: [],
      word_count: 1,
      categories: [],
    });
    await waitFor(() =>
      expect(screen.queryByText(/Loading article/i)).not.toBeInTheDocument(),
    );
  });
});

describe("<WikiArticle history and refresh>", () => {
  it("renders Sources populated from fetchHistory with author slugs visible", async () => {
    // Arrange
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);
    vi.spyOn(api, "fetchHistory").mockResolvedValue({
      commits: [
        {
          sha: "aaaaaaa1111",
          author_slug: "ceo",
          msg: "Initial brief",
          date: "2026-01-16T00:00:00Z",
        },
        {
          sha: "bbbbbbb2222",
          author_slug: "pm",
          msg: "Add pilot scope",
          date: "2026-01-17T00:00:00Z",
        },
        {
          sha: "ccccccc3333",
          author_slug: "cro",
          msg: "Pricing note",
          date: "2026-01-18T00:00:00Z",
        },
      ],
    });

    // Act
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );

    // Assert
    const sourcesHeading = await screen.findByRole("heading", {
      name: "Sources",
    });
    const sourcesSection = sourcesHeading.closest("section") as HTMLElement;
    expect(sourcesSection).not.toBeNull();
    expect(sourcesSection.textContent).toContain("Initial brief");
    expect(sourcesSection.textContent).toContain("Add pilot scope");
    expect(sourcesSection.textContent).toContain("Pricing note");
    // Author slugs surface as upper-cased names inside the Sources list.
    expect(sourcesSection.textContent).toContain("CEO");
    expect(sourcesSection.textContent).toContain("PM");
    expect(sourcesSection.textContent).toContain("CRO");
    // Short SHA rendering (first 7 chars).
    expect(sourcesSection.textContent).toContain("aaaaaaa");
  });

  it("refetches article and history when externalRefreshNonce changes", async () => {
    const fetchArticle = vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      title: "Customer X",
    });
    const fetchHistory = vi
      .spyOn(api, "fetchHistory")
      .mockResolvedValue({ commits: [] });

    const { rerender } = render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
        externalRefreshNonce={0}
      />,
    );

    await waitFor(() => expect(fetchArticle).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(fetchHistory).toHaveBeenCalledTimes(1));

    rerender(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
        externalRefreshNonce={1}
      />,
    );

    await waitFor(() => expect(fetchArticle).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(fetchHistory).toHaveBeenCalledTimes(2));
  });

  it("renders a loading placeholder in Sources while history is fetching", async () => {
    // Arrange
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);
    type Resolve = (v: { commits: api.WikiHistoryCommit[] }) => void;
    let resolveHistory: Resolve | null = null;
    vi.spyOn(api, "fetchHistory").mockImplementation(
      () =>
        new Promise<{ commits: api.WikiHistoryCommit[] }>((r) => {
          resolveHistory = r as Resolve;
        }),
    );

    // Act
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );

    // Assert — article renders, sources placeholder appears
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText(/loading sources/i)).toBeInTheDocument();

    // Finalize so cleanup is clean
    const finish = resolveHistory as Resolve | null;
    finish?.({ commits: [] });
  });

  it("refetches article and history after an inline editor save", async () => {
    const fetchArticleSpy = vi
      .spyOn(api, "fetchArticle")
      .mockResolvedValueOnce({
        ...STUB_ARTICLE,
        content: "Original body",
        commit_sha: "oldsha1",
      })
      .mockResolvedValueOnce({
        ...STUB_ARTICLE,
        content: "Updated body",
        commit_sha: "newsha1",
      });
    const fetchHistorySpy = vi
      .spyOn(api, "fetchHistory")
      .mockResolvedValue({ commits: [] });
    vi.spyOn(api, "writeHumanArticle").mockResolvedValue({
      path: STUB_ARTICLE.path,
      commit_sha: "newsha1",
      bytes_written: 12,
    });

    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );

    await screen.findByText("Original body");
    fireEvent.click(screen.getByRole("button", { name: "Edit source" }));
    const editor = (await screen.findByTestId(
      "wk-tiptap-stub",
    )) as HTMLTextAreaElement;
    fireEvent.change(editor, {
      target: { value: "Updated body" },
    });
    fireEvent.change(screen.getByTestId("wk-editor-commit"), {
      target: { value: "refresh article" },
    });
    fireEvent.click(screen.getByTestId("wk-editor-save"));

    await waitFor(() => expect(fetchArticleSpy).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(fetchHistorySpy).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("Updated body")).toBeInTheDocument();
  });
});

describe("<WikiArticle staleness badges>", () => {
  it("shows 'agents only' badge when agent_read_count > 0 and human_read_count = 0", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 3,
      human_read_count: 0,
      days_unread: 0,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("agents only")).toBeInTheDocument();
  });

  it("shows 'unread 30d+' badge when days_unread > 30", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 2,
      days_unread: 45,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 30d+")).toBeInTheDocument();
  });

  it("shows 'unread 7d+' badge when days_unread is between 8 and 30", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 1,
      days_unread: 14,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 7d+")).toBeInTheDocument();
  });

  it("shows 'unread 30d+' badge at the boundary — days_unread=30 is 'unread 30d+', not 7d+", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 1,
      days_unread: 30,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 30d+")).toBeInTheDocument();
    expect(screen.queryByText("unread 7d+")).not.toBeInTheDocument();
  });

  it("shows 'unread 30d+' badge at exact 30-day boundary", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 1,
      days_unread: 30,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 30d+")).toBeInTheDocument();
    expect(screen.queryByText("unread 7d+")).not.toBeInTheDocument();
  });

  it("shows 'unread 30d+' badge for days_unread=31 (above stale threshold)", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 1,
      days_unread: 31,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 30d+")).toBeInTheDocument();
    expect(screen.queryByText("unread 7d+")).not.toBeInTheDocument();
  });

  it("shows 'unread 7d+' badge at exact boundary — days_unread=7 now triggers aging badge", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 0,
      human_read_count: 1,
      days_unread: 7,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText("unread 7d+")).toBeInTheDocument();
    expect(screen.queryByText("unread 30d+")).not.toBeInTheDocument();
  });

  it("shows no staleness badge for a recently-read article", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      agent_read_count: 1,
      human_read_count: 2,
      days_unread: 3,
    });
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByText("agents only")).not.toBeInTheDocument();
    expect(screen.queryByText("unread 30d+")).not.toBeInTheDocument();
    expect(screen.queryByText("unread 7d+")).not.toBeInTheDocument();
  });
});

describe("<WikiArticle history fallback>", () => {
  it("renders nothing for Sources when fetchHistory rejects", async () => {
    // Arrange
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);
    vi.spyOn(api, "fetchHistory").mockRejectedValue(
      new Error("git log unavailable"),
    );

    // Act
    render(
      <WikiArticle
        path="people/customer-x"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );

    // Assert — article still renders, but no Sources section appears
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.queryByRole("heading", { name: "Sources" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/loading sources/i)).not.toBeInTheDocument();
  });
});

describe("<WikiArticle synthesis status>", () => {
  it("shows 'generating brief…' badge when synthesis_queued is true (ICP Example 1 & 2)", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      ghost: true,
      synthesis_queued: true,
    });
    render(
      <WikiArticle
        path="company/acme-corp"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByRole("status", {
        name: "Brief is being generated from recent activity",
      }),
    ).toHaveTextContent("generating brief…");
  });

  it("shows no 'generating brief…' badge when synthesis_queued is false (ICP Example 3)", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      ghost: true,
      synthesis_queued: false,
    });
    render(
      <WikiArticle
        path="company/cloudvault-inc"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByText("generating brief…")).not.toBeInTheDocument();
  });

  it("shows no 'generating brief…' badge on a real (non-ghost) article", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      ghost: false,
      synthesis_queued: false,
    });
    render(
      <WikiArticle
        path="company/acme-corp"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Customer X" }),
      ).toBeInTheDocument(),
    );
    expect(screen.queryByText("generating brief…")).not.toBeInTheDocument();
  });
});

describe("<WikiArticle open-in-edit hand-off>", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
  });

  it("opens a freshly-created page directly in the editor tab", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue({
      ...STUB_ARTICLE,
      path: "team/playbooks/onboarding.md",
      title: "Onboarding",
      content: "# Onboarding\n",
    });
    // Simulate the tree's create flow parking the intent before navigating.
    requestOpenInEdit("team/playbooks/onboarding.md");

    render(
      <WikiArticle
        path="team/playbooks/onboarding.md"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    // The Edit tab is active and the editor surface mounts without the user
    // having to click "Edit source" first.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Edit source" })).toHaveClass(
        "active",
      ),
    );
    expect(await screen.findByTestId("wk-tiptap-stub")).toBeInTheDocument();
  });

  it("opens an existing page (no pending intent) in the read view", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);

    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={() => {}}
      />,
    );

    await screen.findByRole("heading", { name: "Customer X" });
    expect(screen.getByRole("button", { name: "Article" })).toHaveClass(
      "active",
    );
    expect(screen.queryByTestId("wk-tiptap-stub")).toBeNull();
  });
});

describe("<WikiArticle delete affordance>", () => {
  it("confirms before deleting, then deletes and navigates away on success", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);
    const deleteSpy = vi.spyOn(api, "deletePage").mockResolvedValue({
      path: STUB_ARTICLE.path,
      commit_sha: "del0001",
    });
    const onNavigate = vi.fn();

    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={onNavigate}
      />,
    );

    await screen.findByRole("heading", { name: "Customer X" });
    fireEvent.click(screen.getByTestId("wk-article-delete"));

    // The confirm dialog appears first — no delete yet (tell-don't-ask veto).
    expect(screen.getByTestId("wk-article-delete-confirm")).toBeInTheDocument();
    expect(deleteSpy).not.toHaveBeenCalled();
    // Initial focus lands on Cancel so a stray Enter cancels rather than deletes.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus(),
    );

    fireEvent.click(screen.getByTestId("wk-article-delete-confirm-btn"));
    await waitFor(() => expect(deleteSpy).toHaveBeenCalledTimes(1));
    expect(deleteSpy).toHaveBeenCalledWith("people/customer-x");
    // On success the user is sent back to the catalog (empty path).
    await waitFor(() => expect(onNavigate).toHaveBeenCalledWith(""));
  });

  it("surfaces an error and keeps the user on the page when delete fails", async () => {
    vi.spyOn(api, "fetchArticle").mockResolvedValue(STUB_ARTICLE);
    vi.spyOn(api, "deletePage").mockRejectedValue(new Error("broker down"));
    const onNavigate = vi.fn();

    render(
      <WikiArticle
        path="people/customer-x"
        catalog={[]}
        onNavigate={onNavigate}
      />,
    );

    await screen.findByRole("heading", { name: "Customer X" });
    fireEvent.click(screen.getByTestId("wk-article-delete"));
    fireEvent.click(screen.getByTestId("wk-article-delete-confirm-btn"));

    expect(await screen.findByText("broker down")).toBeInTheDocument();
    // The page is still here; no navigation away on failure.
    expect(onNavigate).not.toHaveBeenCalled();
  });
});
