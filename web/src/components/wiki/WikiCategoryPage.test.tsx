import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { WikiCatalogEntry } from "../../api/wiki";
import WikiCategoryPage, { categoryLabel } from "./WikiCategoryPage";

const CATALOG: WikiCatalogEntry[] = [
  {
    path: "team/people/zoe.md",
    title: "Zoe",
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T12:00:00Z",
    group: "people",
  },
  {
    path: "team/people/eng.md",
    title: "Eng",
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T12:00:00Z",
    group: "people",
  },
  {
    path: "team/people/ana.md",
    title: "Ana",
    author_slug: "ceo",
    last_edited_ts: "2026-06-09T12:00:00Z",
    group: "people",
  },
  {
    path: "team/companies/acme-corp.md",
    title: "Acme Corp",
    author_slug: "eng",
    last_edited_ts: "2026-06-10T12:00:00Z",
    group: "companies",
  },
];

describe("categoryLabel", () => {
  it("capitalizes slug segments", () => {
    expect(categoryLabel("people")).toBe("People");
    expect(categoryLabel("go-to-market")).toBe("Go To Market");
  });
});

describe("<WikiCategoryPage>", () => {
  it("lists category members alphabetically, grouped by first letter", () => {
    render(
      <WikiCategoryPage
        slug="people"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Category: People" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/The following 3 pages are/)).toBeInTheDocument();
    // Letter buckets in order.
    const letters = screen
      .getAllByRole("heading", { level: 2 })
      .map((h) => h.textContent);
    expect(letters).toEqual(["A", "E", "Z"]);
    // Members from another category are excluded.
    expect(screen.queryByText("Acme Corp")).not.toBeInTheDocument();
  });

  it("navigates to a member article on click", () => {
    const onNavigate = vi.fn();
    render(
      <WikiCategoryPage
        slug="people"
        catalog={CATALOG}
        onNavigate={onNavigate}
      />,
    );
    fireEvent.click(screen.getByRole("link", { name: "Ana" }));
    expect(onNavigate).toHaveBeenCalledWith("team/people/ana.md");
  });

  it("cross-links sibling categories", () => {
    const onNavigate = vi.fn();
    render(
      <WikiCategoryPage
        slug="people"
        catalog={CATALOG}
        onNavigate={onNavigate}
      />,
    );
    fireEvent.click(screen.getByRole("link", { name: "Companies" }));
    expect(onNavigate).toHaveBeenCalledWith("_category/companies");
  });

  it("renders an empty state for a category with no pages", () => {
    render(
      <WikiCategoryPage
        slug="ghosts"
        catalog={CATALOG}
        onNavigate={() => {}}
      />,
    );
    expect(screen.getByText("No pages yet.")).toBeInTheDocument();
  });

  it("includes cross-folder articles via explicit categories", () => {
    // A playbook (different folder) filed into the People category via its
    // `categories:` frontmatter shows alongside the folder members.
    const catalog: WikiCatalogEntry[] = [
      ...CATALOG,
      {
        path: "team/playbooks/onboarding.md",
        title: "Onboarding",
        author_slug: "ceo",
        last_edited_ts: "2026-06-11T12:00:00Z",
        group: "playbooks",
        categories: ["people"],
      },
    ];
    render(
      <WikiCategoryPage
        slug="people"
        catalog={catalog}
        onNavigate={() => {}}
      />,
    );
    // 3 folder members + 1 cross-folder explicit member = 4.
    expect(screen.getByText(/The following 4 pages are/)).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Onboarding" }),
    ).toBeInTheDocument();
  });

  it("lists sibling categories that exist only via explicit categories", () => {
    const catalog: WikiCatalogEntry[] = [
      ...CATALOG,
      {
        path: "team/playbooks/mql.md",
        title: "MQL",
        author_slug: "ceo",
        last_edited_ts: "2026-06-11T12:00:00Z",
        group: "playbooks",
        categories: ["revenue-operations"],
      },
    ];
    const onNavigate = vi.fn();
    render(
      <WikiCategoryPage
        slug="people"
        catalog={catalog}
        onNavigate={onNavigate}
      />,
    );
    // "Revenue Operations" is a real category (no matching folder) and appears
    // as a sibling link.
    fireEvent.click(screen.getByRole("link", { name: "Revenue Operations" }));
    expect(onNavigate).toHaveBeenCalledWith("_category/revenue-operations");
  });
});
