import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

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

describe("<WikiArticle>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    // Default history stub — individual tests override as needed.
    vi.spyOn(api, "fetchHistory").mockResolvedValue({ commits: [] });
  });

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
