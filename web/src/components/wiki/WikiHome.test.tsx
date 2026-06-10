import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { WikiCatalogEntry } from "../../api/wiki";
import * as api from "../../api/wiki";
import WikiHome, { suggestTitles } from "./WikiHome";

const CATALOG: WikiCatalogEntry[] = [
  {
    path: "team/companies/acme-corp.md",
    title: "Acme Corp",
    author_slug: "eng",
    last_edited_ts: "2026-06-10T12:00:00Z",
    group: "companies",
  },
  {
    path: "team/people/eng.md",
    title: "Eng",
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T12:00:00Z",
    group: "people",
  },
  {
    path: "team/playbooks/renewal.md",
    title: "Renewal Playbook",
    author_slug: "eng",
    last_edited_ts: "2026-06-08T12:00:00Z",
    group: "playbooks",
  },
];

describe("suggestTitles", () => {
  it("ranks title-prefix matches before substring matches", () => {
    const out = suggestTitles(CATALOG, "en");
    // "Eng" is a title-prefix match; "Renewal Playbook" only matches as a
    // substring ("rENewal") and sorts after it.
    expect(out.map((s) => s.title)).toEqual(["Eng", "Renewal Playbook"]);
  });

  it("matches against paths as well as titles", () => {
    const out = suggestTitles(CATALOG, "playbooks/");
    expect(out.map((s) => s.title)).toEqual(["Renewal Playbook"]);
  });

  it("returns nothing for a blank query", () => {
    expect(suggestTitles(CATALOG, "  ")).toEqual([]);
  });
});

describe("<WikiHome>", () => {
  it("shows instant title suggestions and navigates on click", () => {
    const onNavigate = vi.fn();
    render(
      <WikiHome catalog={CATALOG} onNavigate={onNavigate} recentChanges={[]} />,
    );
    fireEvent.change(screen.getByTestId("wk-home-search"), {
      target: { value: "acme" },
    });
    const suggestion = within(screen.getByTestId("wk-suggestions")).getByText(
      "Acme Corp",
    );
    fireEvent.click(suggestion);
    expect(onNavigate).toHaveBeenCalledWith("team/companies/acme-corp.md");
  });

  it("runs a full-text search on submit and lists hits", async () => {
    vi.spyOn(api, "searchWiki").mockResolvedValue([
      { path: "team/people/eng.md", line: 3, snippet: "billing cadence" },
    ]);
    const onNavigate = vi.fn();
    render(
      <WikiHome catalog={CATALOG} onNavigate={onNavigate} recentChanges={[]} />,
    );
    fireEvent.change(screen.getByTestId("wk-home-search"), {
      target: { value: "billing" },
    });
    fireEvent.submit(screen.getByRole("search"));
    await waitFor(() =>
      expect(screen.getByTestId("wk-search-results")).toBeInTheDocument(),
    );
    expect(screen.getByText("billing cadence")).toBeInTheDocument();
    fireEvent.click(screen.getByText("team/people/eng.md"));
    expect(onNavigate).toHaveBeenCalledWith("team/people/eng.md");
  });

  it("links category entry points to category index pages", () => {
    const onNavigate = vi.fn();
    render(
      <WikiHome catalog={CATALOG} onNavigate={onNavigate} recentChanges={[]} />,
    );
    fireEvent.click(screen.getByRole("link", { name: "Companies" }));
    expect(onNavigate).toHaveBeenCalledWith("_category/companies");
  });

  it("falls back to recently-edited articles when the audit log is empty", () => {
    render(
      <WikiHome catalog={CATALOG} onNavigate={() => {}} recentChanges={[]} />,
    );
    // Most recently edited first.
    const recent = screen.getByLabelText("Recent changes");
    expect(recent).toHaveTextContent("Acme Corp");
    expect(recent).toHaveTextContent("Renewal Playbook");
  });

  it("renders audit-log entries as recent changes when present", () => {
    render(
      <WikiHome
        catalog={CATALOG}
        onNavigate={() => {}}
        recentChanges={[
          {
            sha: "abc123",
            author_slug: "eng",
            timestamp: "2026-06-10T13:00:00Z",
            message: "wiki: update acme article",
            paths: ["team/companies/acme-corp.md"],
          },
        ]}
      />,
    );
    expect(screen.getByText("wiki: update acme article")).toBeInTheDocument();
  });

  it("keeps the All files escape hatch one click away", () => {
    const onNavigate = vi.fn();
    render(
      <WikiHome catalog={CATALOG} onNavigate={onNavigate} recentChanges={[]} />,
    );
    fireEvent.click(screen.getByRole("link", { name: "All files →" }));
    expect(onNavigate).toHaveBeenCalledWith("_files");
  });
});
