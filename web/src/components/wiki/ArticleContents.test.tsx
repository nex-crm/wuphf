import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import ArticleContents from "./ArticleContents";
import type { TocEntry } from "./TocBox";

const ENTRIES: TocEntry[] = [
  { level: 1, num: "1", anchor: "work-history", title: "Work history" },
  { level: 1, num: "2", anchor: "observations", title: "Observations" },
  { level: 2, num: "2.1", anchor: "billing", title: "Billing" },
];

describe("<ArticleContents>", () => {
  it("renders numbered H2/H3 entries", () => {
    render(<ArticleContents entries={ENTRIES} />);
    expect(screen.getByTestId("wk-contents")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "1 Work history" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "2.1 Billing" }),
    ).toBeInTheDocument();
  });

  it("collapses and expands via the [hide] toggle", () => {
    render(<ArticleContents entries={ENTRIES} />);
    fireEvent.click(screen.getByRole("button", { name: "[hide]" }));
    expect(
      screen.queryByRole("link", { name: "1 Work history" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "[show]" }));
    expect(
      screen.getByRole("link", { name: "1 Work history" }),
    ).toBeInTheDocument();
  });

  it("scrolls to the section instead of following the # fragment", () => {
    const heading = document.createElement("h2");
    heading.id = "observations";
    let scrolled = false;
    heading.scrollIntoView = () => {
      scrolled = true;
    };
    document.body.appendChild(heading);
    try {
      render(<ArticleContents entries={ENTRIES} />);
      fireEvent.click(screen.getByRole("link", { name: "2 Observations" }));
      expect(scrolled).toBe(true);
    } finally {
      heading.remove();
    }
  });

  it("renders nothing for an article with no sections", () => {
    render(<ArticleContents entries={[]} />);
    expect(screen.queryByTestId("wk-contents")).not.toBeInTheDocument();
  });
});
