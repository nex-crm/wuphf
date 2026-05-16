import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as richApi from "../../api/richArtifacts";
import * as api from "../../api/wiki";
import WikiArticle from "./WikiArticle";

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
    await waitFor(() => expect(getByText(/streams from/)).toBeInTheDocument());
  });

  it("shows promoted visual views in the Visual tab", async () => {
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

    const visualTab = await screen.findByRole("button", { name: "Visual" });
    await waitFor(() => expect(visualTab).not.toBeDisabled());

    expect(await screen.findByTestId("wk-visual-artifact")).toBeInTheDocument();
    expect(screen.getByTitle("Visual Plan")).toHaveAttribute(
      "srcdoc",
      expect.stringContaining("Visual artifact"),
    );
  });

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
    fireEvent.change(screen.getByTestId("wk-editor-textarea"), {
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
