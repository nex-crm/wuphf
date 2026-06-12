import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { WikiFSTreeNode } from "../../api/wiki";
import * as wiki from "../../api/wiki";
import WikiSidebar from "./WikiSidebar";

const TREE: WikiFSTreeNode[] = [
  {
    name: "companies",
    path: "team/companies",
    type: "dir",
    title: "Companies",
    children: [
      {
        name: "acme.md",
        path: "team/companies/acme.md",
        type: "page",
        title: "Acme Corp",
      },
    ],
  },
  {
    name: "people",
    path: "team/people",
    type: "dir",
    title: "People",
    children: [
      {
        name: "nazz.md",
        path: "team/people/nazz.md",
        type: "page",
        title: "Nazz",
      },
    ],
  },
];

describe("<WikiSidebar>", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.spyOn(wiki, "fetchWikiTree").mockResolvedValue(TREE);
  });

  it("renders the menu links and the page tree as the navigation", async () => {
    render(<WikiSidebar currentPath={null} onNavigate={() => {}} />);
    expect(screen.getByTestId("wk-sidebar-home")).toBeInTheDocument();
    expect(screen.getByTestId("wk-sidebar-audit")).toBeInTheDocument();
    expect(screen.getByTestId("wk-sidebar-lint")).toBeInTheDocument();
    // Root groups = the wiki's kinds (Companies, People, …).
    await waitFor(() =>
      expect(screen.getByText("Companies")).toBeInTheDocument(),
    );
    expect(screen.getByText("People")).toBeInTheDocument();
  });

  it("navigates to a nested page through the tree (deep link path)", async () => {
    const onNavigate = vi.fn();
    render(<WikiSidebar currentPath={null} onNavigate={onNavigate} />);
    await waitFor(() => expect(screen.getByText("People")).toBeInTheDocument());
    // Expand the People space, then open the nested article.
    fireEvent.click(screen.getByText("People"));
    fireEvent.click(await screen.findByText("Nazz"));
    expect(onNavigate).toHaveBeenCalledWith("team/people/nazz.md");
  });

  it("opens the first matching page on Enter in the sidebar search", async () => {
    const onNavigate = vi.fn();
    render(<WikiSidebar currentPath={null} onNavigate={onNavigate} />);
    await waitFor(() =>
      expect(screen.getByText("Companies")).toBeInTheDocument(),
    );
    const search = screen.getByLabelText("Search pages");
    fireEvent.change(search, { target: { value: "acme" } });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(onNavigate).toHaveBeenCalledWith("team/companies/acme.md");
  });

  it("marks the Overview link active on the home view and routes the menu links", () => {
    const onNavigate = vi.fn();
    render(<WikiSidebar currentPath="" onNavigate={onNavigate} />);
    expect(screen.getByTestId("wk-sidebar-home")).toHaveAttribute(
      "aria-current",
      "page",
    );
    fireEvent.click(screen.getByTestId("wk-sidebar-audit"));
    expect(onNavigate).toHaveBeenCalledWith("_audit");
    fireEvent.click(screen.getByTestId("wk-sidebar-lint"));
    expect(onNavigate).toHaveBeenCalledWith("_lint");
    fireEvent.click(screen.getByTestId("wk-sidebar-home"));
    expect(onNavigate).toHaveBeenCalledWith("");
  });

  it("highlights the currently open article in the tree", async () => {
    render(
      <WikiSidebar
        currentPath="team/companies/acme.md"
        onNavigate={() => {}}
      />,
    );
    // The branch containing the current page is visible after expanding.
    await waitFor(() =>
      expect(screen.getByText("Companies")).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByText("Companies"));
    const row = (await screen.findByText("Acme Corp")).closest(
      "[role='treeitem']",
    );
    expect(row).toHaveAttribute("aria-selected", "true");
  });
});
